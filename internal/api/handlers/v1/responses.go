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
	stream := executor.IsStreamRequest(body)
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)
	maxAttempts := 3
	var lastConn *Connection
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
			h.handleFailoverError(conn, provider, modelName, err, attempt, latency)
			continue
		}

		h.resetBanCount(conn.ID)
		h.combo.RecordSuccess(conn.ID)
		h.elig.Update(h.store)

		if stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
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
			c.Header("Content-Type", "application/json")
			c.Status(resp.StatusCode)
			c.Writer.Write(translatedResp)
		}
		return
	}
	msg := "all connections exhausted or failing"
	detail := gin.H{"provider": provider, "model": modelName}
	if lastConn != nil {
		detail["name"] = lastConn.Name
	}
	logging.Logger.Error("all connections exhausted", "provider", provider, "model", modelName, "last_conn", detail)
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": msg, "type": "server_error", "detail": detail}})
}
