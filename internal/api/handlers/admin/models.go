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

// noAuthBaseURLs maps no-auth provider IDs to their base URLs.
// Used for testing and model listing when no DB entry or connection exists.
var noAuthBaseURLs = map[string]string{
	"opencode":      "https://opencode.ai/zen/v1",
	"opencode-free": "https://opencode.ai/zen/v1",
	"oc":            "https://opencode.ai/zen/v1",
	"mimocode":      "https://api.xiaomimimo.com/api/free-ai/openai",
	"mimocode-free": "https://api.xiaomimimo.com/api/free-ai/openai",
}

// ListModels returns available models for a provider.
// Priority: (1) dynamic upstream query via executor, (2) static/synced catalog.
// Works even when the provider has no DB entry (fresh install) — falls back to catalog.
func (h *ModelHandler) ListModels(c *gin.Context) {
	providerID := c.Param("id")

	// Try to get provider from DB (may not exist on fresh install)
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	dbErr := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, providerID).Scan(&provider.ID, &provider.Format, &provider.BaseURL)

	// Try dynamic model listing via executor (only if provider exists in DB)
	if dbErr == nil {
		exec, _, ok := h.registry.Get(provider.ID)
		if ok {
			if tester, ok := exec.(modelTester); ok {
				var apiKey, accessToken string
				err := h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(oauth_token,'') FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&apiKey, &accessToken)
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
	}

	// Fallback: return static/synced model list from catalog
	c.JSON(http.StatusOK, gin.H{"data": staticModels(providerID)})
}

// TestModel tests a specific model by sending a minimal streaming request.
// For no-auth providers (opencode, mimocode), tests without requiring a connection.
func (h *ModelHandler) TestModel(c *gin.Context) {
	providerID := c.Param("id")

	var req struct {
		Model string `json:"model" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	// Get provider from DB (may not exist for no-auth providers)
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	dbErr := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, providerID).Scan(&provider.ID, &provider.Format, &provider.BaseURL)

	// Resolve executor — try DB provider ID first, then the raw ID
	executorID := providerID
	if dbErr == nil {
		executorID = provider.ID
	}
	exec, _, ok := h.registry.Get(executorID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no executor for provider"})
		return
	}

	// Get credentials: try connection first, fall back to no-auth
	var apiKey, accessToken string
	baseURL := provider.BaseURL
	format := provider.Format

	if dbErr == nil {
		err := h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(oauth_token,'') FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&apiKey, &accessToken)
		if err != nil {
			// No connection — check if this is a no-auth provider
			if noAuthURL, ok := noAuthBaseURLs[providerID]; ok {
				baseURL = noAuthURL
				format = "openai"
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "no ready connections"})
				return
			}
		}
	} else {
		// Provider not in DB — check if this is a no-auth provider
		if noAuthURL, ok := noAuthBaseURLs[providerID]; ok {
			baseURL = noAuthURL
			format = "openai"
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}
	}

	bodyBytes := buildTestBody(format, req.Model)

	start := time.Now()
	streamResult, err := exec.ExecuteStream(c.Request.Context(), &executor.Request{
		APIKey:      apiKey,
		AccessToken: accessToken,
		BaseURL:     baseURL,
		Body:        bodyBytes,
		Provider:    providerID,
		Model:       req.Model,
	})
	if err != nil {
		latency := time.Since(start).Milliseconds()
		c.JSON(http.StatusOK, gin.H{
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency,
		})
		return
	}

	var firstErr error
	var gotChunk bool
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			firstErr = chunk.Err
			break
		}
		if !gotChunk && chunk.Payload != nil {
			gotChunk = true
		}
	}
	latency := time.Since(start).Milliseconds()

	if firstErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":     "error",
			"error":      firstErr.Error(),
			"latency_ms": latency,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"status_code": streamResult.StatusCode,
		"latency_ms":  latency,
	})
}

// buildTestBody constructs a minimal test request body matching the provider's native API format.
func buildTestBody(format, model string) []byte {
	switch executor.ProviderFormat(format) {
	case executor.FormatOpenAIResponses:
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
	case executor.FormatClaude:
		body := map[string]any{
			"model":      model,
			"max_tokens": 5,
			"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
		}
		b, _ := json.Marshal(body)
		return b
	case executor.FormatGemini, executor.FormatAntigravity:
		body := map[string]any{
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "Hi"}}},
			},
			"generationConfig": map[string]any{"maxOutputTokens": 5},
		}
		b, _ := json.Marshal(body)
		return b
	default:
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
var providerCatalogKeys = map[string][]string{
	"claude":        {"claude"},
	"gemini":        {"gemini"},
	"vertex":        {"vertex"},
	"cx":            {"codex-free", "codex-team", "codex-plus", "codex-pro"},
	"ag":            {"antigravity"},
	"antigravity":   {"antigravity"},
	"kiro":          {"kimi"},
	"aistudio":      {"aistudio"},
	"xai":           {"xai"},
	"opencode":      {"opencode"},
	"opencode-free": {"opencode"},
	"oc":            {"opencode"},
	"oc-zen":        {"opencode"},
	"oc-go":         {"opencode"},
	"mimocode":      {"mimocode"},
	"mimocode-free": {"mimocode"},
	"mimo":          {"mimocode"},
	"mimo-tp":       {"mimocode"},
	"mimo-token":    {"mimocode"},
	"openai":        {"openai"},
	"groq":          {"groq"},
	"deepseek":      {"deepseek"},
	"openrouter":    {"openrouter"},
	"zai":           {"claude"},
}

// staticModels returns model IDs from the auto-updating catalog.
func staticModels(providerID string) []string {
	keys, ok := providerCatalogKeys[providerID]
	if !ok {
		return []string{}
	}
	return models.GetAllModelIDs(keys...)
}

// defaultTestModel returns the first available model for a provider from the catalog.
func defaultTestModel(providerID string) string {
	if ids := staticModels(providerID); len(ids) > 0 {
		return ids[0]
	}
	switch providerID {
	case "openai":
		return "gpt-4o"
	case "groq":
		return "llama-3.3-70b-versatile"
	case "deepseek":
		return "deepseek-chat"
	case "mimo", "mimocode", "mimocode-free", "mimo-tp", "mimo-token":
		return "mimo-auto"
	case "opencode", "oc", "oc-zen", "oc-go", "opencode-go", "opencode-zen", "opencode-free":
		return "deepseek-v4-flash-free"
	case "openrouter":
		return "openai/gpt-4o"
	default:
		return ""
	}
}
