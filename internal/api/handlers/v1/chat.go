package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// ChatCompletions handles POST /v1/chat/completions
func (h *Handler) ChatCompletions(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// Apply compression (fail-open)
	if h.compressionStrategy.Mode != compression.ModeOff {
		compressed, _, _ := compression.Apply(h.compressionStrategy, body)
		body = compressed
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}

	stream := executor.IsStreamRequest(body)

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		h.handleComboRequest(c, comboResult, body, model, start)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix (e.g., openai/gpt-4o)", "type": "invalid_request_error"}})
		return
	}

	// Cache check (exact match, non-stream, no tools)
	var cacheKey string
	if !stream && h.exactCache != nil && !compression.HasTools(body) {
		cacheKey = cache.ComputeKey(body, model)
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			c.Header("Content-Type", entry.ContentType)
			c.Header("X-Cache-Status", "HIT")
			c.Status(entry.StatusCode)
			c.Writer.Write(entry.Body)
			return
		}
	}

	// Replace model with unprefixed name
	body = executor.JSONSet(body, "model", modelName)

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// Connection failover loop: try up to 3 connections before giving up.
	// On each failure, mark the connection exhausted/cooldown and update eligibility
	// so the next getConnection call picks a different connection.
	clientFormat := executor.FormatOpenAI
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)

	maxAttempts := 3
	for attempt := range maxAttempts {
		// Client disconnected — context is dead, no point trying next connection
		if c.Request.Context().Err() != nil {
			return
		}
		conn, err := h.getConnection(c.Request.Context(), provider, modelName)
		if err != nil {
			if attempt == 0 {
				logging.Logger.Info("chat: get connection failed", "err", err.Error())
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
				return
			}
			break // tried some connections, all failed
		}

		// Parse provider-specific data
		var psdMap map[string]string
		if conn.ProviderSpecificData != "" {
			json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
		}

		logArgs := []any{"model", model, "provider", provider, "conn", conn.ID[:8], "name", conn.Name, "attempt", attempt + 1}
		if accountID := psdMap["accountId"]; accountID != "" {
			logArgs = append(logArgs, "account_id", accountID)
		}
		logging.Logger.Info("route", logArgs...)

		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		req := &executor.Request{
			Model:                modelName,
			Body:                 translatedBody,
			Stream:               stream,
			APIKey:               conn.APIKey,
			AccessToken:          conn.AccessToken,
			BaseURL:              conn.BaseURL,
			Provider:             provider,
			ProviderSpecificData: psdMap,
		}

		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
		latency := time.Since(start).Milliseconds()

		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if streamResult != nil {
			connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
		}

		if err != nil {
			det := connstate.DetectError(0, "", err, provider, modelName, nil)
			if det.Category == connstate.ErrorRateLimit {
				h.exhaustion.MarkExhausted(conn.ID, quota.DefaultExhaustionTTL)
			} else if det.Category == connstate.ErrorQuota && det.CooldownUntil != nil {
				h.exhaustion.MarkExhausted(conn.ID, time.Until(*det.CooldownUntil))
			}
			h.combo.RecordFailure(conn.ID, det)
			h.persistCooldown(conn.ID, det)
			h.elig.Update(h.store) // refresh so next getConnection skips this conn
			h.checkAutoDisable(conn.ID, provider)

			logging.Logger.Error("upstream error, trying next connection",
				"provider", provider,
				"conn", conn.ID[:8],
				"model", modelName,
				"error", det.Category,
				"detail", err.Error(),
				"attempt", attempt+1,
			)

			h.tracker.Log(&usage.LogEntry{
				ConnectionID:   conn.ID,
				ProviderTypeID: provider,
				ModelID:        modelName,
				Modality:       "chat",
				LatencyMs:      latency,
				ErrorMessage:   err.Error(),
			})
			continue // try next connection
		}

		// Success
		h.resetBanCount(conn.ID)
		h.combo.RecordSuccess(conn.ID)
		h.elig.Update(h.store)

		if req.Stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			h.tracker.Log(&usage.LogEntry{
				ConnectionID:    conn.ID,
				ProviderTypeID: provider,
				ModelID:         modelName,
				Modality:        "chat",
				InputTokens:     tokenCounts.InputTokens,
				OutputTokens:    tokenCounts.OutputTokens,
				ReasoningTokens: tokenCounts.ReasoningTokens,
				CachedTokens:    tokenCounts.CachedTokens,
				LatencyMs:       latency,
				StatusCode:      resp.StatusCode,
			})
			// Cache store
			if cacheKey != "" && h.exactCache != nil {
				h.exactCache.Set(cacheKey, cache.CacheEntry{
					Body:        translatedResp,
					StatusCode:  resp.StatusCode,
					ContentType: "application/json",
				})
			}
			c.Header("Content-Type", "application/json")
			c.Header("X-Cache-Status", "MISS")
			c.Status(resp.StatusCode)
			c.Writer.Write(translatedResp)
		}
		return
	}

	// All connections exhausted or failing
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "all connections exhausted or failing for provider: " + provider, "type": "server_error"}})
}

