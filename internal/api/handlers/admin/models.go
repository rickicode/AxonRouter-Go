package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	providerpkg "github.com/rickicode/AxonRouter-Go/internal/provider"
)

// ModelHandler handles model-related admin endpoints.
type ModelHandler struct {
	db       *sql.DB
	registry *executor.Registry
	store    *connstate.Store
	authMgr  *auth.Manager
}

// NewModelHandler creates a new model handler.
func NewModelHandler(db *sql.DB, registry *executor.Registry, store *connstate.Store, authMgr *auth.Manager) *ModelHandler {
	return &ModelHandler{db: db, registry: registry, store: store, authMgr: authMgr}
}

// noAuthBaseURLs maps no-auth provider IDs to their base URLs.
// Used for testing and model listing when no DB entry or connection exists.
var noAuthBaseURLs = map[string]string{
	"oc":            "https://opencode.ai/zen/v1",
	"mimocode":      "https://api.xiaomimimo.com/api/free-ai/openai",
	"mimocode-free": "https://api.xiaomimimo.com/api/free-ai/openai",
}

// ListModels returns available models for a provider.
// Priority: (1) dynamic upstream query via executor, (2) static/synced catalog.
// Works even when the provider has no DB entry (fresh install) — falls back to catalog.
func (h *ModelHandler) ListModels(c *gin.Context) {
	providerID := c.Param("id")
	stored := h.storedModels(providerID)
	if _, ok := noAuthBaseURLs[providerID]; ok {
		// No-auth providers use the static/synced catalog. Their chat base URL
		// is not necessarily the /models URL, so dynamic executor probing can
		// hit HTML/404 pages (opencode.ai/zen/v1) and spam warnings.
		c.JSON(http.StatusOK, gin.H{"data": h.listModelEntries(providerID, stored, staticModels(providerID), nil)})
		return
	}

	// Try to get provider from DB (may not exist on fresh install)
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	dbErr := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, providerID).Scan(&provider.ID, &provider.Format, &provider.BaseURL)

	// Cloudflare-specific discovery: the OpenAI-flavored /v1/models endpoint is not
	// supported by Workers AI; the canonical list lives at
	// /accounts/{accountId}/ai/models/search. Match OmniRoute's behavior.
	if providerID == "cf" && dbErr == nil {
		var cfAPIKey, cfPSD string
		err := h.db.QueryRow(`SELECT COALESCE(api_key,''), provider_specific_data FROM connections WHERE provider_type_id = ? AND status IN ('ready','degraded') AND is_active = 1 LIMIT 1`, providerID).Scan(&cfAPIKey, &cfPSD)
		if err == nil && cfAPIKey != "" {
			psd := make(map[string]string)
			if cfPSD != "" {
				json.Unmarshal([]byte(cfPSD), &psd)
			}
			if accountID := psd["accountId"]; accountID != "" {
				if cfModels, cfKinds, err := models.FetchCloudflareModels(cfAPIKey, accountID); err == nil && len(cfModels) > 0 {
					// Merge discovered CF entries into the shared catalog so /v1/models reflects them.
					models.MergeProviderModelIDs("cf", cfModels, cfKinds)
					c.JSON(http.StatusOK, gin.H{"data": h.listModelEntries(providerID, stored, cfModels, cfKinds)})
					return
				} else if err != nil {
					logging.Logger.Debug("cloudflare model discovery failed", "provider", providerID, "err", err.Error())
				}
			}
		}
	}

	// Try dynamic model listing via executor (only if provider exists in DB)
	if dbErr == nil {
		exec, _, ok := h.registry.Get(provider.ID)
		if ok {
			if tester, ok := exec.(modelTester); ok {
				var apiKey, accessToken string
				var psdJSON sql.NullString
				err := h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(oauth_token,''), provider_specific_data FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&apiKey, &accessToken, &psdJSON)
				if err == nil {
					// Parse provider_specific_data for {accountId} resolution
					psd := make(map[string]string)
					if psdJSON.Valid && psdJSON.String != "" {
						json.Unmarshal([]byte(psdJSON.String), &psd)
					}
					creds := &executor.Request{
						APIKey:               apiKey,
						AccessToken:          accessToken,
						BaseURL:              provider.BaseURL,
						Provider:             providerID,
						ProviderSpecificData: psd,
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
								models = append(models, strings.TrimPrefix(m.ID, "@"))
							}
							c.JSON(http.StatusOK, gin.H{"data": h.listModelEntries(providerID, stored, models, nil)})
							return
						}
						var flat []string
						if err2 := json.Unmarshal(resp.Body, &flat); err2 == nil && len(flat) > 0 {
							stripped := make([]string, len(flat))
							for i, m := range flat {
								stripped[i] = strings.TrimPrefix(m, "@")
							}
							c.JSON(http.StatusOK, gin.H{"data": h.listModelEntries(providerID, stored, stripped, nil)})
						}
					} else {
						logging.Logger.Debug("dynamic model list failed, using static", "provider", providerID, "err", err)
					}
				}
			}
		}
	}

	// Fallback: return static/synced model list from catalog, merged with stored models.
	c.JSON(http.StatusOK, gin.H{"data": h.listModelEntries(providerID, stored, staticModels(providerID), nil)})
}

