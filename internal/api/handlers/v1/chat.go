package v1

import (
	"context"
	"encoding/json"
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

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}

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

	// Replace model with unprefixed name
	body = executor.JSONSet(body, "model", modelName)

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	conn, err := h.getConnection(c.Request.Context(), provider, modelName)
	if err != nil {
		logging.Logger.Info("chat: get connection failed", "err", err.Error())
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}
	// Parse provider-specific data early for logs + executor (e.g., Antigravity projectId)
	var psdMap map[string]string
	if conn.ProviderSpecificData != "" {
		json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
	}

	// Compact route log: one line with all essential info
	logArgs := []any{"model", model, "provider", provider, "conn", conn.ID[:8], "name", conn.Name}
	if accountID := psdMap["accountId"]; accountID != "" {
		logArgs = append(logArgs, "account_id", accountID)
	}
	logging.Logger.Info("route", logArgs...)

	// Proactive token refresh (matches OmniRoute checkAndRefreshToken)
	h.proactiveRefreshToken(c.Request.Context(), conn, provider)

	clientFormat := executor.FormatOpenAI // /v1/chat/completions is OpenAI format
	stream := executor.IsStreamRequest(body)
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)

	// psdMap is already parsed above for route logging and is reused below

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

	// Execute with reactive 401/403 retry (3 attempts, linear backoff)
	var resp *executor.Response
	var streamResult *executor.StreamResult

	resp, streamResult, err = h.executeWithRetry(proxyCtx, exec, req, conn, provider, modelName)

	latency := time.Since(start).Milliseconds()

	// Parse rate limit headers from response
	if resp != nil {
		connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
	}
	if streamResult != nil {
		connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
	}

	// Handle errors
	if err != nil {
		det := connstate.DetectError(0, "", err, provider, modelName, nil)
		// Mark connection exhausted on 429 rate limit (OmniRoute markAccountExhaustedFrom429)
		if det.Category == connstate.ErrorRateLimit {
			h.exhaustion.MarkExhausted(conn.ID, quota.DefaultExhaustionTTL)
		}
		h.combo.RecordFailure(conn.ID, det)
		h.persistCooldown(conn.ID, det)
		// Refresh eligibility only when connection becomes non-eligible
		if det.Status != connstate.StatusReady {
			h.elig.Update(h.store)
		}

		// Auto-disable banned accounts (auth/quota/balance failures)
		h.checkAutoDisable(conn.ID, provider)

		h.tracker.Log(&usage.LogEntry{
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			Modality:       "chat",
			LatencyMs:      latency,
			ErrorMessage:   err.Error(),
		})

		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}

	// Record success (reset ban count BEFORE RecordSuccess which zeros in-memory BanCount)
	h.resetBanCount(conn.ID)
	h.combo.RecordSuccess(conn.ID)
	h.elig.Update(h.store) // refresh eligibility after success

	if req.Stream {
		h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
	} else {
		// Translate response (provider format → client format)
		translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)

		// Extract token counts from translated response (OpenAI format)
		tokenCounts := ExtractTokensFromBody(translatedResp)
		h.tracker.Log(&usage.LogEntry{
			ConnectionID:    conn.ID,
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
		resp, streamResult, err := h.executeWithRetry(proxyCtx, exec, req, conn, provider, modelName)
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