// handleComboRequest handles a request that matched a combo.
func (h *Handler) handleComboRequest(c *gin.Context, comboResult *combo.ComboResult, body []byte, model string, start time.Time) {
	// Enforce combo timeout budget
	comboTimeout := 30 * time.Second // default
	if comboResult.Combo != nil && comboResult.Combo.TimeoutMs > 0 {
		comboTimeout = time.Duration(comboResult.Combo.TimeoutMs) * time.Millisecond
	}
	comboCtx, cancel := context.WithTimeout(c.Request.Context(), comboTimeout)
	defer cancel()

	// Try each step in the combo
	for _, step := range comboResult.Steps {
		connID, ok := h.combo.PickConnection(step)
		if !ok {
			continue
		}

		provider, modelName := executor.SplitModel(step.ModelID)

		// Full preflight: cooldown, exhaustion, token refresh, load credentials.
		conn, err := h.prepareConnection(comboCtx, connID, provider, modelName)
		if err != nil {
			continue
		}

		exec, providerFormat, err := h.resolveExecutor(provider, modelName)
		if err != nil {
			continue
		}

		// Translate request
		clientFormat := executor.FormatOpenAI
		stream := executor.IsStreamRequest(body)
		translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)

		req := &executor.Request{
			Model:       modelName,
			Body:        translatedBody,
			Stream:      stream,
			APIKey:      conn.APIKey,
			AccessToken: conn.AccessToken,
			BaseURL:     conn.BaseURL,
			Provider:    provider,
		}

		proxyCtx := h.proxyContext(comboCtx, conn)
		resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
		latency := time.Since(start).Milliseconds()

		if err != nil {
			det := connstate.DetectError(0, "", err, provider, modelName, nil)
			if det.Category == connstate.ErrorRateLimit {
				h.exhaustion.MarkExhausted(connID, quota.DefaultExhaustionTTL)
			}
			h.combo.RecordFailure(connID, det)
			h.persistCooldown(connID, det)
			if det.Status != connstate.StatusReady {
				h.elig.Update(h.store)
			}
			continue // Try next step
		}

		// Success
		h.resetBanCount(connID)
		h.combo.RecordSuccess(connID)
		h.elig.Update(h.store) // refresh eligibility after success

		if req.Stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)

			tokenCounts := ExtractTokensFromBody(translatedResp)
			h.tracker.Log(&usage.LogEntry{
				ConnectionID:    connID,
				ProviderTypeID:  provider,
				ModelID:         modelName,
				Modality:        "chat",
				InputTokens:     tokenCounts.InputTokens,
				OutputTokens:    tokenCounts.OutputTokens,
				ReasoningTokens: tokenCounts.ReasoningTokens,
				CachedTokens:    tokenCounts.CachedTokens,
				LatencyMs:       latency,
				StatusCode:      resp.StatusCode,
			})

			c.Header("Content-Type", "application/json")
			c.Status(resp.StatusCode)
			c.Writer.Write(translatedResp)
		}
		return
	}

	// All combo steps failed
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "all combo steps failed", "type": "server_error"}})
}

// handleNonStreamResponse handles non-streaming chat completions.
func (h *Handler) handleNonStreamResponse(c *gin.Context, exec executor.Executor, req *executor.Request) {
	resp, err := exec.Execute(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}

// handleStreamResponse handles streaming chat completions.
func (h *Handler) handleStreamResponse(c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte) {
	_, providerFormat, _ := h.registry.Get(provider)
	errFormatter := func(err error) []byte {
		b, _ := json.Marshal(gin.H{"error": gin.H{"message": err.Error()}})
		return b
	}
	h.streamResponse(c, result, conn, provider, model, executor.FormatOpenAI, providerFormat, originalReq, translatedReq, errFormatter, start)
}
