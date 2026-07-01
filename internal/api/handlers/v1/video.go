package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Video handles POST /v1/video/generations
func (h *Handler) Video(c *gin.Context) {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model must include provider prefix", "type": "invalid_request_error"}})
		return
	}

	videoExec := executor.NewVideoExecutor(executor.NewBaseExecutor())

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

	proxyCtx := h.proxyContext(c.Request.Context(), conn)

	resp, err := videoExec.Execute(proxyCtx, req)
	if err != nil {
		// Log failure
		h.tracker.Log(&usage.LogEntry{
			ConnectionID:   conn.ID,
			ProviderTypeID: provider,
			ModelID:        modelName,
			Modality:       "video",
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
		Modality:       "video",
		LatencyMs:      time.Since(start).Milliseconds(),
		StatusCode:     resp.StatusCode,
	})

	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
}
