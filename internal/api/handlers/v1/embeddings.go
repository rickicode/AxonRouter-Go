package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Embeddings handles POST /v1/embeddings
func (h *Handler) Embeddings(c *gin.Context) {
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

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "openai"
		modelName = model
	}

	exec, _, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	body = executor.JSONSet(body, "model", modelName)

	conn, err := h.getConnection(c.Request.Context(), provider, modelName) // Q1: pass modelID
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no available connection", "type": "server_error"}})
		return
	}

	// OAuth refresh
	if !conn.OAuthExpiresAt.IsZero() && time.Now().After(conn.OAuthExpiresAt.Add(-30*time.Second)) {
		if err := h.refreshOAuthToken(c.Request.Context(), conn, provider); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "oauth token refresh failed", "type": "auth_error"}})
			return
		}
	}

	req := &executor.Request{
		Model:       modelName,
		Body:        body,
		APIKey:      conn.APIKey,
		AccessToken: conn.AccessToken,
		BaseURL:     conn.BaseURL,
		Provider:    provider,
	}

	// Use OpenAI executor's Embeddings method
	if openaiExec, ok := exec.(*executor.OpenAIExecutor); ok {
		resp, err := openaiExec.Embeddings(c.Request.Context(), req)
		if err != nil {
			// Log failure
			h.tracker.Log(&usage.LogEntry{
				ConnectionID:   conn.ID,
				ProviderTypeID: provider,
				ModelID:        modelName,
				Modality:       "embedding",
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
			Modality:       "embedding",
			LatencyMs:      time.Since(start).Milliseconds(),
			StatusCode:     resp.StatusCode,
		})

		c.Header("Content-Type", "application/json")
		c.Status(resp.StatusCode)
		c.Writer.Write(resp.Body)
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "embeddings only supported for OpenAI-compatible providers", "type": "invalid_request_error"}})
}
