package v1

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// TTS handles POST /v1/audio/speech
func (h *Handler) TTS(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		model = "tts-1"
	}
	if h.checkTokenBudget(c, body) != nil {
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "openai"
		modelName = model
	}

	// Get TTS executor (always use the global TTS executor)
	ttsExec := executor.NewTTSExecutor(executor.NewBaseExecutor())

	conn, err := h.getConnection(c.Request.Context(), provider, modelName)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}

	// Proactive token refresh
	h.proactiveRefreshToken(c.Request.Context(), conn, provider)
	// Parse provider-specific data
	var psdMap map[string]string
	if conn.ProviderSpecificData != "" {
		if err := json.Unmarshal([]byte(conn.ProviderSpecificData), &psdMap); err != nil {
			logging.Logger.Warn("malformed provider_specific_data", "conn", shortID(conn.ID, 8), "error", err.Error())
		}
	}

	req := &executor.Request{
		Model:                modelName,
		Body:                 body,
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
	resp, streamResult, err = h.executeWithRetry(proxyCtx, ttsExec, req, conn, provider, modelName)
	_ = streamResult
	if err != nil {
		if !h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, false) {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "internal server error", "type": "server_error"}})
		}
		return
	}

	h.tracker.Log(&usage.LogEntry{
		ApiKeyID: c.GetString("api_key_id"),
		ConnectionID: conn.ID,
		ProviderTypeID: provider,
		ModelID: modelName,
		ProxyPoolID: executor.ProxyPoolIDFromContext(proxyCtx),
		ApiType: apiTypeFromPath(c.Request.URL.Path),
		Modality: "audio",
		Stream: false,
		LatencyMs: time.Since(start).Milliseconds(),
		StatusCode: resp.StatusCode})

	h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, nil, false)
	// Return audio bytes
	c.Header("Content-Type", "audio/mpeg")
	if ct := resp.Headers.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}
