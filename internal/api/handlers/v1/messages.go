package v1

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Messages handles POST /v1/messages (Anthropic format)
func (h *Handler) Messages(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", "model is required"))
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
		// Default to claude if no prefix
		provider = "claude"
		modelName = model
	}

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", err.Error()))
		return
	}

	// Replace model with unprefixed name
	body = executor.JSONSet(body, "model", modelName)

	conn, err := h.getConnection(c.Request.Context(), provider, modelName) // Q1: pass modelID
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, claudeError("server_error", "no available connection"))
		return
	}

	// Proactive token refresh
	h.proactiveRefreshToken(c.Request.Context(), conn, provider)

	// Translate request (Claude format → provider format)
	clientFormat := executor.FormatClaude // /v1/messages is Claude format
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

	proxyCtx := h.proxyContext(c.Request.Context(), conn)

	// Execute with reactive 401/403 retry
	var resp *executor.Response
	var streamResult *executor.StreamResult

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		if req.Stream {
			streamResult, err = exec.ExecuteStream(proxyCtx, req)
		} else {
			resp, err = exec.Execute(proxyCtx, req)
		}
		if err == nil {
			break
		}
		if isUnrecoverableRefreshError(err) {
			break
		}
		if attempt < 2 && isAuthError(err) && h.proactiveRefreshToken(c.Request.Context(), conn, provider) {
			req.AccessToken = conn.AccessToken
			continue
		}
		if !isAuthError(err) {
			break
		}
	}

	latency := time.Since(start).Milliseconds()

	// Parse rate limit headers from response
	if resp != nil {
		connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
	}
	if streamResult != nil {
		connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
	}

	if err != nil {
		det := connstate.DetectError(0, "", err, provider, modelName, nil) // Q5: pass modelID
		h.store.RecordFailure(conn.ID, det)
		if det.Status != connstate.StatusReady {
			h.elig.Update(h.store)
		}
		h.combo.RecordFailure(conn.ID, 0, err.Error())

		h.tracker.Log(&usage.LogEntry{
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			Modality:       "chat",
			LatencyMs:      latency,
			ErrorMessage:   err.Error(),
		})

		c.JSON(http.StatusBadGateway, claudeError("server_error", err.Error()))
		return
	}

	h.resetBanCount(conn.ID)
	h.store.RecordSuccess(conn.ID)
	h.elig.Update(h.store) // refresh eligibility after success
	h.combo.RecordSuccess(conn.ID)

	if req.Stream {
		h.handleClaudeStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
	} else {
		// Translate response (provider format → client format)
		translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)

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

// handleClaudeNonStreamResponse handles non-streaming Claude responses.
func (h *Handler) handleClaudeNonStreamResponse(c *gin.Context, exec executor.Executor, req *executor.Request) {
	resp, err := exec.Execute(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, claudeError("server_error", err.Error()))
		return
	}

	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}

// handleClaudeStreamResponse handles streaming Claude responses.
func (h *Handler) handleClaudeStreamResponse(c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, claudeError("server_error", "streaming not supported"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	var lastChunk []byte
	clientFormat := executor.FormatClaude
	_, providerFormat, _ := h.registry.Get(provider)

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			// Q6 fix: escape error message for valid JSON in SSE
			errJSON, _ := json.Marshal(claudeError("api_error", chunk.Err.Error()))
			c.Writer.Write([]byte("event: error\ndata: " + string(errJSON) + "\n\n"))
			flusher.Flush()
			return
		}

		// Translate chunk
		translatedChunks := registry.Response(c.Request.Context(), string(providerFormat), string(clientFormat), model, originalReq, translatedReq, chunk.Payload, nil)
		for _, tc := range translatedChunks {
			c.Writer.Write(tc)
			c.Writer.Write([]byte("\n\n"))
			flusher.Flush()
		}
		lastChunk = chunk.Payload
	}

	// Extract tokens from final chunk
	latency := time.Since(start).Milliseconds()
	tokenCounts := ExtractTokensFromFinalChunk(lastChunk)

	h.tracker.Log(&usage.LogEntry{
		ConnectionID:    conn.ID,
		ProviderTypeID:  provider,
		ModelID:         model,
		Modality:        "chat",
		InputTokens:     tokenCounts.InputTokens,
		OutputTokens:    tokenCounts.OutputTokens,
		ReasoningTokens: tokenCounts.ReasoningTokens,
		CachedTokens:    tokenCounts.CachedTokens,
		LatencyMs:       latency,
		StatusCode:      http.StatusOK,
	})
}

// CountTokens handles POST /v1/messages/count_tokens
func (h *Handler) CountTokens(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, claudeError("invalid_request_error", err.Error()))
		return
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", "model is required"))
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "claude"
		modelName = model
	}

	exec, _, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", err.Error()))
		return
	}

	body = executor.JSONSet(body, "model", modelName)

	conn, err := h.getConnection(c.Request.Context(), provider, modelName) // Q1: pass modelID
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, claudeError("server_error", "no available connection"))
		return
	}

	req := &executor.Request{
		Model:       modelName,
		Body:        body,
		APIKey:      conn.APIKey,
		AccessToken: conn.AccessToken,
		BaseURL:     conn.BaseURL,
		Provider:    provider,
	}

	// Use Claude executor's CountTokens method
	if claudeExec, ok := exec.(*executor.ClaudeExecutor); ok {
		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		resp, err := claudeExec.CountTokens(proxyCtx, req)
		if err != nil {
			c.JSON(http.StatusBadGateway, claudeError("server_error", err.Error()))
			return
		}
		c.Header("Content-Type", "application/json")
		c.Status(resp.StatusCode)
		c.Writer.Write(resp.Body)
		return
	}

	c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", "token counting only supported for Claude models"))
}

func claudeError(errType, message string) gin.H {
	return gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": message},
	}
}
