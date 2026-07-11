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
	Image       string `json:"image"`
	Color       string `json:"color"`
	ConfigType  string `json:"configType"` // "env" | "custom" | "guide"
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

// CLIToolStatus reports whether a tool has been configured through the dashboard.
type CLIToolStatus struct {
	Configured bool `json:"configured"`
}

// CLIToolsHandler manages the dashboard CLI tools feature.
type CLIToolsHandler struct {
	db          *sql.DB
	modelLister func() []map[string]string
}

// NewCLIToolsHandler creates a CLI Tools handler.
func NewCLIToolsHandler(database *sql.DB, modelLister func() []map[string]string) *CLIToolsHandler {
	return &CLIToolsHandler{db: database, modelLister: modelLister}
}

// ListTools returns the supported CLI tool catalog. No secrets.
func (h *CLIToolsHandler) ListTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": cliToolCatalog})
}

// AllStatuses returns the configured/not-configured status for every tool in one round-trip.
func (h *CLIToolsHandler) AllStatuses(c *gin.Context) {
	statuses := make(map[string]CLIToolStatus, len(cliToolCatalog))
	for _, tool := range cliToolCatalog {
		raw := db.GetSetting(cliToolKeyPrefix+tool.ID, "")
		statuses[tool.ID] = CLIToolStatus{Configured: raw != ""}
	}
	c.JSON(http.StatusOK, statuses)
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
		"tool":            findTool(toolID),
		"selection":       sel,
		"defaultBaseUrl":  defaultBaseURL(c),
		"configured":      raw != "",
	})
}

// SaveConfig validates a selection, persists it, and emits the generated config.
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
	if req.APIKeyID != "" && !h.isAPIKeyActive(req.APIKeyID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selected api key is not active"})
		return
	}

	sel := CLIToolSelection{Model: req.Model, APIKeyID: req.APIKeyID, BaseURL: baseURL}
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
	c.JSON(http.StatusOK, gin.H{"selection": sel, "config": cfg})
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

