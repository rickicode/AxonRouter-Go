package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// ModelHandler handles model-related admin endpoints.
type ModelHandler struct {
	db       *sql.DB
	registry *executor.Registry
	store    *connstate.Store
}

// NewModelHandler creates a new model handler.
func NewModelHandler(db *sql.DB, registry *executor.Registry, store *connstate.Store) *ModelHandler {
	return &ModelHandler{db: db, registry: registry, store: store}
}

// ListModels returns available models for a provider.
// Tries dynamic upstream query first; falls back to static model list.
func (h *ModelHandler) ListModels(c *gin.Context) {
	providerID := c.Param("id")

	// Get provider from DB
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	err := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, providerID).Scan(&provider.ID, &provider.Format, &provider.BaseURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Try dynamic model listing via executor
	exec, _, ok := h.registry.Get(provider.ID)
	if ok {
		if tester, ok := exec.(modelTester); ok {
			var apiKey, accessToken string
			err = h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(oauth_token,'') FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&apiKey, &accessToken)
			if err == nil {
				creds := &executor.Request{
					APIKey:      apiKey,
					AccessToken: accessToken,
					BaseURL:     provider.BaseURL,
					Provider:    providerID,
				}
				resp, err := tester.Models(context.Background(), creds)
				if err == nil {
					var modelsResp struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if err := json.Unmarshal(resp.Body, &modelsResp); err == nil && len(modelsResp.Data) > 0 {
						models := make([]string, 0, len(modelsResp.Data))
						for _, m := range modelsResp.Data {
							models = append(models, m.ID)
						}
						c.JSON(http.StatusOK, gin.H{"data": models})
						return
					}
					// Try parsing as flat array
					var flat []string
					if err2 := json.Unmarshal(resp.Body, &flat); err2 == nil && len(flat) > 0 {
						c.JSON(http.StatusOK, gin.H{"data": flat})
						return
					}
				} else {
					log.Printf("WARN: dynamic model list failed for %s, using static fallback: %v", providerID, err)
				}
			}
		}
	}

	// Fallback: return static model list for known providers
	c.JSON(http.StatusOK, gin.H{"data": staticModels(providerID)})
}

// TestModel tests a specific model by sending a minimal request.
// Builds provider-format-aware test bodies (e.g. Codex Responses API needs store:false + input format).
func (h *ModelHandler) TestModel(c *gin.Context) {
	providerID := c.Param("id")

	var req struct {
		Model string `json:"model" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	// Get provider
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	err := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, providerID).Scan(&provider.ID, &provider.Format, &provider.BaseURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Get executor
	exec, _, ok := h.registry.Get(provider.ID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no executor for provider"})
		return
	}

	// Get a ready connection
	var apiKey, accessToken string
	err = h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(oauth_token,'') FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&apiKey, &accessToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no ready connections"})
		return
	}

	// Build provider-format-aware test body
	bodyBytes := buildTestBody(provider.Format, req.Model)

	start := time.Now()
	resp, err := exec.Execute(c.Request.Context(), &executor.Request{
		APIKey:      apiKey,
		AccessToken: accessToken,
		BaseURL:     provider.BaseURL,
		Body:        bodyBytes,
		Provider:    providerID,
		Model:       req.Model,
	})
	latency := time.Since(start).Milliseconds()

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency,
		})
		return
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"status_code": resp.StatusCode,
			"latency_ms":  latency,
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"status":      "error",
			"status_code": resp.StatusCode,
			"error":       string(resp.Body),
			"latency_ms":  latency,
		})
	}
}

// buildTestBody constructs a minimal test request body matching the provider's API format.
// The executor handles stream/store flags internally; this only sets the payload shape.
func buildTestBody(format, model string) []byte {
	switch executor.ProviderFormat(format) {
	case executor.FormatOpenAIResponses:
		// Codex Responses API: input array format. Executor adds stream:true, store:false.
		body := map[string]any{
			"model": model,
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": []map[string]string{
					{"type": "input_text", "text": "Hi"},
				}},
			},
		}
		b, _ := json.Marshal(body)
		return b
	default:
		// OpenAI-compatible, Claude, Gemini, Antigravity, Kiro — standard chat body
		body := map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
			"max_tokens": 5,
		}
		b, _ := json.Marshal(body)
		return b
	}
}

// providerCatalogKeys maps DB provider IDs to models.json top-level keys.
// Providers NOT in this map (openai, groq, deepseek, mimo, opencode, openrouter, zai)
// have no static catalog — their models come from dynamic upstream /models API only.
var providerCatalogKeys = map[string][]string{
	"claude":      {"claude"},
	"gemini":      {"gemini"},
	"cx":          {"codex-free", "codex-team", "codex-plus", "codex-pro"},
	"ag":          {"antigravity"},
	"antigravity": {"antigravity"},
	"kiro":        {"kimi"},
	"aistudio":    {"aistudio"},
}

// staticModels returns model IDs from the auto-updating catalog (models.json + remote refresh).
// Returns empty for providers not in the catalog — those require dynamic upstream listing.
func staticModels(providerID string) []string {
	keys, ok := providerCatalogKeys[providerID]
	if !ok {
		return []string{}
	}
	return models.GetAllModelIDs(keys...)
}
