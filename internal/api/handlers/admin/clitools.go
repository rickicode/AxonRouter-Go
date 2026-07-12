package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

const cliToolKeyPrefix = "clitools:"
const cliToolConfigKeyPrefix = "clitools:config:"

// DefaultModel describes one model-alias slot that the user can override (e.g. "sonnet" → "cc/claude-sonnet-5").
type DefaultModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Alias        string `json:"alias"`
	EnvKey       string `json:"envKey,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// GuideStep is one numbered step in a setup guide.
type GuideStep struct {
	Step     int    `json:"step"`
	Title    string `json:"title"`
	Desc     string `json:"desc,omitempty"`
	Value    string `json:"value,omitempty"`    // template string with {{baseUrl}}, {{apiKey}}, {{model}}
	Copyable bool   `json:"copyable,omitempty"` // true = show copy button for Value
	Type     string `json:"type,omitempty"`     // "apiKeySelector" | "modelSelector" | "text"
}

// CodeBlock is a copyable config snippet with template variables.
type CodeBlock struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// Note is an informational callout shown above the config form.
type Note struct {
	Type string `json:"type"` // "info" | "warning" | "error"
	Text string `json:"text"`
}

// CLIToolStatic holds the catalog metadata for one supported CLI agent.
type CLIToolStatic struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Image             string         `json:"image"`
	Color             string         `json:"color"`
	ConfigType        string         `json:"configType"`
	DocsURL           string         `json:"docsUrl"`
	DefaultModels     []DefaultModel `json:"defaultModels,omitempty"`
	GuideSteps        []GuideStep    `json:"guideSteps,omitempty"`
	CodeBlock         *CodeBlock     `json:"codeBlock,omitempty"`
	Notes             []Note         `json:"notes,omitempty"`
	SupportsDiscovery bool           `json:"supportsDiscovery,omitempty"`
	MultiModel        bool           `json:"multiModel,omitempty"`
}

// CLIToolSelection is what we persist and what the frontend submits.
type CLIToolSelection struct {
	Model        string            `json:"model"`
	APIKeyID     string            `json:"apiKeyId"`
	BaseURL      string            `json:"baseUrl"`
	ModelAliases map[string]string `json:"modelAliases,omitempty"` // alias → gateway model id
	Models       []string          `json:"models,omitempty"`
	UseDiscovery bool              `json:"useDiscovery,omitempty"`
}

// CLIToolConfig is the tool-specific output shown to the user.
type CLIToolConfig struct {
	EnvBlock      string `json:"envBlock"`
	ConfigPath    string `json:"configPath"`
	ConfigContent string `json:"configContent"`
	RunCommand    string `json:"runCommand"`
	BackupPath    string `json:"backupPath,omitempty"`
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

// ListTools returns the supported CLI tool catalog.
func (h *CLIToolsHandler) ListTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": cliToolCatalog})
}

// AllStatuses returns the configured/not-configured status for every tool.
func (h *CLIToolsHandler) AllStatuses(c *gin.Context) {
	statuses := make(map[string]CLIToolStatus, len(cliToolCatalog))
	for _, tool := range cliToolCatalog {
		raw := db.GetSetting(cliToolKeyPrefix+tool.ID, "")
		statuses[tool.ID] = CLIToolStatus{Configured: raw != ""}
	}
	c.JSON(http.StatusOK, statuses)
}

// GetConfig returns the persisted selection for a tool.
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
	var savedCfg CLIToolConfig
	if cfgRaw := db.GetSetting(cliToolConfigKeyPrefix+toolID, ""); cfgRaw != "" {
		_ = json.Unmarshal([]byte(cfgRaw), &savedCfg)
	}
	c.JSON(http.StatusOK, gin.H{
		"tool":           findTool(toolID),
		"selection":      sel,
		"defaultBaseUrl": defaultBaseURL(c),
		"configured":     raw != "",
		"config":         savedCfg,
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

	tool := findTool(toolID)

	// For tools with modelAliases, model can be empty (aliases drive the config).
	// For tools without aliases, model is required.
	if len(tool.DefaultModels) == 0 && len(tool.GuideSteps) == 0 {
		if req.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
			return
		}
		if !h.isModelAvailable(req.Model) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "selected model is not available on this gateway"})
			return
		}
	}

	// For tools that support discovery, require either discovery mode or a non-empty model list.
	if tool.SupportsDiscovery && !req.UseDiscovery {
		if len(req.Models) == 0 && req.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "select at least one model or enable auto-discovery"})
			return
		}
		for _, m := range req.Models {
			if !h.isModelAvailable(m) {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("model %q is not available", m)})
				return
			}
		}
	}

	// Validate alias models too
	for alias, modelID := range req.ModelAliases {
		if modelID != "" && !h.isModelAvailable(modelID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("model for alias %q is not available", alias)})
			return
		}
	}

	if req.APIKeyID != "" && !h.isAPIKeyActive(req.APIKeyID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selected api key is not active"})
		return
	}

	sel := CLIToolSelection{
		Model:        req.Model,
		APIKeyID:     req.APIKeyID,
		BaseURL:      baseURL,
		ModelAliases: req.ModelAliases,
		Models:       req.Models,
		UseDiscovery: req.UseDiscovery,
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
	if cfg.ConfigPath != "" && isWritableConfigPath(cfg.ConfigPath) {
		resolved, backup, werr := writeConfigFile(cfg.ConfigPath, cfg.ConfigContent)
		if werr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config file: " + werr.Error()})
			return
		}
		cfg.ConfigPath = resolved
		cfg.BackupPath = backup
	}
	if cfgJSON, jerr := json.Marshal(cfg); jerr == nil {
		_ = db.SetSetting(cliToolConfigKeyPrefix+toolID, string(cfgJSON))
	}
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
	return findTool(id) != nil
}

func findTool(id string) *CLIToolStatic {
	for i := range cliToolCatalog {
		if cliToolCatalog[i].ID == id {
			return &cliToolCatalog[i]
		}
	}
	return nil
}

// ensureBaseV1 appends /v1 if the base URL doesn't already end with it.
func ensureBaseV1(base string) string {
	if base == "" {
		return "http://localhost:3777/v1"
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

// --- Full CLI Tool Catalog ---

var cliToolCatalog = []CLIToolStatic{
	// ─── Claude Code ────────────────────────────────────────────────
	{
		ID: "claude", Name: "Claude Code", Description: "Anthropic's agentic coding assistant CLI.",
		Image: "/providers/claude.png", Color: "#D97757", ConfigType: "env",
		DocsURL: "https://docs.anthropic.com/en/docs/claude-code/overview",
		DefaultModels: []DefaultModel{
			{ID: "fable", Name: "Claude Fable", Alias: "fable", EnvKey: "ANTHROPIC_DEFAULT_FABLE_MODEL", DefaultValue: "cc/claude-fable-5"},
			{ID: "opus", Name: "Claude Opus", Alias: "opus", EnvKey: "ANTHROPIC_DEFAULT_OPUS_MODEL", DefaultValue: "cc/claude-opus-4-8"},
			{ID: "sonnet", Name: "Claude Sonnet", Alias: "sonnet", EnvKey: "ANTHROPIC_DEFAULT_SONNET_MODEL", DefaultValue: "cc/claude-sonnet-5"},
			{ID: "haiku", Name: "Claude Haiku", Alias: "haiku", EnvKey: "ANTHROPIC_DEFAULT_HAIKU_MODEL", DefaultValue: "cc/claude-haiku-4-5-20251001"},
		},
	},

	// ─── OpenAI Codex ───────────────────────────────────────────────
	{
		ID: "codex", Name: "OpenAI Codex CLI", Description: "OpenAI Codex CLI / App.",
		Image: "/providers/codex.png", Color: "#10A37F", ConfigType: "env",
		DocsURL: "https://github.com/openai/codex",
	},

	// ─── OpenCode ───────────────────────────────────────────────────
	{
		ID: "opencode", Name: "OpenCode", Description: "OpenCode AI Terminal Assistant.",
		Image: "/providers/opencode.png", Color: "#E87040", ConfigType: "env",
		DocsURL: "https://github.com/sst/opencode",
	},

	// ─── OpenClaw ───────────────────────────────────────────────────
	{
		ID: "openclaw", Name: "Open Claw", Description: "Open Claw AI Assistant.",
		Image: "/providers/openclaw.png", Color: "#FF6B35", ConfigType: "custom",
		DocsURL: "https://github.com/openclawhq/openclaw",
	},

	// ─── Cline ──────────────────────────────────────────────────────
	{
		ID: "cline", Name: "Cline", Description: "Cline AI Coding Assistant (VS Code).",
		Image: "/providers/cline.png", Color: "#00D1B2", ConfigType: "custom",
		DocsURL: "https://github.com/cline/cline",
	},

	// ─── Kilo Code ──────────────────────────────────────────────────
	{
		ID: "kilo", Name: "Kilo Code", Description: "Kilo Code AI Assistant (VS Code).",
		Image: "/providers/kilocode.png", Color: "#FF6B6B", ConfigType: "custom",
		DocsURL: "https://github.com/kilo-ai/kilo-code",
	},

	// ─── Roo ────────────────────────────────────────────────────────
	{
		ID: "roo", Name: "Roo", Description: "Roo AI Assistant (VS Code).",
		Image: "/providers/roo.png", Color: "#FF6B6B", ConfigType: "guide",
		DocsURL: "https://github.com/RooCodeInc/RooCode",
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Open Settings", Desc: "Go to Roo Settings panel"},
			{Step: 2, Title: "Select Provider", Desc: "Choose API Provider → Ollama"},
			{Step: 3, Title: "Base URL", Value: "{{baseUrl}}", Copyable: true},
			{Step: 4, Title: "API Key", Type: "apiKeySelector"},
			{Step: 5, Title: "Select Model", Type: "modelSelector"},
		},
	},

	// ─── Continue ───────────────────────────────────────────────────
	{
		ID: "continue", Name: "Continue", Description: "Continue AI Assistant (VS Code / JetBrains).",
		Image: "/providers/continue.png", Color: "#7C3AED", ConfigType: "guide",
		DocsURL: "https://www.continue.dev",
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Open Config", Desc: "Open Continue configuration file"},
			{Step: 2, Title: "API Key", Type: "apiKeySelector"},
			{Step: 3, Title: "Select Model", Type: "modelSelector"},
			{Step: 4, Title: "Add Model Config", Desc: "Add the following configuration to your models array:"},
		},
		CodeBlock: &CodeBlock{
			Language: "json",
			Code:     "{\n  \"apiBase\": \"{{baseUrl}}\",\n  \"title\": \"{{model}}\",\n  \"model\": \"{{model}}\",\n  \"provider\": \"openai\",\n  \"apiKey\": \"{{apiKey}}\"\n}",
		},
	},

	// ─── Cursor ─────────────────────────────────────────────────────
	{
		ID: "cursor", Name: "Cursor", Description: "Cursor AI Code Editor.",
		Image: "/providers/cursor.png", Color: "#000000", ConfigType: "guide",
		DocsURL: "https://cursor.com",
		Notes: []Note{
			{Type: "warning", Text: "Requires Cursor Pro account to use this feature."},
		},
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Open Settings", Desc: "Go to Settings → Models"},
			{Step: 2, Title: "Enable OpenAI API", Desc: "Enable \"OpenAI API key\" option"},
			{Step: 3, Title: "Base URL", Value: "{{baseUrl}}", Copyable: true},
			{Step: 4, Title: "API Key", Type: "apiKeySelector"},
			{Step: 5, Title: "Add Custom Model", Desc: "Click \"View All Model\" → \"Add Custom Model\""},
			{Step: 6, Title: "Select Model", Type: "modelSelector"},
		},
	},

	// ─── Amp CLI ────────────────────────────────────────────────────
	{
		ID: "amp", Name: "Amp CLI", Description: "Sourcegraph Amp coding assistant CLI.",
		Image: "/providers/amp.png", Color: "#F97316", ConfigType: "guide",
		DocsURL: "https://github.com/sst/amp",
		DefaultModels: []DefaultModel{
			{ID: "g25p", Name: "g25p (Gemini 2.5 Pro)", Alias: "g25p", EnvKey: "g25p", DefaultValue: "gemini/gemini-2.5-pro"},
			{ID: "g25f", Name: "g25f (Gemini 2.5 Flash)", Alias: "g25f", EnvKey: "g25f", DefaultValue: "gemini/gemini-2.5-flash"},
			{ID: "cs45", Name: "cs45 (Claude Sonnet 4.5)", Alias: "cs45", EnvKey: "cs45", DefaultValue: "cc/claude-sonnet-4-5-20250929"},
			{ID: "g54", Name: "g54 (Gemini 2.5 Pro)", Alias: "g54", EnvKey: "g54", DefaultValue: "gemini/gemini-2.5-pro"},
		},
		Notes: []Note{
			{Type: "info", Text: "Use AxonRouter model aliases to keep Amp shorthand mappings stable across provider updates."},
			{Type: "warning", Text: "Suggested shorthand examples: g25p → gemini/gemini-2.5-pro, g25f → gemini/gemini-2.5-flash, cs45 → cc/claude-sonnet-4-5-20250929."},
		},
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Install Amp", Desc: "Install the Amp CLI using the package manager supported by your environment."},
			{Step: 2, Title: "API Key", Type: "apiKeySelector"},
			{Step: 3, Title: "Base URL", Value: "{{baseUrl}}", Copyable: true},
			{Step: 4, Title: "Select Model", Type: "modelSelector"},
			{Step: 5, Title: "Add Shorthands", Desc: "Map Amp shorthand names such as g25p or cs45 to AxonRouter aliases in your local config."},
		},
		CodeBlock: &CodeBlock{
			Language: "bash",
			Code:     "export OPENAI_API_KEY=\"{{apiKey}}\"\nexport OPENAI_BASE_URL=\"{{baseUrl}}\"\namp --model \"{{model}}\"\n# Example shorthand aliases you can map locally:\n# g25p -> gemini/gemini-2.5-pro\n# cs45 -> cc/claude-sonnet-4-5-20250929",
		},
	},

	// ─── Qwen Code ──────────────────────────────────────────────────
	{
		ID: "qwen", Name: "Qwen Code", Description: "Alibaba Qwen Code CLI — supports OpenAI, Anthropic & Gemini providers.",
		Image: "/providers/qwen.png", Color: "#10B981", ConfigType: "guide",
		DocsURL: "https://qwenlm.github.io/qwen-code-docs",
		DefaultModels: []DefaultModel{
			{ID: "coder-model", Name: "coder-model", Alias: "coder-model", EnvKey: "coder-model", DefaultValue: "oc/mimo-v2.5-free"},
			{ID: "qwen3-coder-plus", Name: "qwen3-coder-plus", Alias: "qwen3-coder-plus", EnvKey: "qwen3-coder-plus", DefaultValue: "qwen/qwen3-coder-plus"},
			{ID: "qwen3-coder-flash", Name: "qwen3-coder-flash", Alias: "qwen3-coder-flash", EnvKey: "qwen3-coder-flash", DefaultValue: "qwen/qwen3-coder-flash"},
			{ID: "vision-model", Name: "vision-model", Alias: "vision-model", EnvKey: "vision-model", DefaultValue: "oc/hy3-free"},
			{ID: "claude-sonnet-4-6", Name: "claude-sonnet-4-6", Alias: "claude-sonnet-4-6", EnvKey: "claude-sonnet-4-6", DefaultValue: "cc/claude-sonnet-4-6"},
			{ID: "claude-opus-4-6-thinking", Name: "claude-opus-4-6-thinking", Alias: "claude-opus-4-6-thinking", EnvKey: "claude-opus-4-6-thinking", DefaultValue: "cc/claude-opus-4-6"},
			{ID: "gemini-3-flash", Name: "gemini-3-flash", Alias: "gemini-3-flash", EnvKey: "gemini-3-flash", DefaultValue: "gemini/gemini-3-flash"},
			{ID: "gemini-3.1-pro-high", Name: "gemini-3.1-pro-high", Alias: "gemini-3.1-pro-high", EnvKey: "gemini-3.1-pro-high", DefaultValue: "gemini/gemini-3.1-pro-high"},
		},
		Notes: []Note{
			{Type: "info", Text: "Qwen Code supports multiple provider types (openai, anthropic, gemini) via modelProviders in settings.json. AxonRouter works as an OpenAI-compatible endpoint."},
			{Type: "info", Text: "Any model available in AxonRouter can be used — not just Qwen models."},
			{Type: "warning", Text: "Config path: Linux/macOS ~/.qwen/settings.json • Windows %USERPROFILE%\\.qwen\\settings.json"},
		},
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Install Qwen Code", Desc: "npm install -g @qwen-code/qwen-code"},
			{Step: 2, Title: "API Key", Type: "apiKeySelector"},
			{Step: 3, Title: "Base URL", Value: "{{baseUrl}}", Copyable: true},
			{Step: 4, Title: "Select Model", Type: "modelSelector"},
			{Step: 5, Title: "Save Config", Desc: "Copy the JSON below to your ~/.qwen/settings.json file."},
		},
		CodeBlock: &CodeBlock{
			Language: "json",
			Code:     "{\n  \"security\": {\n    \"auth\": {\n      \"selectedType\": \"openai\",\n      \"apiKey\": \"{{apiKey}}\",\n      \"baseUrl\": \"{{baseUrl}}\"\n    }\n  },\n  \"model\": {\n    \"name\": \"{{model}}\"\n  }\n}",
		},
	},

	// ─── DeepSeek TUI ───────────────────────────────────────────────
	{
		ID: "deepseek-tui", Name: "DeepSeek TUI", Description: "DeepSeek Terminal Coding Agent (Rust TUI).",
		Image: "/providers/deepseek-tui.png", Color: "#4D6BFE", ConfigType: "custom",
		DocsURL: "https://github.com/DeepSeek-TUI/DeepSeek-TUI",
		DefaultModels: []DefaultModel{
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Alias: "deepseek-v4-pro"},
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Alias: "deepseek-v4-flash"},
			{ID: "deepseek-chat", Name: "DeepSeek V3 Chat", Alias: "deepseek-chat"},
		},
		Notes: []Note{
			{Type: "info", Text: "DeepSeek TUI uses ~/.deepseek/config.toml for configuration. AxonRouter will update the provider to 'openai' mode with your base_url, api_key, and model."},
			{Type: "warning", Text: "Config path: Linux/macOS ~/.deepseek/config.toml • Windows %USERPROFILE%\\.deepseek\\config.toml"},
		},
	},

	// ─── jcode ──────────────────────────────────────────────────────
	{
		ID: "jcode", Name: "jcode", Description: "High-performance Rust-based coding agent harness.",
		Image: "/providers/jcode.png", Color: "#FF6B35", ConfigType: "custom",
		DocsURL: "https://github.com/1jehuang/jcode",
		DefaultModels: []DefaultModel{
			{ID: "claude-opus-4-7", Name: "Claude Opus 4.7", Alias: "opus", DefaultValue: "cc/claude-opus-4-7"},
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Alias: "sonnet", DefaultValue: "cc/claude-sonnet-4-6"},
			{ID: "gpt-5.5", Name: "GPT 5.5", Alias: "gpt5", DefaultValue: "cx/gpt-5.5"},
			{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", Alias: "gemini", DefaultValue: "gemini/gemini-3.1-pro"},
		},
		Notes: []Note{
			{Type: "info", Text: "jcode is a Rust-based coding agent with semantic memory and extreme performance."},
			{Type: "info", Text: "Configure AxonRouter as an OpenAI-compatible provider to route all jcode requests."},
			{Type: "warning", Text: "Requires jcode installed. Install via: curl -fsSL https://raw.githubusercontent.com/1jehuang/jcode/master/scripts/install.sh | bash"},
		},
	},

	// ─── Hermes Agent ───────────────────────────────────────────────
	{
		ID: "hermes", Name: "Hermes Agent", Description: "Nous Research self-improving AI agent.",
		Image: "/providers/hermes.png", Color: "#8B5CF6", ConfigType: "custom",
		DocsURL: "https://github.com/NousResearch/hermes",
	},

	// ─── Factory Droid ──────────────────────────────────────────────
	{
		ID: "droid", Name: "Factory Droid", Description: "Factory Droid AI Assistant.",
		Image: "/providers/droid.png", Color: "#00D4FF", ConfigType: "custom",
		DocsURL: "https://factory.ai",
	},

	// ─── GitHub Copilot ─────────────────────────────────────────────
	{
		ID: "copilot", Name: "GitHub Copilot", Description: "GitHub Copilot CLI coding agent.",
		Image: "/providers/copilot.png", Color: "#24292E", ConfigType: "guide",
		DocsURL: "https://docs.github.com/copilot",
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Enable Copilot", Desc: "Ensure GitHub Copilot is enabled in your VS Code or IDE."},
			{Step: 2, Title: "API Key", Type: "apiKeySelector"},
			{Step: 3, Title: "Base URL", Value: "{{baseUrl}}", Copyable: true},
			{Step: 4, Title: "Select Model", Type: "modelSelector"},
			{Step: 5, Title: "Configure", Desc: "Set the OpenAI-compatible endpoint in Copilot settings."},
		},
		CodeBlock: &CodeBlock{
			Language: "json",
			Code:     "{\n  \"github.copilot.advanced\": {\n    \"debug.overrideModel\": \"{{model}}\",\n    \"debug.overrideProxyUrl\": \"{{baseUrl}}\"\n  }\n}",
		},
	},

	// ─── Generic OpenAI-compatible ──────────────────────────────────
	{
		ID: "generic", Name: "Generic OpenAI-compatible", Description: "Any CLI that accepts OPENAI_BASE_URL + OPENAI_API_KEY.",
		Image: "/providers/openai.png", Color: "#10A37F", ConfigType: "env",
	},
	// ─── PI Coding Agent ───────────────────────────────────────────
	{
		ID:                "pi",
		Name:              "PI Coding Agent",
		Description:       "Oh-My-Pi coding agent — register AxonRouter as an OpenAI-compatible provider in ~/.pi/agent/models.json.",
		Image:             "/providers/pi.png",
		Color:             "#8B5CF6",
		ConfigType:        "guide",
		DocsURL:           "https://github.com/oh-my-pi/pi-coding-agent",
		SupportsDiscovery: true,
		MultiModel:        true,
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Open pi models config", Desc: "Edit ~/.pi/agent/models.json and find the providers object."},
			{Step: 2, Title: "Select a model", Desc: "Pick which AxonRouter model to register (browse or type provider/model-id).", Type: "modelSelector"},
			{Step: 3, Title: "Merge provider block", Desc: "Paste the JSON entry below into the providers object."},
		},
		CodeBlock: &CodeBlock{
			Language: "json",
			Code: "  \"AxonRouter\": {\n" +
				"    \"baseUrl\": \"{{baseUrl}}\",\n" +
				"    \"api\": \"openai-completions\",\n" +
				"    \"apiKey\": \"{{apiKey}}\",\n" +
				"    \"authHeader\": true,\n" +
				"    \"compat\": { \"supportsDeveloperRole\": true, \"supportsReasoningEffort\": true },\n" +
				"    \"models\": [\n" +
				"      { \"id\": \"{{model}}\", \"name\": \"AxonRouter\", \"reasoning\": false, \"input\": [\"text\",\"image\"], \"contextWindow\": 200000, \"maxTokens\": 16384 }\n" +
				"    ]\n" +
				"  }",
		},
		Notes: []Note{
			{Type: "info", Text: "AxonRouter exposes /v1/models — you can add more model entries from there."},
			{Type: "info", Text: "If your pi build supports discovery, replace the models array with \"discovery\": { \"type\": \"openai-models-list\" }."},
		},
	},
	// ─── OMP (Oh My Pi) ─────────────────────────────────────────────
	{
		ID:                "omp",
		Name:              "OMP (Oh My Pi)",
		Description:       "Oh-My-Pi shell/agent — register AxonRouter as an OpenAI-compatible provider in ~/.omp/agent/models.yml.",
		Image:             "/providers/omp.png",
		Color:             "#EC4899",
		ConfigType:        "guide",
		DocsURL:           "https://github.com/oh-my-pi/omp",
		SupportsDiscovery: true,
		MultiModel:        true,
		GuideSteps: []GuideStep{
			{Step: 1, Title: "Open omp models config", Desc: "Edit ~/.omp/agent/models.yml and find the providers section."},
			{Step: 2, Title: "Merge provider block", Desc: "Paste the YAML entry below into the providers section. Models are auto-discovered."},
		},
		CodeBlock: &CodeBlock{
			Language: "yaml",
			Code: "axonrouter-go:\n" +
				" api: openai-completions\n" +
				" apiKey: \"{{apiKey}}\"\n" +
				" authHeader: true\n" +
				" baseUrl: \"{{baseUrl}}\"\n" +
				" discovery:\n" +
				"   type: openai-models-list",
		},
		Notes: []Note{
			{Type: "info", Text: "OMP auto-discovers models via /v1/models, so no manual model list is needed."},
		},
	},
}

// --- Config generators ---

// expandHome resolves a leading ~ to the current user's home directory.
func expandHome(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// isWritableConfigPath reports whether cfg.ConfigPath points at a single real
// file we may write, excluding placeholder or multi-file descriptions such as
// "<VS Code user settings.json>" or "~/.hermes/config.yaml + ~/.hermes/.env".
func isWritableConfigPath(p string) bool {
	if p == "" {
		return false
	}
	if strings.ContainsAny(p, " \t<>+") {
		return false
	}
	return strings.HasPrefix(p, "~") || filepath.IsAbs(p)
}

// writeConfigFile writes content to the resolved config path, backing up any
// existing file to a timestamped .bak first. It returns the resolved path and
// the backup path (empty when the destination did not previously exist).
func writeConfigFile(cfgPath, content string) (resolved string, backup string, err error) {
	resolved = expandHome(cfgPath)
	if _, statErr := os.Stat(resolved); statErr == nil {
		ts := time.Now().Format("20060102-150405")
		backup = resolved + ".bak-" + ts
		if data, rerr := os.ReadFile(resolved); rerr == nil {
			_ = os.WriteFile(backup, data, 0o644)
		}
	}
	if mkErr := os.MkdirAll(filepath.Dir(resolved), 0o755); mkErr != nil {
		return resolved, backup, mkErr
	}
	err = os.WriteFile(resolved, []byte(content), 0o644)
	return resolved, backup, err
}

func generateConfig(toolID string, sel CLIToolSelection, apiKey string) CLIToolConfig {
	base := ensureBaseV1(sel.BaseURL)

	switch toolID {
	case "claude":
		return claudeConfig(sel, apiKey, base)
	case "codex":
		return codexConfig(sel.Model, apiKey, base)
	case "opencode":
		return opencodeConfig(sel.Model, apiKey, base)
	case "openclaw":
		return openclawConfig(sel.Model, apiKey, base)
	case "cline":
		return clineConfig(sel.Model, apiKey, base)
	case "kilo":
		return kiloConfig(sel.Model, apiKey, base)
	case "roo":
		return guideConfig(sel.Model, apiKey, base)
	case "continue":
		return guideConfig(sel.Model, apiKey, base)
	case "cursor":
		return guideConfig(sel.Model, apiKey, base)
	case "amp":
		return ampConfig(sel, apiKey, base)
	case "qwen":
		return qwenConfig(sel, apiKey, base)
	case "deepseek-tui":
		return deepseekTuiConfig(sel, apiKey, base)
	case "jcode":
		return jcodeConfig(sel, apiKey, base)
	case "hermes":
		return hermesConfig(sel.Model, apiKey, base)
	case "droid":
		return droidConfig(sel.Model, apiKey, base)
	case "copilot":
		return guideConfig(sel.Model, apiKey, base)
	case "generic":
		return genericConfig(sel.Model, apiKey, base)
	case "pi":
		return piConfig(sel, apiKey, base)
	case "omp":
		return ompConfig(sel, apiKey, base)
	}
	return CLIToolConfig{}
}

// collectModels resolves the model list from sel.Models, falling back to sel.Model.
func collectModels(sel CLIToolSelection) []string {
	if len(sel.Models) > 0 {
		return sel.Models
	}
	if sel.Model != "" {
		return []string{sel.Model}
	}
	return []string{"provider/model-id"}
}

// piConfig builds the PI Coding Agent provider block. With UseDiscovery it emits a
// discovery mapping; otherwise it lists the selected models explicitly.
func piConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	provider := map[string]interface{}{
		"baseUrl":    base,
		"api":        "openai-completions",
		"apiKey":     apiKey,
		"authHeader": true,
	}
	if sel.UseDiscovery {
		provider["discovery"] = map[string]string{"type": "openai-models-list"}
	} else {
		models := collectModels(sel)
		entries := make([]map[string]interface{}, 0, len(models))
		for _, m := range models {
			entries = append(entries, map[string]interface{}{
				"id":            m,
				"name":          "AxonRouter",
				"reasoning":     false,
				"input":         []string{"text", "image"},
				"contextWindow": 200000,
				"maxTokens":     16384,
			})
		}
		provider["models"] = entries
	}
	out := map[string]interface{}{"AxonRouter": provider}
	b, _ := json.MarshalIndent(out, "", "  ")
	return CLIToolConfig{ConfigPath: "~/.pi/agent/models.json", ConfigContent: string(b)}
}

// ompConfig builds the OMP provider block (YAML). Discovery or explicit models.
func ompConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	var b strings.Builder
	b.WriteString("axonrouter-go:\n")
	b.WriteString("  api: openai-completions\n")
	b.WriteString(fmt.Sprintf("  apiKey: %q\n", apiKey))
	b.WriteString("  authHeader: true\n")
	b.WriteString(fmt.Sprintf("  baseUrl: %q\n", base))
	if sel.UseDiscovery {
		b.WriteString("  discovery:\n")
		b.WriteString("    type: openai-models-list\n")
	} else {
		models := collectModels(sel)
		b.WriteString("  models:\n")
		for _, m := range models {
			b.WriteString(fmt.Sprintf("    - id: %s\n", m))
			b.WriteString("      name: AxonRouter\n")
		}
	}
	return CLIToolConfig{ConfigPath: "~/.omp/agent/models.yml", ConfigContent: b.String()}
}

func claudeConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	// Build env block with alias mappings
	env := fmt.Sprintf("export ANTHROPIC_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export ANTHROPIC_API_KEY=%q\n", apiKey)

	// Map aliases to env keys
	tool := findTool("claude")
	if tool != nil && len(sel.ModelAliases) > 0 {
		for _, dm := range tool.DefaultModels {
			if mapped, ok := sel.ModelAliases[dm.Alias]; ok && mapped != "" {
				env += fmt.Sprintf("export %s=%q\n", dm.EnvKey, mapped)
			} else if dm.DefaultValue != "" {
				env += fmt.Sprintf("export %s=%q\n", dm.EnvKey, dm.DefaultValue)
			}
		}
	}

	cfg := fmt.Sprintf("{\n  \"env\": {\n    \"ANTHROPIC_BASE_URL\": %q,\n    \"ANTHROPIC_API_KEY\": %q", base, apiKey)
	if tool != nil && len(sel.ModelAliases) > 0 {
		for _, dm := range tool.DefaultModels {
			if mapped, ok := sel.ModelAliases[dm.Alias]; ok && mapped != "" {
				cfg += fmt.Sprintf(",\n    %q: %q", dm.EnvKey, mapped)
			} else if dm.DefaultValue != "" {
				cfg += fmt.Sprintf(",\n    %q: %q", dm.EnvKey, dm.DefaultValue)
			}
		}
	}
	cfg += "\n  }\n}"

	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.claude/settings.json", ConfigContent: cfg, RunCommand: "claude"}
}

func codexConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	cfg := fmt.Sprintf("model = %q\n\n[model.axonrouter]\nbase_url = %q\napi_key = %q\n", model, base, apiKey)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.codex/config.toml", ConfigContent: cfg, RunCommand: fmt.Sprintf("codex --model %q", model)}
}

func opencodeConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.opencode/config.json", RunCommand: fmt.Sprintf("opencode --model %q", model)}
}

func openclawConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# OpenClaw reads ~/.openclaw/openclaw.json\n")
	cfg := fmt.Sprintf("{\n  \"providers\": [{\n    \"name\": \"axonrouter\",\n    \"baseUrl\": %q,\n    \"apiKey\": %q,\n    \"models\": [%q]\n  }],\n  \"defaultProvider\": \"axonrouter\",\n  \"defaultModel\": %q\n}", base, apiKey, model, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.openclaw/openclaw.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("openclaw --model %q", model)}
}

func clineConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Cline reads VS Code settings.json\n")
	cfg := fmt.Sprintf("{\n  \"cline.openAiBaseUrl\": %q,\n  \"cline.openAiApiKey\": %q,\n  \"cline.openAiModelId\": %q\n}", base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "<VS Code user settings.json>", ConfigContent: cfg, RunCommand: "Open Cline in VS Code → Settings → OpenAI-compatible provider"}
}

func kiloConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Kilo Code reads VS Code settings.json\n")
	cfg := fmt.Sprintf("{\n  \"kilocode.openAiBaseUrl\": %q,\n  \"kilocode.openAiApiKey\": %q,\n  \"kilocode.openAiModelId\": %q\n}", base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.local/share/kilo/auth.json", ConfigContent: cfg, RunCommand: "Open Kilo Code in VS Code → Settings → OpenAI-compatible provider"}
}

func guideConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "", RunCommand: ""}
}

func ampConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	aliasLines := ""
	if len(sel.ModelAliases) > 0 {
		for alias, modelID := range sel.ModelAliases {
			if modelID != "" {
				aliasLines += fmt.Sprintf("# %s -> %s\n", alias, modelID)
			}
		}
	}
	model := sel.Model
	if model == "" {
		model = "g25p"
	}
	cfg := fmt.Sprintf("export OPENAI_API_KEY=%q\nexport OPENAI_BASE_URL=%q\namp --model %q\n%s", apiKey, base, model, aliasLines)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "", ConfigContent: cfg, RunCommand: fmt.Sprintf("amp --model %q", model)}
}

func qwenConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("# Qwen Code reads ~/.qwen/settings.json\n")
	model := sel.Model
	if sel.ModelAliases != nil {
		if m, ok := sel.ModelAliases["coder-model"]; ok && m != "" {
			model = m
		}
	}
	if model == "" {
		model = "coder-model"
	}
	aliasBlock := ""
	if len(sel.ModelAliases) > 0 {
		aliasBlock = ",\n \"modelAliases\": {\n"
		first := true
		for alias, modelID := range sel.ModelAliases {
			if modelID == "" {
				continue
			}
			if !first {
				aliasBlock += ",\n"
			}
			aliasBlock += fmt.Sprintf("  %q: %q", alias, modelID)
			first = false
		}
		aliasBlock += "\n }"
	}
	cfg := fmt.Sprintf("{\n \"security\": {\n \"auth\": {\n \"selectedType\": \"openai\",\n \"apiKey\": %q,\n \"baseUrl\": %q\n }\n },\n \"model\": {\n \"name\": %q\n }%s\n}", apiKey, base, model, aliasBlock)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.qwen/settings.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("qwen --model %q", model)}
}

func deepseekTuiConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	model := sel.Model
	if model == "" {
		model = "deepseek-v4-pro"
	}
	cfg := fmt.Sprintf("[providers.openai]\nbase_url = %q\napi_key = %q\nmodel = %q\n", base, apiKey, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.deepseek/config.toml", ConfigContent: cfg, RunCommand: fmt.Sprintf("deepseek --model %q", model)}
}

func jcodeConfig(sel CLIToolSelection, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	model := sel.Model
	if model == "" {
		model = "cc/claude-sonnet-4-6"
	}
	cfg := fmt.Sprintf("[providers.axonrouter]\ntype = \"openai\"\nbase_url = %q\napi_key = %q\ndefault_model = %q\n\n[[providers.axonrouter.models]]\nid = %q\n", base, apiKey, model, model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.jcode/config.toml", ConfigContent: cfg, RunCommand: fmt.Sprintf("jcode --model %q", model)}
}

func hermesConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	cfg := fmt.Sprintf("model:\n  default: %q\nprovider:\n  name: custom\n  base_url: %q\n", model, base)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.hermes/config.yaml + ~/.hermes/.env", ConfigContent: cfg, RunCommand: fmt.Sprintf("hermes --model %q", model)}
}

func droidConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	cfg := fmt.Sprintf("{\n  \"customModels\": [{\n    \"id\": \"custom:axr-0\",\n    \"model\": %q,\n    \"baseUrl\": %q,\n    \"apiKey\": %q,\n    \"provider\": \"openai\",\n    \"maxOutputTokens\": 131072\n  }]\n}", model, base, apiKey)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "~/.factory/settings.json", ConfigContent: cfg, RunCommand: fmt.Sprintf("droid --model %q", model)}
}

func genericConfig(model, apiKey, base string) CLIToolConfig {
	env := fmt.Sprintf("export OPENAI_BASE_URL=%q\n", base)
	env += fmt.Sprintf("export OPENAI_API_KEY=%q\n", apiKey)
	env += fmt.Sprintf("export OPENAI_MODEL=%q\n", model)
	return CLIToolConfig{EnvBlock: env, ConfigPath: "", RunCommand: fmt.Sprintf("<your-cli> --model %q", model)}
}