// cliToolCatalog is the full CLI agent catalog, mirroring 9router's set.
var cliToolCatalog = []CLIToolStatic{
	{ID: "claude", Name: "Claude Code", Description: "Anthropic Claude Code CLI", Image: "/providers/claude.png", Color: "#D97757", ConfigType: "env", DocsURL: "https://docs.anthropic.com/en/docs/claude-code/overview"},
	{ID: "codex", Name: "OpenAI Codex CLI", Description: "OpenAI Codex CLI / App", Image: "/providers/codex.png", Color: "#10A37F", ConfigType: "env", DocsURL: "https://github.com/openai/codex"},
	{ID: "opencode", Name: "OpenCode", Description: "OpenCode AI Terminal Assistant", Image: "/providers/opencode.png", Color: "#E87040", ConfigType: "env", DocsURL: "https://github.com/sst/opencode"},
	{ID: "openclaw", Name: "Open Claw", Description: "Open Claw AI Assistant", Image: "/providers/openclaw.png", Color: "#FF6B35", ConfigType: "custom", DocsURL: "https://github.com/openclawhq/openclaw"},
	{ID: "cline", Name: "Cline", Description: "Cline AI Coding Assistant (VS Code)", Image: "/providers/cline.png", Color: "#00D1B2", ConfigType: "custom", DocsURL: "https://github.com/cline/cline"},
	{ID: "kilo", Name: "Kilo Code", Description: "Kilo Code AI Assistant (VS Code)", Image: "/providers/kilocode.png", Color: "#FF6B6B", ConfigType: "custom", DocsURL: "https://github.com/kilo-ai/kilo-code"},
	{ID: "roo", Name: "Roo", Description: "Roo AI Assistant (VS Code)", Image: "/providers/roo.png", Color: "#FF6B6B", ConfigType: "guide", DocsURL: "https://github.com/RooCodeInc/RooCode"},
	{ID: "continue", Name: "Continue", Description: "Continue AI Assistant (VS Code)", Image: "/providers/continue.png", Color: "#7C3AED", ConfigType: "guide", DocsURL: "https://www.continue.dev"},
	{ID: "cursor", Name: "Cursor", Description: "Cursor AI Code Editor", Image: "/providers/cursor.png", Color: "#000000", ConfigType: "guide", DocsURL: "https://cursor.com"},
	{ID: "amp", Name: "Amp CLI", Description: "Sourcegraph Amp coding assistant CLI", Image: "/providers/amp.png", Color: "#F97316", ConfigType: "guide", DocsURL: "https://github.com/sst/amp"},
	{ID: "qwen", Name: "Qwen Code", Description: "Alibaba Qwen Code CLI — OpenAI-compatible", Image: "/providers/qwen.png", Color: "#10B981", ConfigType: "guide", DocsURL: "https://qwenlm.github.io/qwen-code-docs"},
	{ID: "deepseek-tui", Name: "DeepSeek TUI", Description: "DeepSeek Terminal Coding Agent (Rust TUI)", Image: "/providers/deepseek-tui.png", Color: "#4D6BFE", ConfigType: "custom", DocsURL: "https://github.com/DeepSeek-TUI/DeepSeek-TUI"},
	{ID: "jcode", Name: "jcode", Description: "High-performance Rust-based coding agent harness", Image: "/providers/jcode.png", Color: "#FF6B35", ConfigType: "custom", DocsURL: "https://github.com/1jehuang/jcode"},
	{ID: "hermes", Name: "Hermes Agent", Description: "Nous Research self-improving AI agent", Image: "/providers/hermes.png", Color: "#8B5CF6", ConfigType: "custom", DocsURL: "https://github.com/NousResearch/hermes"},
	{ID: "droid", Name: "Factory Droid", Description: "Factory Droid AI Assistant", Image: "/providers/droid.png", Color: "#00D4FF", ConfigType: "custom", DocsURL: "https://factory.ai"},
	{ID: "copilot", Name: "GitHub Copilot", Description: "GitHub Copilot CLI coding agent", Image: "/providers/copilot.png", Color: "#24292E", ConfigType: "guide", DocsURL: "https://docs.github.com/copilot"},
	{ID: "generic", Name: "Generic OpenAI-compatible", Description: "Any CLI that accepts OPENAI_BASE_URL + OPENAI_API_KEY", Image: "/providers/openai.png", Color: "#10A37F", ConfigType: "env", DocsURL: ""},
}

// ensureBaseV1 appends /v1 if the base URL doesn't already end with it.
func ensureBaseV1(base string) string {
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func generateConfig(toolID string, sel CLIToolSelection, apiKey string) CLIToolConfig {
	base := ensureBaseV1(sel.BaseURL)
	if base == "" {
		base = "http://localhost:3777/v1"
	}

	switch toolID {
	case "claude":
		return claudeConfig(sel.Model, apiKey, base)
	case "codex":
		return codexConfig(sel.Model, apiKey, base)
	case "opencode":
		return opencodeConfig(sel.Model, apiKey, base)
	case "openclaw":
		return openclawConfig(sel.Model, apiKey, base)
	case "cline":
		return vscodeConfig(sel.Model, apiKey, base, "cline")
	case "kilo":
		return vscodeConfig(sel.Model, apiKey, base, "kilocode")
	case "roo":
		return vscodeConfig(sel.Model, apiKey, base, "roo")
	case "continue":
		return continueConfig(sel.Model, apiKey, base)
	case "cursor":
		return cursorConfig(sel.Model, apiKey, base)
	case "amp":
		return ampConfig(sel.Model, apiKey, base)
	case "qwen":
		return qwenConfig(sel.Model, apiKey, base)
	case "deepseek-tui":
		return deepseekTuiConfig(sel.Model, apiKey, base)
	case "jcode":
		return jcodeConfig(sel.Model, apiKey, base)
	case "hermes":
		return hermesConfig(sel.Model, apiKey, base)
	case "droid":
		return droidConfig(sel.Model, apiKey, base)
	case "copilot":
		return copilotConfig(sel.Model, apiKey, base)
	case "generic":
		return genericConfig(sel.Model, apiKey, base)
	}
	return CLIToolConfig{}
}

// --- Per-tool generators ---

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
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.claude/settings.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("claude --model %q", model)}
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
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.codex/config.toml", ConfigContent: cfg, RunCommand: fmt.Sprintf("codex --model %q", model)}
}

func opencodeConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`{
  "provider": "openai",
  "baseUrl": %q,
  "apiKey": %q,
  "model": %q
}`, base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.opencode/config.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("opencode --model %q", model)}
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
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.openclaw/openclaw.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("openclaw --model %q", model)}
}

// vscodeConfig generates config for Cline/Kilo/Roo — VS Code extensions with OpenAI-compatible settings.
func vscodeConfig(model, apiKey, base, keyPrefix string) CLIToolConfig {
	env := fmt.Sprintf("# %s reads these keys from VS Code settings.json\n", keyPrefix)
	cfg := fmt.Sprintf(`{
  "%s.apiProvider": "openai-compatible",
  "%s.openAiBaseUrl": %q,
  "%s.openAiApiKey": %q,
  "%s.openAiModelId": %q
}`, keyPrefix, keyPrefix, base, keyPrefix, apiKey, keyPrefix, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "<VS Code user settings.json>", ConfigContent: cfg, RunCommand: fmt.Sprintf("Select OpenAI-compatible provider in %s settings", keyPrefix)}
}

func continueConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Continue reads ~/.continue/config.json\n")
	cfg := fmt.Sprintf(`{
  "apiBase": %q,
  "title": %q,
  "model": %q,
  "provider": "openai",
  "apiKey": %q
}`, base, model, model, apiKey)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.continue/config.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("Add model in Continue config")}
}

func cursorConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Cursor: Settings → Models → Enable OpenAI API\n")
	env += fmt.Sprintf("# Base URL: %s\n", base)
	env += fmt.Sprintf("# API Key: %s\n", apiKey)
	cfg := fmt.Sprintf(`// In Cursor: Settings → Models → OpenAI API
// Base URL: %s
// API Key: %s
// Add custom model: %s`, base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "Cursor Settings → Models", ConfigContent: cfg, RunCommand: fmt.Sprintf("Cursor → Settings → Models → Add %q", model)}
}

func ampConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	cfg := fmt.Sprintf(`export OPENAI_API_KEY=%q
export OPENAI_BASE_URL=%q
amp --model %q
# Example shorthand aliases:
# g25p -> gemini/gemini-2.5-pro
# cs45 -> cc/claude-sonnet-4-5-20250929`, apiKey, base, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.amp/.env", ConfigContent: cfg, RunCommand: fmt.Sprintf("amp --model %q", model)}
}

func qwenConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Qwen Code reads ~/.qwen/settings.json\n")
	cfg := fmt.Sprintf(`{
  "security": {
    "auth": {
      "selectedType": "openai",
      "apiKey": %q,
      "baseUrl": %q
    }
  },
  "model": {
    "name": %q
  }
}`, apiKey, base, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.qwen/settings.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("qwen --model %q", model)}
}

func deepseekTuiConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`[provider]
type = "openai"
base_url = %q
api_key = %q
model = %q
`, base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.deepseek/config.toml", ConfigContent: cfg, RunCommand: fmt.Sprintf("deepseek --model %q", model)}
}

func jcodeConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`{
  "provider": "openai",
  "baseUrl": %q,
  "apiKey": %q,
  "model": %q
}`, base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.jcode/config.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("jcode --model %q", model)}
}

func hermesConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.hermes/.env", ConfigContent: "", RunCommand: fmt.Sprintf("hermes --model %q", model)}
}

func droidConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`{
  "provider": "openai",
  "baseUrl": %q,
  "apiKey": %q,
  "model": %q
}`, base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.droid/config.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("droid --model %q", model)}
}

func copilotConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# GitHub Copilot CLI\n")
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf(`# Set environment variables:
export OPENAI_API_KEY=%q
export OPENAI_BASE_URL=%q
export OPENAI_MODEL=%q
# Then run: gh copilot --model %q`, apiKey, base, model, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.copilot/config", ConfigContent: cfg, RunCommand: fmt.Sprintf("gh copilot --model %q", model)}
}

func genericConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "", ConfigContent: "", RunCommand: fmt.Sprintf("<your-cli> --base-url %q --api-key %q --model %q", base, apiKey, model)}
}
