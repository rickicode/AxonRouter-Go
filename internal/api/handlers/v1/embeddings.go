package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/modalities"
	providerpkg "github.com/rickicode/AxonRouter-Go/internal/provider"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// Embeddings handles POST /v1/embeddings
func (h *Handler) Embeddings(c *gin.Context) {
	start := time.Now()
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	// Apply compression (fail-open); skip if the request uses prompt-cache markers.
	// Embeddings bodies rarely benefit, but this keeps the handler consistent with
	// the rest of the v1 surface and is essentially a no-op when no messages exist.
	body = h.compressRequestBody(body)

	model := executor.JSONGet(body, "model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model is required", "type": "invalid_request_error"}})
		return
	}
	if h.checkTokenBudget(c, body) != nil {
		return
	}
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "openai"
		modelName = model
	}

	// Resolve executor before capability checks so we can report unknown providers
	// explicitly and then validate modality requirements.
	exec, _, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// Validate provider capabilities before allocating a connection.
	var serviceKinds string
	err = h.db.QueryRow(`SELECT COALESCE(service_kinds, '[]') FROM provider_types WHERE id = ?`, provider).Scan(&serviceKinds)
	if err != nil {
		// Unknown providers already fail above; this is defensive.
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "provider not configured for embeddings", "type": "invalid_request_error"}})
		return
	}
	var kinds []string
	if err := json.Unmarshal([]byte(serviceKinds), &kinds); err != nil {
		kinds = providerpkg.DefaultServiceKinds()
	}
	if !providerpkg.HasServiceKind(kinds, providerpkg.ServiceKindEmbedding) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "provider does not support embeddings", "type": "invalid_request_error"}})
		return
	}

	// When a per-modality registry exists for the provider, restrict to known models.
	if len(modalities.Models(provider, "embedding")) > 0 {
		canon := normalizeEmbeddingModel(provider, modelName)
		if !modalities.SupportsModel(provider, "embedding", canon) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model not available for embeddings on provider " + provider, "type": "invalid_request_error"}})
			return
		}
	}

	body = executor.JSONSet(body, "model", modelName)
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
		Model: modelName,
		Body: body,
		APIKey: conn.APIKey,
		AccessToken: conn.AccessToken,
		BaseURL: conn.BaseURL,
		Provider: provider,
		ProviderSpecificData: psdMap,
	}
	proxyCtx := h.proxyContext(c.Request.Context(), conn)

	// Use any executor that implements EmbeddingsExecutor through the standard
	// adapter so executeWithRetry can drive it with reactive retry.
	embedExec, ok := exec.(executor.EmbeddingsExecutor)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "embeddings only supported for OpenAI-compatible providers", "type": "invalid_request_error"}})
		return
	}
	resp, _, err := h.executeWithRetry(proxyCtx, executor.NewEmbeddingsAdapter(embedExec), req, conn, provider, modelName)
	if err != nil {
		if !h.writeUpstreamClientError(proxyCtx, c, err, conn, provider, modelName, start, false) {
			// writeUpstreamClientError returns false for rate-limit (429) so the chat
			// path can failover; embeddings has no failover, so surface the upstream
			// body directly for a clearer client error.
			var upErr *executor.UpstreamError
			if errors.As(err, &upErr) && len(upErr.Body) > 0 {
				c.Header("Content-Type", "application/json")
				c.Status(upErr.StatusCode)
				c.Writer.Write(upErr.Body)
				return
			}
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
		Modality: "embedding",
		Stream: false,
		LatencyMs: time.Since(start).Milliseconds(),
		StatusCode: resp.StatusCode})
	h.accumulateAPIKeyUsage(c.GetString("api_key_id"), body, resp.Body, false)
	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(resp.Body)
	return
}

// normalizeEmbeddingModel converts a gateway model id to the canonical upstream
// form used by the per-modality registry. For Cloudflare Workers AI the gateway
// id is "cf/author/model" while the registry lists "@cf/author/model".
func normalizeEmbeddingModel(provider, modelName string) string {
	if provider == "cf" && !strings.HasPrefix(modelName, "@cf/") {
		if strings.HasPrefix(modelName, "cf/") {
			return "@" + modelName
		}
		return "@cf/" + modelName
}
	return modelName
}
