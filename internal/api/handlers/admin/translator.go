package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/tidwall/gjson"
)

// TranslatorHandler powers the dashboard Translator Debugger page. It replays
// the exact request pipeline the gateway uses: client request → source format →
// OpenAI intermediate → target request (URL/headers/body preview) → raw provider
// response → OpenAI response → client response.
//
// It is an admin/debugging surface: nothing here is on the /v1 hot path, and
// the "send" step deliberately bypasses failover/usage-tracking so a developer
// can inspect one raw provider round trip without perturbing production state.
type TranslatorHandler struct {
	db       *sql.DB
	registry *executor.Registry
	dataDir  string
	// sender executes a raw request against a provider connection and writes the
	// upstream body (SSE or JSON) back to the client. It is wired to the v1
	// Handler's DebugSend in the router so the debugger uses the same
	// credentials/proxy handling as the real gateway.
	sender func(c *gin.Context, provider, model string, body []byte)
}

// NewTranslatorHandler creates the translator debugger handler.
func NewTranslatorHandler(db *sql.DB, dataDir string, sender func(c *gin.Context, provider, model string, body []byte)) *TranslatorHandler {
	return &TranslatorHandler{db: db, registry: executor.GetRegistry(), dataDir: dataDir, sender: sender}
}

// allowedDebugFiles is the allowlist of files the debugger may load/save. The
// names match the 9router translator debugger's step files so saved sessions
// are portable across gateways.
var allowedDebugFiles = map[string]bool{
	"1_req_client.json":  true,
	"2_req_source.json":  true,
	"3_req_openai.json":  true,
	"4_req_target.json":  true,
	"5_res_provider.txt": true,
	"6_res_openai.txt":   true,
	"7_res_client.txt":   true,
	"7_res_client.json":  true,
}

