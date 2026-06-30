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

	// Build minimal test request
	testBody := map[string]any{
		"model":      req.Model,
		"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
		"max_tokens": 5,
	}
	bodyBytes, _ := json.Marshal(testBody)

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

// staticModels returns a static model list for known provider types.
// ponytail: hardcoded per provider, update when new models ship.
// Covers both canonical IDs and DB-stored IDs.
func staticModels(providerID string) []string {
	switch providerID {
	case "openai":
		return []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "o1", "o1-mini", "o1-pro", "o3", "o3-mini", "o4-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano"}
	case "claude":
		return []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022", "claude-3-opus-20240229"}
	case "gemini":
		return []string{"gemini-2.5-pro-preview-05-06", "gemini-2.5-flash-preview-05-20", "gemini-2.0-flash", "gemini-1.5-pro", "gemini-1.5-flash"}
	case "cx":
		return []string{"gpt-5.4", "gpt-5.4-mini", "o3", "o4-mini", "codex-mini"}
	case "ag", "antigravity":
		return []string{"gemini-2.5-pro", "gemini-2.5-flash"}
	case "kiro":
		return []string{"claude-sonnet-4", "claude-3-5-sonnet"}
	case "groq":
		return []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768"}
	case "deepseek":
		return []string{"deepseek-chat", "deepseek-reasoner"}
	case "mimo", "mimocode", "mimo-tp", "mimocode-free", "mimo-token":
		return []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-pro", "mimo-v2-omni", "mimo-v2-flash"}
	case "opencode", "oc", "oc-zen", "oc-go", "opencode-go", "opencode-zen":
		return []string{"kimi-k2", "glm-4", "qwen-2.5-72b"}
	case "openrouter":
		return []string{"openai/gpt-4o", "anthropic/claude-sonnet-4", "google/gemini-2.5-pro", "deepseek/deepseek-chat", "meta-llama/llama-3.3-70b-instruct"}
	case "zai":
		return []string{"glm-4-plus", "glm-4-flash", "glm-4-long"}
	case "elevenlabs":
		return []string{"eleven_multilingual_v2", "eleven_turbo_v2_5", "eleven_monolingual_v1"}
	case "deepgram":
		return []string{"nova-2", "nova-2-medical", "whisper-large"}
	default:
		return []string{}
	}
}
