package v1

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Tags handles GET /v1/tags — an Ollama-compatible model tag list.
// Clients that speak the Ollama API can point their base URL at the gateway
// and use /v1/tags to enumerate available models (mirrors 9router's /api/tags).
func (h *Handler) Tags(c *gin.Context) {
	models := h.buildModelList()
	tags := make([]gin.H, 0, len(models))
	for _, m := range models {
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		var details gin.H
		if v, ok := m["owned_by"].(string); ok && v != "" {
			details = gin.H{"family": v}
		} else {
			details = gin.H{}
		}
		tags = append(tags, gin.H{
			"name":      id,
			"model":     id,
			"modified_at": "2024-01-01T00:00:00Z",
			"size":      0,
			"digest":    "sha256:" + strings.Repeat("0", 64),
			"details":   details,
		})
	}
	if tags == nil {
		tags = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"models": tags})
}
