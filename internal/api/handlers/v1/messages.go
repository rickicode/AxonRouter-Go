package v1

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Messages handles POST /v1/messages (Anthropic format)
func (h *Handler) Messages(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, claudeError("invalid_request_error", err.Error()))
		return
	}

	// Apply compression (fail-open); skip if the request uses prompt-cache markers.
	body = h.compressRequestBody(body)

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", "model is required"))
		return
	}

	stream := executor.IsStreamRequest(body)

	// Exact cache check (non-stream, no tools, no cache_control)
	cacheKey := h.exactCacheKey(body, model, stream)
	if cacheKey != "" {
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			h.serveCacheHit(c, entry)
			return
		}
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		h.handleComboRequest(c, comboResult, body, model, start)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "claude"
		modelName = model
	}
	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", err.Error()))
		return
	}
	body = executor.JSONSet(body, "model", modelName)

	// Connection failover loop: try up to 3 connections before giving up.
	clientFormat := executor.FormatClaude
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	maxAttempts := 3
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
				c.JSON(http.StatusServiceUnavailable, claudeError("server_error", "no available connection"))
				return
			}
			break
		}
		lastConn = conn

		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		psdMap := map[string]string{}
		if conn.ProviderSpecificData != "" {
			if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
				logging.Logger.Error("failed to unmarshal provider_specific_data", "conn", conn.ID[:8], "error", err.Error())
			}
		}

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
			retry, cat := h.handleFailoverError(conn, provider, modelName, err, attempt, latency)
			lastErr = err
			lastErrCategory = cat
			if !retry {
				break
			}
			continue
		}

		h.resetBanCount(conn.ID)
		h.combo.RecordSuccess(conn.ID)

		if req.Stream {
			h.handleClaudeStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
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
			h.storeExactCache(cacheKey, translatedResp, resp.StatusCode)
			h.writeJSONResponse(c, resp.StatusCode, translatedResp)
		}
		return
	}

	msg := "all connections exhausted or failing"
	statusCode := http.StatusServiceUnavailable
	errType := "server_error"
	switch lastErrCategory {
	case string(connstate.ErrorModelNotFound):
		msg = "model not found: " + modelName
		statusCode = http.StatusNotFound
		errType = "invalid_request_error"
	case string(connstate.ErrorAuth):
		msg = "authentication failed for all connections"
		statusCode = http.StatusUnauthorized
		errType = "authentication_error"
	case string(connstate.ErrorRateLimit):
		statusCode = http.StatusTooManyRequests
		errType = "rate_limit_error"
	}

	if lastErrCategory == string(connstate.ErrorRateLimit) {
		if upErr := extractUpstreamError(lastErr); upErr != nil {
			msg = extractErrorMessage(upErr.Body)
			if msg == "" {
				msg = upErr.Error()
			}
		}
		if msg == "" || msg == "all connections exhausted or failing" {
			msg = "rate limit exceeded for all connections"
		}
	}

	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, claudeError(errType, msg))
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
	_, providerFormat, _ := h.registry.Get(provider)
	errFormatter := func(err error) []byte {
		logging.Logger.Error("upstream streaming error", "provider", provider, "model", model, "error", err)
		b, _ := json.Marshal(claudeError("api_error", "upstream streaming error"))
		return b
	}
	h.streamResponse(c, result, conn, provider, model, executor.FormatClaude, providerFormat, originalReq, translatedReq, errFormatter, start)
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
	conn, err := h.getConnection(c.Request.Context(), provider, modelName)
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