// storedModels returns user-added custom models persisted for a provider.
func (h *ModelHandler) storedModels(providerID string) []string {
	rows, err := h.db.Query(`SELECT model FROM provider_models WHERE provider_type_id = ? ORDER BY model`, providerID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var m string
		if rows.Scan(&m) == nil && m != "" {
			out = append(out, m)
		}
	}
	return out
}

// listModelEntries builds the model list response for a provider. Catalog/upstream
// models are not user-deletable (custom=false); user-added models are custom=true.
// Every id is prefixed with the provider alias (e.g. "oc/gpt-4o") so it matches the
// ids served by /v1/models.
func (h *ModelHandler) listModelEntries(providerID string, stored []string, extra []string, extraKinds map[string][]string) []gin.H {
	seen := make(map[string]bool)
	var entries []gin.H
	keys := providerCatalogKeys[providerID]
	add := func(raw string, custom bool, kindsOverride []string) {
		id := prefixedModelID(providerID, raw)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		entry := gin.H{"id": id, "custom": custom}
		if len(kindsOverride) > 0 {
			entry["service_kinds"] = kindsOverride
		} else {
			for _, key := range keys {
				if kinds := models.GetModelServiceKinds(key, raw); len(kinds) > 0 {
					entry["service_kinds"] = kinds
					break
				}
			}
		}
		entries = append(entries, entry)
	}
	// Custom entries first so any collision with a static id upgrades to custom.
	for _, m := range stored {
		add(m, true, nil)
	}
	for _, m := range extra {
		add(m, false, extraKinds[m])
	}
	return entries
}

// prefixedModelID returns the display model id for a provider, prefixed with the
// provider alias (e.g. "oc/gpt-4o") so it matches the ids served by /v1/models.
func prefixedModelID(providerID, raw string) string {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "@")
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, providerID+"/") {
		return providerID + "/" + raw
	}
	return raw
}

// CreateModel adds a user-defined model to a custom provider.
func (h *ModelHandler) CreateModel(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		Model string `json:"model" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	// Strip any provider alias prefix (e.g. "oc/") so the raw model name is stored
	// consistently regardless of how the client sends it.
	model := strings.TrimPrefix(strings.TrimSpace(req.Model), providerID+"/")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_models (provider_type_id, model, created_at) VALUES (?, ?, ?)`, providerID, model, time.Now().Unix()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.storedModels(providerID)})
}

