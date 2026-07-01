package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Images handles POST /v1/images/generations
func (h *Handler) Images(c *gin.Context) {
	start := time.Now()

	body, err := readBody(c)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
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

	imagesExec := executor.NewImagesExecutor(executor.NewBaseExecutor())

	conn, err := h.getConnection(c.Request.Context(), provider, modelName) // Q1: pass modelID
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}

	// Proactive token refresh
	h.proactiveRefreshToken(c.Request.Context(), conn, provider)

	req := &executor.Request{
		Model:       modelName,
		Body:        body,
		APIKey:      conn.APIKey,
		AccessToken: conn.AccessToken,
		BaseURL:     conn.BaseURL,
		Provider:    provider,
	}

	proxyCtx := h.proxyContext(c.Request.Context(), conn)

	// Execute with reactive 401/403 retry
	var resp *executor.Response
	for attempt := 0; attempt < 2; attempt++ {
		resp, err = imagesExec.Execute(proxyCtx, req)
		if attempt == 0 && err != nil && isAuthError(err) {
			if h.proactiveRefreshToken(c.Request.Context(), conn, provider) {
				req.AccessToken = conn.AccessToken
				continue
			}
		}
		break
	}
	if err != nil {
		// Log failure
		h.tracker.Log(&usage.LogEntry{
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			Modality:       "image",
			LatencyMs:      time.Since(start).Milliseconds(),
			ErrorMessage:   err.Error(),
		})

		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "type": "server_error"}})
		return
	}

	// Log success
	h.tracker.Log(&usage.LogEntry{
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        modelName,
		Modality:       "image",
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode,
	})

	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}
