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

// Responses handles POST /v1/responses (OpenAI Responses format)
func (h *Handler) Responses(c *gin.Context) {
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
		h.handleComboRequest(c, comboResult, body, model, start, stream)
		return
	}

	// Direct routing
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}
	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	body = executor.JSONSet(body, "model", modelName)
	clientFormat := executor.FormatOpenAIResponses
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
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
				return
			}
			break
		}
		lastConn = conn
		h.proactiveRefreshToken(c.Request.Context(), conn, provider)
		psdMap := map[string]string{}
		if conn.ProviderSpecificData != "" {
			json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap)
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
		var resp *executor.Response
		var streamResult *executor.StreamResult
		if stream {
			streamResult, err = exec.ExecuteStream(proxyCtx, req)
		} else {
			resp, err = exec.Execute(proxyCtx, req)
		}
		latency := time.Since(start).Milliseconds()
		if resp != nil {
			connstate.ParseRateLimitHeaders(resp.Headers, h.store, conn.ID, modelName)
		}
		if streamResult != nil {
			connstate.ParseRateLimitHeaders(streamResult.Headers, h.store, conn.ID, modelName)
		}
		if err != nil {
			if h.isClientCanceled(c, err) {
				return
			}
			retry, cat := h.handleFailoverError(c, conn, provider, modelName, err, attempt, latency, stream)
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

		if stream {
			_, providerFmt, _ := h.registry.Get(provider)
			errFormatter := func(err error) []byte {
				logging.Logger.Error("upstream streaming error", "provider", provider, "model", modelName, "error", err)
				b, _ := json.Marshal(gin.H{"error": gin.H{"message": "upstream streaming error", "type": "server_error"}})
				return b
			}
			h.streamResponse(c, streamResult, conn, provider, modelName, executor.FormatOpenAIResponses, providerFmt, body, translatedBody, errFormatter, start)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)
			tokenCounts := ExtractTokensFromBody(translatedResp)
			h.tracker.Log(&usage.LogEntry{
				ConnectionID:    conn.ID,
				ProviderTypeID:  provider,
				ModelID:         modelName,
				Modality:        "chat",
				Stream:          stream,
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
	msg, statusCode, errType := buildFailoverErrorResponse(lastErrCategory, lastErr, modelName)

	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error(msg, "provider", provider, "model", modelName, "category", lastErrCategory)
	c.JSON(statusCode, gin.H{"error": gin.H{"message": msg, "type": errType, "detail": detail}})
}
