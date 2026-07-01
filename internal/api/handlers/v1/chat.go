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

	conn, err := h.getConnection(c.Request.Context(), provider, modelName) // Q1: pass modelID
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}

	// Check OAuth expiry and refresh if needed
	if !conn.OAuthExpiresAt.IsZero() && time.Now().After(conn.OAuthExpiresAt.Add(-30*time.Second)) {
		if err := h.refreshOAuthToken(c.Request.Context(), conn, provider); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "oauth token refresh failed", "type": "auth_error"}})
			return
		}
	}

	// Translate request (client format → provider format)
	clientFormat := executor.FormatOpenAI // /v1/chat/completions is OpenAI format
	stream := executor.IsStreamRequest(body)
	translatedBody := registry.Request(string(clientFormat), string(providerFormat), modelName, body, stream)

	// Parse provider-specific data for executor (e.g., Antigravity projectId)
	var psdMap map[string]string
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

	if req.Stream {
		streamResult, err = exec.ExecuteStream(proxyCtx, req)
	} else {
		resp, err = exec.Execute(proxyCtx, req)
	}

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
		det := connstate.DetectError(0, "", err, provider, modelName, nil) // Q5: pass modelID
		h.store.RecordFailure(conn.ID, det)
		// Refresh eligibility only when connection becomes non-eligible
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

		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}

	// Record success
	h.store.RecordSuccess(conn.ID)
	h.elig.Update(h.store) // refresh eligibility after success
	h.combo.RecordSuccess(conn.ID)

	if req.Stream {
		h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
	} else {
		// Translate response (provider format → client format)
		translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)

		// Log usage
		h.tracker.Log(&usage.LogEntry{
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			Modality:       "chat",
			LatencyMs:      latency,
			StatusCode:     resp.StatusCode,
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

		// Load connection details
		conn, err := h.loadConnectionByID(c.Request.Context(), connID)
		if err != nil {
			continue
		}

		proxyCtx := h.proxyContext(comboCtx, conn)

		provider, modelName := executor.SplitModel(step.ModelID)
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

		var resp *executor.Response
		var streamResult *executor.StreamResult

		if req.Stream {
			streamResult, err = exec.ExecuteStream(proxyCtx, req)
		} else {
			resp, err = exec.Execute(proxyCtx, req)
		}

		latency := time.Since(start).Milliseconds()

		if err != nil {
			det := connstate.DetectError(0, "", err, provider, modelName, nil) // Q5: pass modelID
			h.store.RecordFailure(connID, det)                                 // Q7: update circuit breaker
			if det.Status != connstate.StatusReady {
				h.elig.Update(h.store)
			}
			h.combo.RecordFailure(connID, 0, err.Error())
			continue // Try next step
		}

		// Success
		h.combo.RecordSuccess(connID)
		h.store.RecordSuccess(connID)
		h.elig.Update(h.store) // refresh eligibility after success

		if req.Stream {
			h.handleStreamResponse(c, streamResult, conn, provider, modelName, start, translatedBody, body)
		} else {
			translatedResp := registry.ResponseNonStream(c.Request.Context(), string(providerFormat), string(clientFormat), modelName, body, translatedBody, resp.Body, nil)

			h.tracker.Log(&usage.LogEntry{
				ConnectionID:   connID,
				ProviderTypeID: provider,
				ModelID:        modelName,
				Modality:       "chat",
				LatencyMs:      latency,
				StatusCode:     resp.StatusCode,
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
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "server_error"}})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	var lastChunk []byte
	clientFormat := executor.FormatOpenAI
	_, providerFormat, _ := h.registry.Get(provider)

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			// Q6 fix: escape error message for valid JSON in SSE
			errJSON, _ := json.Marshal(gin.H{"error": gin.H{"message": chunk.Err.Error()}})
			c.Writer.Write([]byte("data: " + string(errJSON) + "\n\n"))
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

	// Send [DONE] marker
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

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
		LatencyMs:       latency,
		StatusCode:      http.StatusOK,
	})
}
