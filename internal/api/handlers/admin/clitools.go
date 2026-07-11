package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

const cliToolKeyPrefix = "clitools:"

// CLIToolStatic holds the catalog metadata for one supported CLI agent.
type CLIToolStatic struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	ConfigKind  string `json:"configKind"`
	DocsURL     string `json:"docsUrl"`
}

// CLIToolSelection is what we persist and what the frontend submits.
type CLIToolSelection struct {
	Model    string `json:"model"`
	APIKeyID string `json:"apiKeyId"`
	BaseURL  string `json:"baseUrl"`
}

// CLIToolConfig is the tool-specific output shown to the user.
type CLIToolConfig struct {
	EnvBlock      string `json:"envBlock"`
	ConfigPath    string `json:"configPath"`
	ConfigContent string `json:"configContent"`
	RunCommand    string `json:"runCommand"`
}

// CLIToolsHandler manages the dashboard CLI tools feature.
type CLIToolsHandler struct {
	db          *sql.DB
	modelLister func() []map[string]string
}

// NewCLIToolsHandler creates a CLI Tools handler.
// modelLister should return the same unified catalog used by /v1/models.
func NewCLIToolsHandler(database *sql.DB, modelLister func() []map[string]string) *CLIToolsHandler {
	return &CLIToolsHandler{
		db:          database,
		modelLister: modelLister,
	}
}

// ListTools returns the supported CLI tool catalog. No secrets.
func (h *CLIToolsHandler) ListTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": cliToolCatalog})
}

// GetConfig returns the persisted selection for a tool, plus the gateway base URL default.
func (h *CLIToolsHandler) GetConfig(c *gin.Context) {
	toolID := c.Param("toolId")
	if !isKnownTool(toolID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown cli tool"})
		return
	}

	raw := db.GetSetting(cliToolKeyPrefix+toolID, "")
	var sel CLIToolSelection
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &sel)
	}

	c.JSON(http.StatusOK, gin.H{
		"tool":          findTool(toolID),
		"selection":     sel,
		"defaultBaseUrl": defaultBaseURL(c),
	})
}