// DeleteModel removes a user-defined model from a custom provider.
func (h *ModelHandler) DeleteModel(c *gin.Context) {
	providerID := c.Param("id")
	model := c.Query("model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	model = strings.TrimPrefix(model, providerID+"/")
	if _, err := h.db.Exec(`DELETE FROM provider_models WHERE provider_type_id = ? AND model = ?`, providerID, model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.storedModels(providerID)})
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
	var providerSpecificData map[string]string
	baseURL := provider.BaseURL
	format := provider.Format

	if dbErr == nil {
		var psdJSON sql.NullString
		var refreshToken sql.NullString
		var expiresAt int64
		var connID string
		err := h.db.QueryRow(`SELECT id, COALESCE(api_key,''), COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data,'') FROM connections WHERE provider_type_id = ? AND status = 'ready' AND is_active = 1 LIMIT 1`, providerID).Scan(&connID, &apiKey, &accessToken, &refreshToken, &expiresAt, &psdJSON)
		if err != nil {
			// No connection — check if this is a no-auth provider
			if noAuthURL, ok := noAuthBaseURLs[providerID]; ok {
				baseURL = noAuthURL
				format = "openai"
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "no ready connections"})
				return
			}
		} else {
			if psdJSON.Valid && psdJSON.String != "" {
				providerSpecificData = make(map[string]string)
				json.Unmarshal([]byte(psdJSON.String), &providerSpecificData)
			}
			// Refresh OAuth token if expired
			if accessToken != "" && expiresAt > 0 && time.Now().Unix() > expiresAt-30 && refreshToken.Valid && refreshToken.String != "" && h.authMgr != nil {
				creds := &auth.Credentials{AccessToken: accessToken, RefreshToken: refreshToken.String, ExpiresAt: time.Unix(expiresAt, 0)}
				newCreds, err := h.authMgr.RefreshToken(c.Request.Context(), auth.ProviderType(providerID), creds)
				if err != nil {
					logging.Logger.Debug("OAuth refresh failed", "conn", connID, "err", err)
				} else {
					accessToken = newCreds.AccessToken
					h.db.Exec(`UPDATE connections SET oauth_token = ?, oauth_expires_at = ?, updated_at = ? WHERE id = ?`,
						newCreds.AccessToken, newCreds.ExpiresAt.Unix(), time.Now().Unix(), connID)
				}
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

	modelName := req.Model
	// Strip the provider alias prefix (e.g. "oc/") so the upstream receives the
	// real model name. Mirrors how /v1 routing resolves provider/model-id.
	modelName = strings.TrimPrefix(modelName, providerID+"/")

	start := time.Now()
	testReq := &executor.Request{
		APIKey:               apiKey,
		AccessToken:          accessToken,
		BaseURL:              baseURL,
		Provider:             providerID,
		Model:                modelName,
		ProviderSpecificData: providerSpecificData,
	}

	modelServiceKinds := models.GetModelServiceKinds(providerID, modelName)

	// Cloudflare Workers AI only has OpenAI-compatible endpoints for chat and embeddings.
	// Image models and some embedding models must use the native /ai/run/{model} endpoint.
	if providerID == "cf" && (providerpkg.HasServiceKind(modelServiceKinds, providerpkg.ServiceKindImage) || providerpkg.HasServiceKind(modelServiceKinds, providerpkg.ServiceKindEmbedding)) {
		accountID := providerSpecificData["accountId"]
		if accountID == "" {
			accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		if accountID == "" {
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": "Cloudflare Workers AI requires an Account ID", "latency_ms": time.Since(start).Milliseconds()})
			return
		}
		var body []byte
		var kind string
		if providerpkg.HasServiceKind(modelServiceKinds, providerpkg.ServiceKindImage) {
			body = []byte(`{"prompt":"A simple test image"}`)
			kind = "image"
		} else {
			body = []byte(`{"text":"Hello World"}`)
			kind = "embedding"
		}
		resp, err := runCloudflareModelTest(accountID, apiKey, modelName, body)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": err.Error(), "latency_ms": latency})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			msg := fmt.Sprintf("CF %s test failed: HTTP %d", kind, resp.StatusCode)
			if body, readErr := io.ReadAll(resp.Body); readErr == nil && len(body) > 0 {
				msg = string(body)
			}
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": msg, "latency_ms": latency})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "status_code": resp.StatusCode, "latency_ms": latency})
		return
	}

	switch {
	case providerpkg.HasServiceKind(modelServiceKinds, providerpkg.ServiceKindImage):
		body := map[string]any{"model": modelName, "prompt": "A simple test image", "n": 1}
		bodyBytes, _ := json.Marshal(body)
		testReq.Body = bodyBytes
		imgGen, ok := exec.(executor.ImageGenerator)
		if !ok {
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": "provider does not support image generation", "latency_ms": time.Since(start).Milliseconds()})
			return
		}
		resp, err := imgGen.Images(c.Request.Context(), testReq)
		latency := time.Since(start).Milliseconds()
		if err != nil || resp == nil || resp.StatusCode >= 400 {
			msg := "image test failed"
			if err != nil {
				msg = err.Error()
			} else if resp != nil {
				msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": msg, "latency_ms": latency})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "status_code": resp.StatusCode, "latency_ms": latency})
		return
	case providerpkg.HasServiceKind(modelServiceKinds, providerpkg.ServiceKindEmbedding):
		body := map[string]any{"model": modelName, "input": "Hi"}
		bodyBytes, _ := json.Marshal(body)
		testReq.Body = bodyBytes
		embExec, ok := exec.(executor.EmbeddingsExecutor)
		if !ok {
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": "provider does not support embeddings", "latency_ms": time.Since(start).Milliseconds()})
			return
		}
		resp, err := embExec.Embeddings(c.Request.Context(), testReq)
		latency := time.Since(start).Milliseconds()
		if err != nil || resp == nil || resp.StatusCode >= 400 {
			msg := "embedding test failed"
			if err != nil {
				msg = err.Error()
			} else if resp != nil {
				msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			c.JSON(http.StatusOK, gin.H{"status": "error", "error": msg, "latency_ms": latency})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "status_code": resp.StatusCode, "latency_ms": latency})
		return
	}

	testReq.Body = buildTestBody(format, modelName)
	streamResult, err := exec.ExecuteStream(c.Request.Context(), testReq)
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
	"kiro":          {"kiro"},
	"aistudio":      {"aistudio"},
	"xai":           {"xai"},
	"oc":            {"oc"},
	"oc-zen":        {"oc-zen"},
	"oc-go":         {"oc-go"},
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
	"cf":            {"cf"},
	"glm":           {"glm"},
	"minimax":       {"minimax"},
	"kimi":          {"kimi"},
	"mistral":       {"mistral"},
	"cerebras":      {"cerebras"},
	"together":      {"together"},
	"fireworks":     {"fireworks"},
	"novita":        {"novita"},
	"lambda": {"lambda"},
	"pollinations": {"pollinations"},
}

// staticModels returns model IDs from the auto-updating catalog, stripped of leading "@".
func staticModels(providerID string) []string {
	keys, ok := providerCatalogKeys[providerID]
	if !ok {
		return []string{}
	}
	ids := models.GetAllModelIDs(keys...)
	stripped := make([]string, len(ids))
	for i, id := range ids {
		stripped[i] = strings.TrimPrefix(id, "@")
	}
	return stripped
}

// defaultTestModel returns the first available model for a provider from the catalog.
// For Cloudflare, the catalog stores gateway IDs like cf/author/model; the test
// body should contain only the upstream model name (author/model) so the CF
// sanitizer can prepend @cf/ exactly once. Prefer an LLM model for CF so the
// default test body is appropriate for chat completions.
func defaultTestModel(providerID string) string {
	ids := staticModels(providerID)
	if len(ids) > 0 {
		if providerID == "cf" {
			// Test only an LLM model; CF image/embedding endpoints need different payloads.
			return "qwen/qwq-32b"
		}
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
	case "oc", "oc-zen", "oc-go":
		return "deepseek-v4-flash-free"
	case "openrouter":
		return "openai/gpt-4o"
	case "cerebras":
		return "gpt-oss-120b"
	case "together":
		return "MiniMaxAI/MiniMax-M3"
	case "fireworks":
		return "accounts/fireworks/models/llama-v3p1-8b-instruct"
	case "novita":
		return "meta-llama/llama-3.1-8b-instruct"
	case "lambda":
		return "llama3.1-8b-instruct"
	case "pollinations":
		return "openai"
	default:
		return ""
	}
}

// runCloudflareModelTest calls the Cloudflare Workers AI /ai/run/{model} endpoint
// directly. This is needed for modalities that do not have an OpenAI-compatible
// endpoint (e.g. text-to-image) and for embeddings where the native run endpoint
// expects a different payload shape than /v1/embeddings.
func runCloudflareModelTest(accountID, apiKey, modelName string, body []byte) (*http.Response, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/@cf/%s", accountID, modelName)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

// SyncModels triggers an immediate sync of per-provider models from upstream endpoints.
func (h *ModelHandler) SyncModels(c *gin.Context) {
	models.SyncNow(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"message": "models synced successfully"})
}
