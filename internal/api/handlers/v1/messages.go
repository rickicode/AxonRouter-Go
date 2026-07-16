package v1

import (
	"context"
	"encoding/json"
	"errors"
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
		if errors.Is(err, errBodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, claudeError("invalid_request_error", err.Error()))
		} else {
			c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", errReadBody.Error()))
		}
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
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	// Exact cache check (non-stream, no tools, no cache_control)
	cacheKey := h.exactCacheKey(body, model, stream)
	if cacheKey != "" {
		if entry, ok := h.exactCache.Get(cacheKey); ok {
			h.serveCacheHit(c, body, entry)
			return
		}
	}

	// Combo-first routing
	if comboResult, ok := h.combo.Resolve(model); ok {
		h.handleComboRequest(c, comboResult, body, model, start, stream)
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

	// Connection failover loop: try up to failoverMaxAttempts connections before giving up.
	clientFormat := executor.FormatClaude
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	translatedBody = sanitizeStreamOptions(translatedBody, stream, clientFormat, providerFormat, c.Request.URL.Path)
	// NOTE: configurable via failover_max_attempts setting.
	maxAttempts := h.failoverAttempts()
	var lastErr error
	var lastErrCategory string
	for attempt := range maxAttempts {
		if c.Request.Context().Err() != nil {
			writeContextDone(c)
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
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)
	psdMap := map[string]string{}
	if conn.ProviderSpecificData != "" {
		if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
			logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
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
		if provider == "cx" {
			h.codexPersistIfCodex(conn, resp, streamResult)
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				return
			}
			retry, cat := h.handleFailoverError(proxyCtx, c, conn, provider, modelName, err, attempt, latency, stream)
			lastErr = err
			lastErrCategory = cat
			if !retry {
				break
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
		h.handleClaudeStreamResponse(proxyCtx, c, streamResult, conn, provider, modelName, start, translatedBody, body, "")
	} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(clientFormat), string(providerFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			tokensEstimated := false
			if tokenCounts.InputTokens+tokenCounts.OutputTokens == 0 && resp.StatusCode < 400 {
				estInput := usage.EstimateTokensFromRequest(body)
				estOutput := usage.EstimateTokensFromResponse(translatedResp)
				if estInput > 0 || estOutput > 0 {
					tokenCounts.InputTokens = estInput
					tokenCounts.OutputTokens = estOutput
					tokensEstimated = true
				}
			}
	h.logRequest(c, &usage.LogEntry{
		ApiKeyID: c.GetString("api_key_id"),
		ConnectionID: conn.ID,
		ProviderTypeID: provider,
		ModelID: modelName,
		ProxyPoolID: executor.ProxyPoolIDFromContext(proxyCtx),
		ApiType:     apiTypeFromPath(c.Request.URL.Path),
		Modality: "chat",
		Stream: stream,
		InputTokens: tokenCounts.InputTokens,
		OutputTokens: tokenCounts.OutputTokens,
		ReasoningTokens: tokenCounts.ReasoningTokens,
		CachedTokens: tokenCounts.CachedTokens,
		CacheCreationTokens: tokenCounts.CacheCreationTokens,
		LatencyMs: latency,
			StatusCode: resp.StatusCode,
			TokensEstimated: tokensEstimated,
		})
		h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, translatedResp, true)
		if resp.StatusCode < 300 {
			h.storeExactCache(cacheKey, translatedResp, resp.StatusCode)
		}
		h.writeJSONResponse(c, resp.StatusCode, translatedResp)
		}
		return
	}

	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, claudeError(errType, msg))
}

// handleClaudeStreamResponse handles streaming Claude responses.
func (h *Handler) handleClaudeStreamResponse(ctx context.Context, c *gin.Context, result *executor.StreamResult, conn *Connection, provider, model string, start time.Time, translatedReq, originalReq []byte, comboID string) {
	_, providerFormat, _ := h.registry.Get(provider)
	errFormatter := func(err error) []byte {
		logging.Logger.Error("upstream streaming error", "provider", provider, "model", model, "error", err)
		b, _ := json.Marshal(claudeError("api_error", "upstream streaming error"))
		return b
	}
	h.streamResponse(ctx, c, result, conn, provider, model, executor.FormatClaude, providerFormat, originalReq, translatedReq, errFormatter, start, comboID)
}

// CountTokens handles POST /v1/messages/count_tokens
func (h *Handler) CountTokens(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, claudeError("invalid_request_error", err.Error()))
		} else {
			c.JSON(http.StatusBadRequest, claudeError("invalid_request_error", errReadBody.Error()))
		}
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

	if tc, ok := exec.(executor.TokenCounter); ok {
		proxyCtx := h.proxyContext(c.Request.Context(), conn)
		resp, err := tc.CountTokens(proxyCtx, req)
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