// SaveConfig validates a selection, persists it, and emits the generated config.
// apiKeyValue is supplied by the user each time; it is embedded in the response
// but never stored. axonrouter only stores bcrypt hashes of API keys.
func (h *CLIToolsHandler) SaveConfig(c *gin.Context) {
	toolID := c.Param("toolId")
	if !isKnownTool(toolID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown cli tool"})
		return
	}

	var req struct {
		CLIToolSelection
		APIKeyValue string `json:"apiKeyValue"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL(c)
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	if !h.isModelAvailable(req.Model) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selected model is not available on this gateway"})
		return
	}

	if req.APIKeyID != "" {
		if !h.isAPIKeyActive(req.APIKeyID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "selected api key is not active"})
			return
		}
	}

	sel := CLIToolSelection{
		Model:    req.Model,
		APIKeyID: req.APIKeyID,
		BaseURL:  baseURL,
	}
	selJSON, err := json.Marshal(sel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal selection"})
		return
	}
	if err := db.SetSetting(cliToolKeyPrefix+toolID, string(selJSON)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	apiKey := strings.TrimSpace(req.APIKeyValue)
	if apiKey == "" {
		apiKey = "__YOUR_AXONROUTER_API_KEY__"
	}

	cfg := generateConfig(toolID, sel, apiKey)
	c.JSON(http.StatusOK, gin.H{
		"selection": sel,
		"config":    cfg,
	})
}

func (h *CLIToolsHandler) isModelAvailable(modelID string) bool {
	for _, m := range h.modelLister() {
		if m["id"] == modelID {
			return true
		}
	}
	return false
}

func (h *CLIToolsHandler) isAPIKeyActive(id string) bool {
	var isActive int
	err := h.db.QueryRow(`SELECT is_active FROM api_keys WHERE id = ?`, id).Scan(&isActive)
	return err == nil && isActive == 1
}

func defaultBaseURL(c *gin.Context) string {
	// Prefer a full gateway base URL the dashboard browser already uses.
	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		host = "localhost:3777"
	}
	return proto + "://" + host + "/v1"
}

func isKnownTool(id string) bool {
	for _, t := range cliToolCatalog {
		if t.ID == id {
			return true
		}
	}
	return false
}

func findTool(id string) *CLIToolStatic {
	for _, t := range cliToolCatalog {
		if t.ID == id {
			return &t
		}
	}
	return nil
}

// cliToolCatalog is the curated agent/CLI catalog. Add more tools here when needed.
var cliToolCatalog = []CLIToolStatic{
	{
		ID:          "claude",
		Name:        "Claude Code",
		Description: "Anthropic's agentic coding assistant.",
		Icon:        "Bot",
		ConfigKind:  "json",
		DocsURL:     "https://docs.anthropic.com/en/docs/claude-code/overview",
	},
	{
		ID:          "codex",
		Name:        "OpenAI Codex",
		Description: "OpenAI's CLI agent for coding tasks.",
		Icon:        "SquareTerminal",
		ConfigKind:  "toml",
		DocsURL:     "https://github.com/openai/codex",
	},
	{
		ID:          "cline",
		Name:        "Cline",
		Description: "OpenAI-compatible VS Code extension.",
		Icon:        "Sparkles",
		ConfigKind:  "json",
		DocsURL:     "https://github.com/cline/cline",
	},
	{
		ID:          "kilo",
		Name:        "Kilo Code",
		Description: "Cline-based fork for OpenAI-compatible endpoints.",
		Icon:        "Boxes",
		ConfigKind:  "json",
		DocsURL:     "https://github.com/kilo-ai/kilo-code",
	},
	{
		ID:          "openclaw",
		Name:        "OpenClaw",
		Description: "Pluggable CLI agent with provider profiles.",
		Icon:        "Orbit",
		ConfigKind:  "json",
		DocsURL:     "https://github.com/openclawhq/openclaw",
	},
	{
		ID:          "generic",
		Name:        "Generic OpenAI-compatible",
		Description: "Any CLI that accepts OPENAI_BASE_URL + OPENAI_API_KEY.",
		Icon:        "Terminal",
		ConfigKind:  "env",
		DocsURL:     "",
	},
}

func generateConfig(toolID string, sel CLIToolSelection, apiKey string) CLIToolConfig {
	base := sel.BaseURL
	if base == "" {
		base = "http://localhost:3777/v1"
	}

	switch toolID {
	case "claude":
		return claudeConfig(sel.Model, apiKey, base)
	case "codex":
		return codexConfig(sel.Model, apiKey, base)
	case "cline":
		return clineConfig(sel.Model, apiKey, base, "cline")
	case "kilo":
		return clineConfig(sel.Model, apiKey, base, "kilocode")
	case "openclaw":
		return openclawConfig(sel.Model, apiKey, base)
	case "generic":
		return genericConfig(sel.Model, apiKey, base)
	}
	return CLIToolConfig{}
}

func claudeConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export ANTHROPIC_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export ANTHROPIC_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export ANTHROPIC_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`{
  "env": {
    "ANTHROPIC_BASE_URL": %q,
    "ANTHROPIC_API_KEY": %q
  },
  "model": %q
}`, base, apiKey, model)
	return CLIToolConfig{
		EnvBlock:      env,
		ConfigPath:    "~/.claude/settings.json",
		ConfigContent: cfg,
		RunCommand:    fmt.Sprintf("claude --model %q", model),
	}
}

func codexConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`model = %q

[model.axonrouter]
base_url = %q
api_key = %q
`, model, base, apiKey)
	return CLIToolConfig{
		EnvBlock:      env,
		ConfigPath:    "~/.codex/config.toml",
		ConfigContent: cfg,
		RunCommand:    fmt.Sprintf("codex --model %q", model),
	}
}

func clineConfig(model, apiKey, base, keyPrefix string) CLIToolConfig {
	note := fmt.Sprintf("# %s reads these keys from VS Code settings.\n", keyPrefix)
	env := note + fmt.Sprintf("# Paste into .vscode/settings.json or your VS Code user settings.\n")
	cfg := fmt.Sprintf(`{
  "%s.apiProvider": "openai-compatible",
  "%s.openAiBaseUrl": %q,
  "%s.openAiApiKey": %q,
  "%s.openAiModelId": %q
}`, keyPrefix, keyPrefix, base, keyPrefix, apiKey, keyPrefix, model)
	return CLIToolConfig{
		EnvBlock:      env,
		ConfigPath:    "<VS Code user settings.json>",
		ConfigContent: cfg,
		RunCommand:    "Open Cline/Kilo Code in VS Code and select the OpenAI-compatible provider.",
	}
}

func openclawConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# OpenClaw reads ~/.openclaw/openclaw.json\n")
	cfg := fmt.Sprintf(`{
  "providers": [
    {
      "name": "axonrouter",
      "baseUrl": %q,
      "apiKey": %q,
      "models": [%q]
    }
  ],
  "defaultProvider": "axonrouter",
  "defaultModel": %q
}`, base, apiKey, model, model)
	return CLIToolConfig{
		EnvBlock:      env,
		ConfigPath:    "~/.openclaw/openclaw.json",
		ConfigContent: cfg,
		RunCommand:    fmt.Sprintf("openclaw --model %q", model),
	}
}

func genericConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{
		EnvBlock:      env,
		ConfigPath:    "",
		ConfigContent: "",
		RunCommand:    fmt.Sprintf("<your-cli> --base-url %q --api-key %q --model %q", base, apiKey, model),
	}
}
