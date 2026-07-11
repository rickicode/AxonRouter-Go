package v1

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
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
		// Default to cx (Codex) for responses format
		provider = "cx"
		modelName = model
	}

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	body = executor.JSONSet(body, "model", modelName)

	// Connection failover loop: try up to 3 connections, no retry with same account.
	clientFormat := executor.FormatOpenAIResponses
	stream := executor.IsStreamRequest(body)
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)

	maxAttempts := 3
	for attempt := range maxAttempts {
		conn, err := h.getConnection(c.Request.Context(), provider, modelName)
		if err != nil {
			if attempt == 0 {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
				return
			}
			break
		}

		h.proactiveRefreshToken(c.Request.Context(), conn, provider)

		req := &executor.Request{
			Model:       modelName,
			Body:        translatedBody,
			Stream:      stream,
			APIKey:      conn.APIKey,
			AccessToken: conn.AccessToken,
			BaseURL:     conn.BaseURL,
			Provider:    provider,
		}

		// Use OpenAI Responses-specific methods for codex format
		if providerFormat == executor.FormatOpenAIResponses {
			h.handleResponsesFormat(c, exec, req, provider, conn, start, translatedBody, body)
			return
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
			// If client disconnected, don't try next connection — context is dead
			if c.Request.Context().Err() != nil {
				return
			}
			det := connstate.DetectError(0, "", err, provider, modelName, nil)
			if det.Category == connstate.ErrorRateLimit {
				h.exhaustion.MarkExhausted(conn.ID, quota.DefaultExhaustionTTL)
			} else if det.Category == connstate.ErrorQuota && det.CooldownUntil != nil {
				h.exhaustion.MarkExhausted(conn.ID, time.Until(*det.CooldownUntil))
			}
			h.combo.RecordFailure(conn.ID, det)
			h.persistCooldown(conn.ID, det)
			h.elig.Update(h.store)
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
				Modality:        "chat",
				LatencyMs:       latency,
				ErrorMessage:   err.Error(),
			})
			continue // immediately try next connection, no same-account retry
		}

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

	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "all connections exhausted or failing for provider: " + provider, "type": "server_error"}})
}

// handleResponsesFormat handles OpenAI Responses API format.
func (h *Handler) handleResponsesFormat(c *gin.Context, exec executor.Executor, req *executor.Request, provider string, conn *Connection, start time.Time, translatedReq, originalReq []byte) {
	proxyCtx := h.proxyContext(c.Request.Context(), conn)
	// ponytail: use the OpenAI executor's Responses/ResponsesStream methods
	openaiExec, ok := exec.(*executor.OpenAIExecutor)
	if !ok {
		// Fall back to generic execute
		if req.Stream {
			streamResult, err := exec.ExecuteStream(proxyCtx, req)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
				return
			}
			h.handleStreamResponse(c, streamResult, conn, provider, req.Model, start, translatedReq, originalReq)
		} else {
			resp, err := exec.Execute(proxyCtx, req)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
				return
			}
			c.Header("Content-Type", "application/json")
			c.Status(resp.StatusCode)
			c.Writer.Write(resp.Body)
		}
		return
	}

	if req.Stream {
		result, err := openaiExec.ResponsesStream(proxyCtx, req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
			return
		}

		_, providerFormat, _ := h.registry.Get(provider)
		errFormatter := func(err error) []byte {
			b, _ := json.Marshal(gin.H{"error": gin.H{"message": err.Error()}})
			return b
		}
		h.streamResponse(c, result, conn, provider, req.Model, executor.FormatOpenAIResponses, providerFormat, originalReq, translatedReq, errFormatter, start)
	} else {
		resp, err := openaiExec.Responses(proxyCtx, req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
			return
		}

		// Translate response
		clientFormat := executor.FormatOpenAIResponses
		_, providerFormat, _ := h.registry.Get(provider)
		translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), req.Model, originalReq, translatedReq, resp.Body, nil)

		// Log usage
		latency := time.Since(start).Milliseconds()
		tokenCounts := ExtractTokensFromBody(translatedResp)
		h.tracker.Log(&usage.LogEntry{
			ConnectionID:    conn.ID,
			ProviderTypeID:  provider,
			ModelID:         req.Model,
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