// debugDir returns the translator debugger scratch directory under the data
// dir (logs/translator), creating it if needed.
func (h *TranslatorHandler) debugDir() string {
	dir := filepath.Join(h.dataDir, "logs", "translator")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// detectFormat mirrors 9router's open-sse/services/provider.js detectFormat:
// it classifies a raw client body into one of the translator format strings.
func detectFormat(body []byte) string {
	if !gjson.ValidBytes(body) {
		return "openai"
	}
	b := gjson.ParseBytes(body)

	// OpenAI Responses API: has input (array or string) instead of messages[]
	if b.Get("input").Exists() && !b.Get("messages").Exists() {
		return "openai-responses"
	}

	// Gemini format: has contents array
	if b.Get("contents").IsArray() {
		return "gemini"
	}

	// OpenAI-specific indicators checked before Claude.
	if b.Get("stream_options").Exists() ||
		b.Get("response_format").Exists() ||
		b.Get("logprobs").Exists() ||
		b.Get("top_logprobs").Exists() ||
		b.Get("n").Exists() ||
		b.Get("presence_penalty").Exists() ||
		b.Get("frequency_penalty").Exists() ||
		b.Get("logit_bias").Exists() ||
		b.Get("user").Exists() {
		return "openai"
	}

	// Claude format: messages with content as array of objects with type.
	if b.Get("messages").IsArray() {
		if b.Get("system").Exists() || b.Get("anthropic_version").Exists() {
			return "claude"
		}
		first := b.Get("messages.0.content")
		if first.IsArray() {
			types := map[string]bool{}
			first.ForEach(func(_, v gjson.Result) bool {
				types[v.Get("type").String()] = true
				return true
			})
			if types["tool_use"] || types["tool_result"] {
				return "claude"
			}
			if types["image"] { // Claude image uses source.type
				return "claude"
			}
			if types["image_url"] { // OpenAI image uses image_url.url
				return "openai"
			}
		}
	}

	// Default to OpenAI format.
	return "openai"
}

// sourceFormat returns the source format based on body plus the client endpoint
// path (the gateway resolves /v1/messages → claude, /v1/responses →
// openai-responses before translation). path may be empty.
func sourceFormat(path string, body []byte) string {
	switch {
	case strings.HasSuffix(path, "/v1/messages") || strings.HasSuffix(path, "/messages"):
		return "claude"
	case strings.HasSuffix(path, "/v1/responses") || strings.HasSuffix(path, "/responses"):
		return "openai-responses"
	default:
		return detectFormat(body)
	}
}

// connectionCreds is the subset of connection data the debugger needs to build
// an upstream request preview and to execute a send.
type connectionCreds struct {
	ID          string
	Name        string
	APIKey      string
	AccessToken string
	BaseURL     string
	PSD         map[string]string
}

// findActiveConnection loads the highest-priority active connection for a
// provider, mirroring the gateway's credential resolution. Returns nil when no
// usable connection exists (the debugger surfaces a friendly error).
func (h *TranslatorHandler) findActiveConnection(provider string) (*connectionCreds, error) {
	rows, err := h.db.Query(`
		SELECT c.id, c.name,
			COALESCE(c.api_key, '') AS api_key,
			COALESCE(c.oauth_token, '') AS oauth_token,
			COALESCE(pt.base_url, '') AS base_url,
			COALESCE(c.provider_specific_data, '') AS psd
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.provider_type_id = ? AND c.is_active = 1
		ORDER BY COALESCE(c.priority, 0) DESC, c.created_at DESC
		LIMIT 1`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	var creds connectionCreds
	var psd string
	if err := rows.Scan(&creds.ID, &creds.Name, &creds.APIKey, &creds.AccessToken, &creds.BaseURL, &psd); err != nil {
		return nil, err
	}
	if psd != "" {
		_ = json.Unmarshal([]byte(psd), &creds.PSD)
	}
	return &creds, nil
}

// Translate handles POST /api/admin/translator/translate.
//
// Steps:
//  1. detect  — {provider, model, sourceFormat, targetFormat} from a client body
//  2. toOpenai — source → openai intermediate translation
//  3. toTarget — openai → target translation + built URL/headers/body preview
func (h *TranslatorHandler) Translate(c *gin.Context) {
	var payload struct {
		Step  int             `json:"step"`
		Path  string          `json:"path"`
		Body  json.RawMessage `json:"body"`
		Model string          `json:"model"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
		return
	}
	if len(payload.Body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "body required"})
		return
	}

	model := payload.Model
	if model == "" {
		model = gjson.GetBytes(payload.Body, "model").String()
	}
	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "model must include provider prefix (e.g. openai/gpt-4o)"})
		return
	}

	switch payload.Step {
	case 1:
		_, targetFormat, ok := h.registry.Get(provider)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "unknown provider: " + provider})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"result": gin.H{
				"provider":     provider,
				"model":        modelName,
				"sourceFormat": sourceFormat(payload.Path, payload.Body),
				"targetFormat": string(targetFormat),
			},
		})

	case 2:
		// source → OpenAI intermediate
		src := sourceFormat(payload.Path, payload.Body)
		stream := executor.IsStreamRequest(payload.Body)
		translated := registry.Request(src, "openai", modelName, payload.Body, stream)
		c.JSON(http.StatusOK, gin.H{"success": true, "result": gin.H{"body": json.RawMessage(translated)}})

	case 3:
		// OpenAI intermediate → target + URL/headers/body preview
		_, targetFormat, ok := h.registry.Get(provider)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "unknown provider: " + provider})
			return
		}
		stream := executor.IsStreamRequest(payload.Body)
		translated := registry.Request("openai", string(targetFormat), modelName, payload.Body, stream)

		creds, err := h.findActiveConnection(provider)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if creds == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "no active connection for provider: " + provider,
			})
			return
		}

		built, err := executor.BuildUpstreamRequest(&executor.Request{
			Model:                modelName,
			Body:                 translated,
			Stream:               stream,
			APIKey:               creds.APIKey,
			AccessToken:          creds.AccessToken,
			BaseURL:              creds.BaseURL,
			Provider:             provider,
			ProviderSpecificData: creds.PSD,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"result": gin.H{
				"provider": provider,
				"model":    modelName,
				"format":   string(targetFormat),
				"url":      built.URL,
				"headers":  built.Headers,
				"body":     json.RawMessage(built.Body),
				"conn": gin.H{
					"id":   creds.ID,
					"name": creds.Name,
				},
			},
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid step (1-3)"})
	}
}

// Send handles POST /api/admin/translator/send. It executes the built target
// request against the provider and streams the raw response body back to the
// client (SSE or JSON bytes, verbatim). Delegates to the v1 handler so the
// debugger exercises the same executor/proxy path as the live gateway.
func (h *TranslatorHandler) Send(c *gin.Context) {
	var payload struct {
		Provider string          `json:"provider"`
		Model    string          `json:"model"`
		Body     json.RawMessage `json:"body"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
		return
	}
	if payload.Provider == "" || payload.Model == "" || len(payload.Body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "provider, model, and body required"})
		return
	}
	if h.sender == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "translator send is not wired"})
		return
	}
	h.sender(c, payload.Provider, payload.Model, payload.Body)
}

// Load returns the contents of a saved debugger file.
// GET /api/admin/translator/load?name=1_req_client.json
func (h *TranslatorHandler) Load(c *gin.Context) {
	name := c.Query("name")
	if !allowedDebugFiles[name] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid file name"})
		return
	}
	content, err := os.ReadFile(filepath.Join(h.debugDir(), name))
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"success": true, "name": name, "content": ""})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "name": name, "content": string(content)})
}

// Save writes a debugger step file to the allowlisted logs/translator dir.
// POST /api/admin/translator/save  {name, content}
func (h *TranslatorHandler) Save(c *gin.Context) {
	var payload struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
		return
	}
	if !allowedDebugFiles[payload.Name] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid file name"})
		return
	}
	path := filepath.Join(h.debugDir(), payload.Name)
	if err := os.WriteFile(path, []byte(payload.Content), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "name": payload.Name})
}
