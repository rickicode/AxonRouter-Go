package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Images handles POST /v1/images/generations
func (h *Handler) Images(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	model := executor.JSONGet(body, "model")
	if model == "" {
		model = "dall-e-3"
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "openai"
		modelName = model
	}

	var imagesExec executor.Executor
	if exec, format, err := h.resolveExecutor(provider, modelName); err == nil {
		if imgGen, ok := exec.(executor.ImageGenerator); ok && format == executor.FormatOpenAI {
			imagesExec = &imageGeneratorAdapter{ImageGenerator: imgGen}
		}
	}
	if imagesExec == nil {
		imagesExec = executor.NewImagesExecutor(executor.NewBaseExecutor())
	}

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
	resp, streamResult, err = h.executeWithRetry(proxyCtx, imagesExec, req, conn, provider, modelName)
	_ = streamResult
	if err != nil {
		if !h.writeUpstreamClientError(c, err, conn, provider, modelName, start, false) {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "internal server error", "type": "server_error"}})
		}
		return
	}

	h.tracker.Log(&usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		Modality:       "image",
		Stream:         false,
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode})

	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}

// imageGeneratorAdapter exposes an executor.ImageGenerator through the standard
// executor.Executor interface so executeWithRetry can drive it.
type imageGeneratorAdapter struct {
	ImageGenerator executor.ImageGenerator
}

func (a *imageGeneratorAdapter) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return a.ImageGenerator.Images(ctx, req)
}

func (a *imageGeneratorAdapter) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, fmt.Errorf("image generation does not support streaming")
}
