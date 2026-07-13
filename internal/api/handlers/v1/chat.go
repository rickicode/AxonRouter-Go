package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
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

	// Apply compression (fail-open); skip if the request uses prompt-cache markers.
	body = h.compressRequestBody(body)

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}
	stream := executor.IsStreamRequest(body)
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		h.handleComboRequest(c, comboResult, body, model, start, stream)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix (e.g., openai/gpt-4o)", "type": "invalid_request_error"}})
		return
	}

	// Cache check (exact match, non-stream, no tools, no cache_control)
	cacheKey := h.exactCacheKey(body, model, stream)
	if cacheKey != "" {
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			h.serveCacheHit(c, entry)
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
	maxAttempts := 5
	var lastConn *Connection
	var lastErr error
	var lastErrCategory string
	for attempt := range maxAttempts {
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
			break
		}
		lastConn = conn

		var psdMap map[string]string
		if conn.ProviderSpecificData != "" {
			json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
		}

		// Resolve proxy config early so we can log it
		var proxyCfg executor.ProxyConfig
		if h.resolver != nil {
			resolved := h.resolver.Resolve(conn.ProviderSpecificData, conn.Provider)
			proxyCfg = executor.ProxyConfig{
				Enabled:     resolved.Enabled,
				ProxyURL:    resolved.ProxyURL,
				NoProxy:     resolved.NoProxy,
				RelayURL:    resolved.RelayURL,
				RelayAuth:   resolved.RelayAuth,
				RelayType:   resolved.RelayType,
				StrictProxy: resolved.StrictProxy,
			}
		}

		logArgs := []any{"model", model, "provider", provider, "conn", conn.ID[:8], "name", conn.Name, "attempt", attempt + 1, "proxy", proxyCfg.ProxyLabel()}
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

		proxyCtx := executor.ContextWithProxy(c.Request.Context(), proxyCfg)
		resp, streamResult, err := h.executeDirect(proxyCtx, exec, req)
		latency := time.Since(start).Milliseconds()
		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if streamResult != nil {
			connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
		}
		if provider == "cx" {
			h.codexPersistIfCodex(conn, resp, streamResult)
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				return
			}
			if h.writeUpstreamClientError(c, err, conn, provider, modelName, start, stream) {
				return
			}
			retry, cat := h.handleFailoverError(c, conn, provider, modelName, err, attempt, latency, stream)
			lastErr = err
			lastErrCategory = cat
			if !retry {
				break // non-retryable error, stop failover
			}
			if !failoverBackoff(c.Request.Context(), attempt, maxAttempts) {
				return
			}
			continue
		}

		h.resetBanCount(conn.ID)
		h.persistSuccess(conn.ID)
		h.combo.RecordSuccess(conn.ID)

		if req.Stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			h.tracker.Log(&usage.LogEntry{
				ApiKeyID:            c.GetString("api_key_id"),
				ConnectionID:        conn.ID,
				ProviderTypeID:      provider,
				ModelID:             modelName,
				Modality:            "chat",
				Stream:              stream,
				InputTokens:         tokenCounts.InputTokens,
				OutputTokens:        tokenCounts.OutputTokens,
				ReasoningTokens:     tokenCounts.ReasoningTokens,
				CachedTokens:        tokenCounts.CachedTokens,
				CacheCreationTokens: tokenCounts.CacheCreationTokens,
				LatencyMs:           latency,
				StatusCode:          resp.StatusCode,
			})
			h.storeExactCache(cacheKey, translatedResp, resp.StatusCode)
			h.incrementAPIKeyUsage(c.GetString("api_key_id"), tokenCounts.InputTokens+tokenCounts.OutputTokens)
			h.writeJSONResponse(c, resp.StatusCode, translatedResp)
		}
		return
	}

	// Build category-specific error message
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)

	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}

// handleComboRequest handles a request that matched a combo.
func (h *Handler) handleComboRequest(c *gin.Context, comboResult *combo.ComboResult, body []byte, model string, start time.Time, stream bool) {
	comboTimeout := 30 * time.Second
	if comboResult.Combo != nil && comboResult.Combo.TimeoutMs > 0 {
		comboTimeout = time.Duration(comboResult.Combo.TimeoutMs) * time.Millisecond
	}
	comboCtx, cancel := context.WithTimeout(c.Request.Context(), comboTimeout)
	defer cancel()

	for _, step := range comboResult.Steps {
		connID, ok := h.combo.PickConnection(step)
		if !ok {
			continue
		}
		provider, modelName := executor.SplitModel(step.ModelID)
		conn, err := h.prepareConnection(comboCtx, connID, provider, modelName)
		if err != nil {
			continue
		}
		exec, providerFormat, err := h.resolveExecutor(provider, modelName)
		if err != nil {
			continue
		}

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
			if h.isClientCanceled(c, err) {
				return
			}
			det := connstate.DetectError(c.Request.Context(), 0, "", err, provider, modelName, nil)
			if connstate.HasPerModelQuota(provider) && det.ModelID != "" &&
				(det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota) {
				scope := connstate.ModelScope(provider, det.ModelID)
				h.exhaustion.MarkExhausted(quota.ExhaustKey(connID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
			} else if det.Category == connstate.ErrorRateLimit {
				h.exhaustion.MarkExhausted(connID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
			}
			h.combo.RecordFailure(connID, det)
			h.persistCooldownScoped(connID, det)
			if det.Status != connstate.StatusReady {
				h.elig.ScheduleUpdate()
			}
			continue
		}

		h.resetBanCount(connID)
		h.persistSuccess(connID)
		h.combo.RecordSuccess(connID)
		if provider == "cx" {
			h.codexPersistIfCodex(conn, resp, streamResult)
		}

		if req.Stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			h.tracker.Log(&usage.LogEntry{
				ApiKeyID:            c.GetString("api_key_id"),
				ConnectionID:        connID,
				ProviderTypeID:      provider,
				ModelID:             modelName,
				Modality:            "chat",
				Stream:              stream,
				InputTokens:         tokenCounts.InputTokens,
				OutputTokens:        tokenCounts.OutputTokens,
				ReasoningTokens:     tokenCounts.ReasoningTokens,
				CachedTokens:        tokenCounts.CachedTokens,
				CacheCreationTokens: tokenCounts.CacheCreationTokens,
				LatencyMs:           latency,
				StatusCode:          resp.StatusCode,
			})
			c.Header("Content-Type", "application/json")
			h.incrementAPIKeyUsage(c.GetString("api_key_id"), tokenCounts.InputTokens+tokenCounts.OutputTokens)
			c.Status(resp.StatusCode)
			c.Writer.Write(translatedResp)
		}
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "all combo steps failed", "type": "server_error"}})
}

// handleNonStreamResponse handles non-streaming chat completions.
func (h *Handler) handleNonStreamResponse(c *gin.Context, exec executor.Executor, req *executor.Request) {
	start := time.Now()
	resp, err := exec.Execute(c.Request.Context(), req)
	if err != nil {
		if h.writeUpstreamClientError(c, err, nil, "", req.Model, start, false) {
			return
		}
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
		var upErr *executor.UpstreamError
		if errors.As(err, &upErr) {
			return upErr.Body
		}
		logging.Logger.Error("upstream streaming error", "provider", provider, "model", model, "error", err)
		b, _ := json.Marshal(gin.H{"error": gin.H{"message": "upstream streaming error"}})
		return b
	}
	h.streamResponse(c, result, conn, provider, model, executor.FormatOpenAI, providerFormat, originalReq, translatedReq, errFormatter, start)
}
