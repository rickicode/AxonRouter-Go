package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// cliToolDriver mirrors 9router's per-tool settings route (detect/apply/reset).
type cliToolDriver interface {
	// detect checks whether the CLI tool is installed and whether it already
	// points at AxonRouter. It returns driver-specific state.
	detect(ctx context.Context) (installed bool, hasUs bool, state map[string]any, err error)

	// apply writes the CLI tool config so that it uses AxonRouter.
	apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error)

	// reset removes AxonRouter-specific settings from the CLI tool config.
	reset(ctx context.Context) error
}

var toolDrivers = map[string]cliToolDriver{
	"claude":         &claudeDriver{},
	"codex":          &codexDriver{},
	"opencode":       &opencodeDriver{},
	"openclaw":       &openclawDriver{},
	"cline":          &clineDriver{},
	"kilo":           &kiloDriver{},
	"droid":          &droidDriver{},
	"hermes":         &hermesDriver{},
	"deepseek-tui":   &deepseekTuiDriver{},
	"jcode":          &jcodeDriver{},
	"copilot":        &copilotDriver{},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func userHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}


func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lookPath checks whether a binary exists in PATH (with Windows npm global fallback).
func lookPath(name string) bool {
	pathEnv := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			pathEnv = appData + "\\npm;" + pathEnv
		}
	}
	_, err := exec.LookPath(name)
	return err == nil
}

var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

func stripJSONC(b []byte) []byte {
	return trailingCommaRe.ReplaceAll(b, []byte("$1"))
}

func readJSONC(path string, out any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(stripJSONC(b), out); err != nil {
		return false
	}
	return true
}

func writeJSONPretty(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func normalizeBaseV1(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "http://localhost:3777/v1"
	}
	base = strings.TrimSuffix(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func normalizeBaseNoV1(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "http://localhost:3777"
	}
	base = strings.TrimSuffix(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return strings.TrimSuffix(base, "/v1")
	}
	return base
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func ensureMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		v = make(map[string]any)
		m[key] = v
	}
	return v
}

func modelOrDefault(sel CLIToolSelection) string {
	if sel.Model != "" {
		return sel.Model
	}
	for _, m := range sel.Models {
		if m != "" {
			return m
		}
	}
	for _, v := range sel.ModelAliases {
		if v != "" {
			return v
		}
	}
	return ""
}

func defaultEnvBlock(keys ...string) string {
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
	}
	return sb.String()
}

// setEnvModelAliases fills env with ANTHROPIC-style default model env keys,
// matching 9router's claude/openclaw behavior.
func setEnvModelAliases(env map[string]string, sel CLIToolSelection, tool *CLIToolStatic) {
	if tool == nil {
		return
	}
	for _, dm := range tool.DefaultModels {
		if envKey := dm.EnvKey; envKey != "" {
			if mapped := sel.ModelAliases[dm.Alias]; mapped != "" {
				env[envKey] = mapped
			} else if dm.DefaultValue != "" {
				env[envKey] = dm.DefaultValue
			}
		}
	}
}

// modelAliasesFromSelection returns the effective alias map from a selection.
func modelAliasesFromSelection(sel CLIToolSelection, tool *CLIToolStatic) map[string]string {
	out := make(map[string]string)
	for k, v := range sel.ModelAliases {
		if v != "" {
			out[k] = v
		}
	}
	if tool != nil {
		for _, dm := range tool.DefaultModels {
			if _, ok := out[dm.Alias]; !ok && dm.DefaultValue != "" {
				out[dm.Alias] = dm.DefaultValue
			}
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Claude Code
// 9router: src/app/api/cli-tools/claude-settings/route.js
// ---------------------------------------------------------------------------

type claudeDriver struct{}

func (claudeDriver) settingsPath() string {
	return filepath.Join(userHomeDir(), ".claude", "settings.json")
}

func (d claudeDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("claude") && !fileExists(d.settingsPath()) {
		return false, false, nil, nil
	}
	var settings map[string]any
	if !readJSONC(d.settingsPath(), &settings) {
		settings = map[string]any{}
	}
	env := getMap(settings, "env")
	hasUs := false
	if env != nil {
		if v, ok := env["ANTHROPIC_BASE_URL"].(string); ok && v != "" {
			hasUs = true
		}
	}
	state := map[string]any{
		"settings":     settings,
		"settingsPath": d.settingsPath(),
	}
	return true, hasUs, state, nil
}

func (d claudeDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}

	settings := map[string]any{}
	_ = readJSONC(path, &settings)

	env := ensureMap(settings, "env")
	env["ANTHROPIC_BASE_URL"] = normalizeBaseV1(sel.BaseURL)
	env["ANTHROPIC_AUTH_TOKEN"] = firstNonEmpty(apiKey, "sk_9router")
	env["API_TIMEOUT_MS"] = "600000"

	tool := findTool("claude")
	for _, dm := range tool.DefaultModels {
		envKey := dm.EnvKey
		if envKey == "" {
			continue
		}
		if mapped := sel.ModelAliases[dm.Alias]; mapped != "" {
			env[envKey] = mapped
		} else if dm.DefaultValue != "" {
			env[envKey] = dm.DefaultValue
		}
	}

	if _, ok := settings["hasCompletedOnboarding"]; !ok {
		settings["hasCompletedOnboarding"] = true
	}

	contentBs, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return CLIToolConfig{}, err
	}

	backup, err := backupExistingFile(path)
	if err != nil && !os.IsNotExist(err) {
		return CLIToolConfig{}, err
	}
	if err := os.WriteFile(path, contentBs, 0o644); err != nil {
		restoreBackup(path, backup)
		return CLIToolConfig{}, err
	}

	cfg := CLIToolConfig{
		ConfigPath:    path,
		BackupPath:    backup,
		ConfigContent: string(contentBs),
		RunCommand:    "claude",
	}

	var envBlock strings.Builder
	envBlock.WriteString(fmt.Sprintf("export ANTHROPIC_BASE_URL=%q\n", env["ANTHROPIC_BASE_URL"]))
	envBlock.WriteString(fmt.Sprintf("export ANTHROPIC_AUTH_TOKEN=%q\n", env["ANTHROPIC_AUTH_TOKEN"]))
	envBlock.WriteString(fmt.Sprintf("export API_TIMEOUT_MS=%q\n", env["API_TIMEOUT_MS"]))
	for _, k := range []string{"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL"} {
		if v, ok := env[k].(string); ok && v != "" {
			envBlock.WriteString(fmt.Sprintf("export %s=%q\n", k, v))
		}
	}
	cfg.EnvBlock = envBlock.String()
	return cfg, nil
}

func (d claudeDriver) reset(ctx context.Context) error {
	path := d.settingsPath()
	settings := map[string]any{}
	if !readJSONC(path, &settings) {
		return nil
	}
	env := getMap(settings, "env")
	if env == nil {
		return nil
	}
	for _, k := range []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"API_TIMEOUT_MS",
	} {
		delete(env, k)
	}
	if len(env) == 0 {
		delete(settings, "env")
	}
	return writeJSONPretty(path, settings)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func backupExistingFile(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	backup := path + ".bak-" + time.Now().Format("20060102-150405")
	data, err := os.ReadFile(path)
	if err != nil {
		return backup, err
	}
	return backup, os.WriteFile(backup, data, 0o644)
}

func restoreBackup(path, backup string) {
	if backup == "" {
		return
	}
	if data, err := os.ReadFile(backup); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// ---------------------------------------------------------------------------
// Codex CLI
// 9router: src/app/api/cli-tools/codex-settings/route.js
// ---------------------------------------------------------------------------

type codexDriver struct{}

func (codexDriver) configPath() string  { return filepath.Join(userHomeDir(), ".codex", "config.toml") }
func (codexDriver) authPath() string    { return filepath.Join(userHomeDir(), ".codex", "auth.json") }

func (d codexDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("codex") && !fileExists(d.configPath()) {
		return false, false, nil, nil
	}
	content := ""
	if b, err := os.ReadFile(d.configPath()); err == nil {
		content = string(b)
	}
	hasUs := strings.Contains(content, `model_provider = "9router"`) ||
		strings.Contains(content, `[model_providers.9router]`)
	state := map[string]any{
		"config":     content,
		"configPath": d.configPath(),
	}
	return true, hasUs, state, nil
}

func (d codexDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	if err := os.MkdirAll(filepath.Dir(d.configPath()), 0o755); err != nil {
		return CLIToolConfig{}, err
	}

	base := normalizeBaseV1(sel.BaseURL)
	model := firstNonEmpty(sel.Model, sel.SubagentModel)
	if model == "" && len(sel.Models) > 0 {
		model = sel.Models[0]
	}
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	subagent := firstNonEmpty(sel.SubagentModel, model)

	parsed := map[string]any{}
	if b, err := os.ReadFile(d.configPath()); err == nil {
		_ = toml.Unmarshal(b, &parsed)
	}

	parsed["model"] = model
	parsed["model_provider"] = "9router"
	mp := ensureMap(parsed, "model_providers")
	mp["9router"] = map[string]any{
		"name":     "9Router",
		"base_url": base,
		"wire_api": "responses",
	}
	agents := ensureMap(parsed, "agents")
	agents["subagent"] = map[string]any{"model": subagent}

	cfgContent, err := toml.Marshal(parsed)
	if err != nil {
		return CLIToolConfig{}, err
	}

	backup, _ := backupExistingFile(d.configPath())
	if err := os.WriteFile(d.configPath(), cfgContent, 0o644); err != nil {
		restoreBackup(d.configPath(), backup)
		return CLIToolConfig{}, err
	}

	auth := map[string]any{}
	if b, err := os.ReadFile(d.authPath()); err == nil {
		_ = json.Unmarshal(stripJSONC(b), &auth)
	}
	auth["OPENAI_API_KEY"] = firstNonEmpty(apiKey, "sk_9router")
	auth["auth_mode"] = "apikey"
	_ = writeJSONPretty(d.authPath(), auth)

	return CLIToolConfig{
		ConfigPath:    d.configPath(),
		BackupPath:    backup,
		ConfigContent: string(cfgContent),
		RunCommand:    fmt.Sprintf("codex --model %q", model),
	}, nil
}

// ---------------------------------------------------------------------------
// OpenCode
// 9router: src/app/api/cli-tools/opencode-settings/route.js
// ---------------------------------------------------------------------------

type opencodeDriver struct{}

func (opencodeDriver) configPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode", "opencode.json")
	}
	return filepath.Join(userHomeDir(), ".config", "opencode", "opencode.json")
}

func (d opencodeDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("opencode") && !fileExists(d.configPath()) {
		return false, false, nil, nil
	}
	config := map[string]any{}
	if !readJSONC(d.configPath(), &config) {
		config = map[string]any{}
	}
	provider := getMap(getMap(config, "provider"), "9router")
	hasUs := provider != nil
	state := map[string]any{
		"config":     config,
		"configPath": d.configPath(),
	}
	if provider != nil {
		active := ""
		if model, ok := config["model"].(string); ok && strings.HasPrefix(model, "9router/") {
			active = strings.TrimPrefix(model, "9router/")
		}
		baseURL := ""
		if opts := getMap(provider, "options"); opts != nil {
			if v, ok := opts["baseURL"].(string); ok {
				baseURL = v
			}
		}
		state["opencode"] = map[string]any{
			"baseURL":     baseURL,
			"activeModel": active,
		}
	}
	return true, hasUs, state, nil
}

func (d opencodeDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}

	config := map[string]any{}
	_ = readJSONC(path, &config)
	if config == nil {
		config = map[string]any{}
	}

	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "sk_9router")

	models := []string{}
	if len(sel.Models) > 0 {
		models = append(models, sel.Models...)
	} else if sel.Model != "" {
		models = append(models, sel.Model)
	}
	for _, v := range sel.ModelAliases {
		if v != "" {
			models = append(models, v)
		}
	}
	if len(models) == 0 {
		return CLIToolConfig{}, fmt.Errorf("at least one model is required")
	}

	provider := ensureMap(ensureMap(config, "provider"), "9router")
	if provider["options"] == nil {
		provider["options"] = map[string]any{}
	}
	provider["options"].(map[string]any)["baseURL"] = base
	provider["options"].(map[string]any)["apiKey"] = key
	provider["npm"] = "@ai-sdk/openai-compatible"
	if provider["models"] == nil {
		provider["models"] = map[string]any{}
	}
	modelMap := provider["models"].(map[string]any)
	for _, m := range models {
		modelMap[m] = map[string]any{
			"name":       m,
			"modalities": map[string]any{"input": []string{"text", "image"}, "output": []string{"text"}},
		}
	}

	active := firstNonEmpty(sel.ActiveModel, models[0])
	if active != "" {
		config["model"] = "9router/" + active
	}

	agents := ensureMap(config, "agent")
	agents["explorer"] = map[string]any{
		"description": "Fast explorer subagent for codebase exploration",
		"mode":        "subagent",
		"model":       "9router/" + firstNonEmpty(sel.SubagentModel, models[0]),
	}

	if err := writeJSONPretty(path, config); err != nil {
		return CLIToolConfig{}, err
	}

	return CLIToolConfig{
		ConfigPath: path,
		RunCommand: fmt.Sprintf("opencode --model %q", active),
	}, nil
}

func (d opencodeDriver) reset(ctx context.Context) error {
	path := d.configPath()
	config := map[string]any{}
	if !readJSONC(path, &config) {
		return nil
	}
	if provider := getMap(config, "provider"); provider != nil {
		delete(provider, "9router")
		if len(provider) == 0 {
			delete(config, "provider")
		}
	}
	if model, ok := config["model"].(string); ok && strings.HasPrefix(model, "9router/") {
		delete(config, "model")
	}
	if agent := getMap(config, "agent"); agent != nil {
		if explorer := getMap(agent, "explorer"); explorer != nil {
			if model, ok := explorer["model"].(string); ok && strings.HasPrefix(model, "9router/") {
				delete(agent, "explorer")
			}
		}
		if len(agent) == 0 {
			delete(config, "agent")
		}
	}
	return writeJSONPretty(path, config)
}

func (d codexDriver) reset(ctx context.Context) error {
	parsed := map[string]any{}
	if b, err := os.ReadFile(d.configPath()); err == nil {
		_ = toml.Unmarshal(b, &parsed)
	} else if os.IsNotExist(err) {
		return nil
	} else {
		return err
	}

	if mp, ok := parsed["model_provider"].(string); ok && mp == "9router" {
		delete(parsed, "model")
		delete(parsed, "model_provider")
	}
	if mps := getMap(parsed, "model_providers"); mps != nil {
		delete(mps, "9router")
		if len(mps) == 0 {
			delete(parsed, "model_providers")
		}
	}
	if agents := getMap(parsed, "agents"); agents != nil {
		delete(agents, "subagent")
		if len(agents) == 0 {
			delete(parsed, "agents")
		}
	}
	if len(parsed) == 0 {
		// Avoid writing empty TOML; keep a sensible default.
		_ = os.WriteFile(d.configPath(), []byte("model = \"gpt-4.1\"\n"), 0o644)
	} else {
		if out, err := toml.Marshal(parsed); err == nil {
			_ = os.WriteFile(d.configPath(), out, 0o644)
		}
	}

	auth := map[string]any{}
	if b, err := os.ReadFile(d.authPath()); err == nil {
		_ = json.Unmarshal(stripJSONC(b), &auth)
	} else {
		return nil
	}
	delete(auth, "OPENAI_API_KEY")
	delete(auth, "auth_mode")
	if len(auth) == 0 {
		_ = os.Remove(d.authPath())
	} else {
		_ = writeJSONPretty(d.authPath(), auth)
	}
	return nil
}
// resolveAgentModel normalizes an OpenClaw agent.model value (plain string or
// {primary, fallbacks} object) to its string id, matching 9router's behavior.
func resolveAgentModel(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		if p, ok := m["primary"].(string); ok {
			return p
		}
	}
	return ""
}

// isLocalOr9Router reports whether base points at localhost/127.0.0.1/9router.
func isLocalOr9Router(base string) bool {
	return strings.Contains(base, "localhost") ||
		strings.Contains(base, "127.0.0.1") ||
		strings.Contains(base, "9router")
}

// isLocalOr9RouterWide is like isLocalOr9Router but also matches 0.0.0.0
// (used by hermes, which explicitly allows 0.0.0.0 bindings).
func isLocalOr9RouterWide(base string) bool {
	return isLocalOr9Router(base) || strings.Contains(base, "0.0.0.0")
}

func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ---------------------------------------------------------------------------
// OpenClaw
// 9router: src/app/api/cli-tools/openclaw-settings/route.js
// ---------------------------------------------------------------------------
type openclawDriver struct{}

func (d openclawDriver) settingsPath() string {
	return filepath.Join(userHomeDir(), ".openclaw", "openclaw.json")
}

func (d openclawDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("openclaw") && !fileExists(d.settingsPath()) {
		return false, false, nil, nil
	}
	settings := map[string]any{}
	if !readJSONC(d.settingsPath(), &settings) {
		settings = map[string]any{}
	}
	agents := map[string]any{}
	if a, ok := settings["agents"].(map[string]any); ok {
		agents = a
	}
	listRaw, _ := agents["list"].([]any)
	enriched := make([]map[string]any, 0, len(listRaw))
	for _, a := range listRaw {
		agent, ok := a.(map[string]any)
		if !ok {
			continue
		}
		agent = cloneMap(agent)
		agent["model"] = resolveAgentModel(agent["model"])
		if agentDir, _ := agent["agentDir"].(string); agentDir != "" {
			if m, _ := d.readAgentModel(agentDir); m != "" {
				agent["currentModel"] = m
			}
		}
		enriched = append(enriched, agent)
	}
	hasUs := false
	if models := getMap(settings, "models"); models != nil {
		if providers := getMap(models, "providers"); providers != nil {
			if _, ok := providers["9router"]; ok {
				hasUs = true
			}
		}
	}
	state := map[string]any{
		"settings":    settings,
		"agents":      enriched,
		"settingsPath": d.settingsPath(),
	}
	return true, hasUs, state, nil
}

func (openclawDriver) readAgentModel(agentDir string) (string, error) {
	if agentDir == "" {
		return "", nil
	}
	modelsPath := filepath.Join(agentDir, "models.json")
	data := map[string]any{}
	if !readJSONC(modelsPath, &data) {
		return "", nil
	}
	providers := getMap(data, "providers")
	if providers == nil {
		return "", nil
	}
	router := getMap(providers, "9router")
	if router == nil {
		return "", nil
	}
	models, _ := router["models"].([]any)
	if len(models) == 0 {
		return "", nil
	}
	if first, ok := models[0].(map[string]any); ok {
		if id, _ := first["id"].(string); id != "" {
			return id, nil
		}
	}
	return "", nil
}
func (d openclawDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	settings := map[string]any{}
	_ = readJSONC(path, &settings)
	model := firstNonEmpty(sel.Model, modelOrDefault(sel))
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "your_api_key")
	fullModelId := "9router/" + model

	agents := ensureMap(settings, "agents")
	defaults := ensureMap(agents, "defaults")
	modelDef := ensureMap(defaults, "model")
	allowlist := ensureMap(defaults, "models")
	modelsRoot := ensureMap(ensureMap(settings, "models"), "providers")

	// Remove stale 9router/* allowlist entries.
	for k := range allowlist {
		if strings.HasPrefix(k, "9router/") {
			delete(allowlist, k)
		}
	}

	// Collect all unique model ids (default + per-agent overrides).
	allIDs := map[string]struct{}{model: {}}
	for _, m := range sel.AgentModels {
		if m != "" {
			allIDs[m] = struct{}{}
		}
	}
	for m := range allIDs {
		allowlist["9router/"+m] = map[string]any{}
	}

	modelDef["primary"] = fullModelId

	// Strip old 9router models from the agents.list entries.
	listRaw, _ := agents["list"].([]any)
	for _, a := range listRaw {
		agent, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if rm := resolveAgentModel(agent["model"]); rm != "" &&
			strings.HasPrefix(rm, "9router/") {
			delete(agent, "model")
		}
	}

	// Build the provider models list (raw ids, no 9router/ prefix).
	providerModels := make([]map[string]any, 0, len(allIDs))
	for m := range allIDs {
		providerModels = append(providerModels, map[string]any{
			"id":   m,
			"name": lastPathSegment(m),
		})
	}
	modelsRoot["9router"] = map[string]any{
		"baseUrl": base,
		"apiKey":  key,
		"api":     "openai-completions",
		"models":  providerModels,
	}

	// Apply per-agent overrides and write each agent's models.json.
	if len(listRaw) > 0 {
		for _, a := range listRaw {
			agent, ok := a.(map[string]any)
			if !ok {
				continue
			}
			id, _ := agent["id"].(string)
			if override, ok := sel.AgentModels[id]; ok && override != "" {
				agent["model"] = "9router/" + override
			}
			if agentDir, _ := agent["agentDir"].(string); agentDir != "" {
				modelToWrite := firstNonEmpty(sel.AgentModels[id], model)
				if err := d.writeAgentModels(agentDir, modelToWrite, base, key); err != nil {
					return CLIToolConfig{}, err
				}
			}
		}
		agents["list"] = listRaw
	}

	bs, err := json.MarshalIndent(settings, "", " ")
	if err != nil {
		return CLIToolConfig{}, err
	}
	backup, _ := backupExistingFile(path)
	if err := os.WriteFile(path, bs, 0o644); err != nil {
		restoreBackup(path, backup)
		return CLIToolConfig{}, err
	}
	return CLIToolConfig{
		ConfigPath:    path,
		BackupPath:    backup,
		ConfigContent: string(bs),
		RunCommand:    "openclaw",
	}, nil
}

func (d openclawDriver) writeAgentModels(agentDir, model, base, apiKey string) error {
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	modelsPath := filepath.Join(agentDir, "models.json")
	existing := map[string]any{}
	_ = readJSONC(modelsPath, &existing)
	providers := ensureMap(existing, "providers")
	providers["9router"] = map[string]any{
		"baseUrl": base,
		"apiKey":  firstNonEmpty(apiKey, "your_api_key"),
		"api":     "openai-completions",
		"models": []map[string]any{
			{"id": model, "name": lastPathSegment(model)},
		},
	}
	return writeJSONPretty(modelsPath, existing)
}

func (d openclawDriver) reset(ctx context.Context) error {
	path := d.settingsPath()
	settings := map[string]any{}
	if !readJSONC(path, &settings) {
		return nil
	}
	if models := getMap(settings, "models"); models != nil {
		if providers := getMap(models, "providers"); providers != nil {
			delete(providers, "9router")
			if len(providers) == 0 {
				delete(models, "providers")
			}
		}
	}
	if agents := getMap(settings, "agents"); agents != nil {
		if defaults := getMap(agents, "defaults"); defaults != nil {
			if allowlist := getMap(defaults, "models"); allowlist != nil {
				for k := range allowlist {
					if strings.HasPrefix(k, "9router/") {
						delete(allowlist, k)
					}
				}
				if len(allowlist) == 0 {
					delete(defaults, "models")
				}
			}
			if modelDef := getMap(defaults, "model"); modelDef != nil {
				if primary, _ := modelDef["primary"].(string); strings.HasPrefix(primary, "9router/") {
					delete(modelDef, "primary")
				}
				if len(modelDef) == 0 {
					delete(defaults, "model")
				}
			}
		}
		// Strip per-agent 9router model overrides.
		if list, ok := agents["list"].([]any); ok {
			for _, a := range list {
				agent, ok := a.(map[string]any)
				if !ok {
					continue
				}
				if rm := resolveAgentModel(agent["model"]); strings.HasPrefix(rm, "9router/") {
					delete(agent, "model")
				}
			}
		}
	}
	return writeJSONPretty(path, settings)
}

// ---------------------------------------------------------------------------
// Cline
// 9router: src/app/api/cli-tools/cline-settings/route.js
// ---------------------------------------------------------------------------
type clineDriver struct{}

func (clineDriver) dataDir() string {
	return filepath.Join(userHomeDir(), ".cline", "data")
}

func (d clineDriver) globalStatePath() string {
	return filepath.Join(d.dataDir(), "globalState.json")
}

func (d clineDriver) secretsPath() string {
	return filepath.Join(d.dataDir(), "secrets.json")
}

func (d clineDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("cline") && !fileExists(d.globalStatePath()) {
		return false, false, nil, nil
	}
	global := map[string]any{}
	_ = readJSONC(d.globalStatePath(), &global)
	hasUs := false
	if provider, _ := global["actModeApiProvider"].(string); provider == "openai" {
		hasUs = isLocalOr9Router(fmt.Sprintf("%v", global["openAiBaseUrl"]))
	} else if provider, _ := global["planModeApiProvider"].(string); provider == "openai" {
		hasUs = isLocalOr9Router(fmt.Sprintf("%v", global["openAiBaseUrl"]))
	}
	state := map[string]any{
		"actModeApiProvider":  global["actModeApiProvider"],
		"planModeApiProvider": global["planModeApiProvider"],
		"openAiBaseUrl":       global["openAiBaseUrl"],
		"openAiModelId":       global["openAiModelId"],
	}
	return true, hasUs, state, nil
}

func (d clineDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.globalStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	global := map[string]any{}
	_ = readJSONC(path, &global)
	model := firstNonEmpty(sel.Model, modelOrDefault(sel))
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	base := normalizeBaseNoV1(sel.BaseURL)
	global["actModeApiProvider"] = "openai"
	global["planModeApiProvider"] = "openai"
	global["openAiBaseUrl"] = base
	global["openAiModelId"] = model
	global["planModeOpenAiModelId"] = model

	backup, _ := backupExistingFile(path)
	if err := writeJSONPretty(path, global); err != nil {
		restoreBackup(path, backup)
		return CLIToolConfig{}, err
	}
	secrets := map[string]any{}
	_ = readJSONC(d.secretsPath(), &secrets)
	secrets["openAiApiKey"] = firstNonEmpty(apiKey, "sk_9router")
	_ = writeJSONPretty(d.secretsPath(), secrets)

	bs, _ := json.MarshalIndent(global, "", " ")
	return CLIToolConfig{
		ConfigPath:    path,
		BackupPath:    backup,
		ConfigContent: string(bs),
		RunCommand:    "cline",
	}, nil
}

func (d clineDriver) reset(ctx context.Context) error {
	path := d.globalStatePath()
	global := map[string]any{}
	if !readJSONC(path, &global) {
		return nil
	}
	if provider, _ := global["actModeApiProvider"].(string); provider == "openai" {
		delete(global, "openAiBaseUrl")
		delete(global, "openAiModelId")
		delete(global, "planModeOpenAiModelId")
		global["actModeApiProvider"] = "cline"
		global["planModeApiProvider"] = "cline"
		_ = writeJSONPretty(path, global)
	}
	secrets := map[string]any{}
	if readJSONC(d.secretsPath(), &secrets) {
		delete(secrets, "openAiApiKey")
		_ = writeJSONPretty(d.secretsPath(), secrets)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kilo Code
// 9router: src/app/api/cli-tools/kilo-settings/route.js
// ---------------------------------------------------------------------------
type kiloDriver struct{}

func (kiloDriver) dataDir() string {
	return filepath.Join(userHomeDir(), ".local", "share", "kilo")
}

func (d kiloDriver) authPath() string {
	return filepath.Join(d.dataDir(), "auth.json")
}

func (d kiloDriver) vscodeSettingsPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User", "settings.json")
	}
	return filepath.Join(userHomeDir(), ".config", "Code", "User", "settings.json")
}

func (d kiloDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("kilo") && !fileExists(d.authPath()) {
		return false, false, nil, nil
	}
	auth := map[string]any{}
	_ = readJSONC(d.authPath(), &auth)
	hasUs := false
	var entry map[string]any
	if e, ok := auth["openai-compatible"].(map[string]any); ok {
		entry = e
	} else if e, ok := auth["9router"].(map[string]any); ok {
		entry = e
	}
	if entry != nil {
		base, _ := entry["baseUrl"].(string)
		if base == "" {
			base, _ = entry["baseURL"].(string)
		}
		hasUs = isLocalOr9Router(base)
	}
	keys := make([]string, 0, len(auth))
	for k := range auth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	state := map[string]any{
		"auth":       keys,
		"authPath":   d.authPath(),
	}
	return true, hasUs, state, nil
}

func (d kiloDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.authPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	model := firstNonEmpty(sel.Model, modelOrDefault(sel))
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "sk_9router")
	auth := map[string]any{}
	_ = readJSONC(path, &auth)
	auth["openai-compatible"] = map[string]any{
		"type":    "api-key",
		"apiKey":  key,
		"baseUrl": base,
		"model":   model,
	}
	backup, _ := backupExistingFile(path)
	if err := writeJSONPretty(path, auth); err != nil {
		restoreBackup(path, backup)
		return CLIToolConfig{}, err
	}

	// Best-effort VS Code settings update.
	if vpath := d.vscodeSettingsPath(); fileExists(vpath) {
		vs := map[string]any{}
		if readJSONC(vpath, &vs) {
			vs["kilocode.customProvider"] = map[string]any{
				"name":    "9Router",
				"baseURL": base,
				"apiKey":  key,
			}
			vs["kilocode.defaultModel"] = model
			_ = writeJSONPretty(vpath, vs)
		}
	}

	bs, _ := json.MarshalIndent(auth, "", " ")
	return CLIToolConfig{
		ConfigPath:    path,
		BackupPath:    backup,
		ConfigContent: string(bs),
		RunCommand:    "kilo",
	}, nil
}

func (d kiloDriver) reset(ctx context.Context) error {
	path := d.authPath()
	auth := map[string]any{}
	if !readJSONC(path, &auth) {
		return nil
	}
	delete(auth, "openai-compatible")
	delete(auth, "9router")
	if err := writeJSONPretty(path, auth); err != nil {
		return err
	}
	if vpath := d.vscodeSettingsPath(); fileExists(vpath) {
		vs := map[string]any{}
		if readJSONC(vpath, &vs) {
			delete(vs, "kilocode.customProvider")
			delete(vs, "kilocode.defaultModel")
			_ = writeJSONPretty(vpath, vs)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Factory Droid
// 9router: src/app/api/cli-tools/droid-settings/route.js
// ---------------------------------------------------------------------------
type droidDriver struct{}

func (d droidDriver) settingsDir() string {
	return filepath.Join(userHomeDir(), ".factory")
}

func (d droidDriver) settingsPath() string {
	return filepath.Join(d.settingsDir(), "settings.json")
}

func (d droidDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("droid") && !fileExists(d.settingsPath()) {
		return false, false, nil, nil
	}
	settings := map[string]any{}
	_ = readJSONC(d.settingsPath(), &settings)
	hasUs := false
	if list, ok := settings["customModels"].([]any); ok {
		for _, m := range list {
			if mm, ok := m.(map[string]any); ok {
				if id, _ := mm["id"].(string); strings.HasPrefix(id, "custom:9Router") {
					hasUs = true
					break
				}
			}
		}
	}
	state := map[string]any{
		"settings":    settings,
		"settingsPath": d.settingsPath(),
	}
	return true, hasUs, state, nil
}

func (d droidDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	modelsArray := []string{}
	if len(sel.Models) > 0 {
		modelsArray = append(modelsArray, sel.Models...)
	} else if sel.Model != "" {
		modelsArray = append(modelsArray, sel.Model)
	}
	clean := modelsArray[:0]
	for _, m := range modelsArray {
		if m != "" {
			clean = append(clean, m)
		}
	}
	modelsArray = clean
	if len(modelsArray) == 0 {
		return CLIToolConfig{}, fmt.Errorf("at least one model is required")
	}
	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "your_api_key")

	settings := map[string]any{}
	_ = readJSONC(path, &settings)
	if settings["customModels"] == nil {
		settings["customModels"] = []any{}
	}
	list, ok := settings["customModels"].([]any)
	if !ok {
		list = []any{}
	}
	// Drop existing 9Router entries.
	kept := make([]any, 0, len(list))
	for _, m := range list {
		if mm, ok := m.(map[string]any); ok {
			if id, _ := mm["id"].(string); strings.HasPrefix(id, "custom:9Router") {
				continue
			}
		}
		kept = append(kept, m)
	}
	list = kept

	start := len(list)
	// Determine the default (active) model index.
	defaultIndex := 0
	if sel.ActiveModel == "" {
		defaultIndex = -1
	} else {
		found := false
		for i, m := range modelsArray {
			if m == sel.ActiveModel {
				defaultIndex = i
				found = true
				break
			}
		}
		if !found {
			defaultIndex = 0
		}
	}

	for i, m := range modelsArray {
		list = append(list, map[string]any{
			"model":             m,
			"id":                fmt.Sprintf("custom:9Router-%d", i),
			"index":             i,
			"baseUrl":           base,
			"apiKey":            key,
			"displayName":       m,
			"maxOutputTokens":   131072,
			"noImageSupport":    false,
			"provider":          "openai",
		})
	}

	if defaultIndex >= 0 {
		actualIdx := start + defaultIndex
		if actualIdx < len(list) {
			defaultEntry := list[actualIdx]
			rest := append(list[:actualIdx], list[actualIdx+1:]...)
			list = append([]any{defaultEntry}, rest...)
		}
	}
	for i := range list {
		if mm, ok := list[i].(map[string]any); ok {
			mm["index"] = i
		}
	}
	settings["customModels"] = list

	bs, err := json.MarshalIndent(settings, "", " ")
	if err != nil {
		return CLIToolConfig{}, err
	}
	backup, _ := backupExistingFile(path)
	if err := os.WriteFile(path, bs, 0o644); err != nil {
		restoreBackup(path, backup)
		return CLIToolConfig{}, err
	}
	return CLIToolConfig{
		ConfigPath:    path,
		BackupPath:    backup,
		ConfigContent: string(bs),
		RunCommand:    "droid",
	}, nil
}

func (d droidDriver) reset(ctx context.Context) error {
	path := d.settingsPath()
	settings := map[string]any{}
	if !readJSONC(path, &settings) {
		return nil
	}
	if list, ok := settings["customModels"].([]any); ok {
		kept := make([]any, 0, len(list))
		for _, m := range list {
			if mm, ok := m.(map[string]any); ok {
				if id, _ := mm["id"].(string); strings.HasPrefix(id, "custom:9Router") {
					continue
				}
			}
			kept = append(kept, m)
		}
		if len(kept) == 0 {
			delete(settings, "customModels")
		} else {
			settings["customModels"] = kept
		}
	}
	return writeJSONPretty(path, settings)
}

// ---------------------------------------------------------------------------
// Hermes Agent
// 9router: src/app/api/cli-tools/hermes-settings/route.js
// ---------------------------------------------------------------------------
type hermesDriver struct{}

func (d hermesDriver) dir() string {
	return filepath.Join(userHomeDir(), ".hermes")
}

func (d hermesDriver) configPath() string {
	return filepath.Join(d.dir(), "config.yaml")
}

func (d hermesDriver) envPath() string {
	return filepath.Join(d.dir(), ".env")
}

var hermesModelBlockRe = regexp.MustCompile(`(?m)^model:[ \t]*\r?\n((?:[ \t]+.*\r?\n?|[ \t]*\r?\n)*)`)

func parseHermesModelBlock(yaml string) map[string]string {
	m := hermesModelBlockRe.FindStringSubmatch(yaml)
	if m == nil {
		return nil
	}
	body := m[1]
	out := map[string]string{}
	for _, key := range []string{"default", "provider", "base_url"} {
		re := regexp.MustCompile(`(?m)^[ \t]+` + regexp.QuoteMeta(key) + `:[ \t]*["']?([^"'#\r\n]+)["']?`)
		if sm := re.FindStringSubmatch(body); sm != nil {
			out[key] = strings.TrimSpace(sm[1])
		}
	}
	return out
}

func buildHermesModelBlock(model, base string) string {
	return fmt.Sprintf("model:\n default: %q\n provider: \"custom\"\n base_url: %q\n", model, base)
}

func upsertHermesModelBlock(yaml, block string) string {
	if hermesModelBlockRe.MatchString(yaml) {
		return hermesModelBlockRe.ReplaceAllString(yaml, block)
	}
	if len(yaml) > 0 {
		return block + "\n" + yaml
	}
	return block
}

func removeHermesModelBlock(yaml string) string {
	return strings.TrimPrefix(hermesModelBlockRe.ReplaceAllString(yaml, ""), "\n")
}

func upsertEnvLine(envText, key, value string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=.*$`)
	line := key + "=" + value
	if re.MatchString(envText) {
		return re.ReplaceAllString(envText, line)
	}
	if envText != "" && !strings.HasSuffix(envText, "\n") {
		return envText + "\n" + line + "\n"
	}
	return envText + line + "\n"
}

func removeEnvLine(envText, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=.*\r?\n?`)
	return re.ReplaceAllString(envText, "")
}

func (d hermesDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("hermes") && !fileExists(d.configPath()) {
		return false, false, nil, nil
	}
	yaml := ""
	if b, err := os.ReadFile(d.configPath()); err == nil {
		yaml = string(b)
	}
	model := parseHermesModelBlock(yaml)
	hasUs := false
	if model != nil && model["provider"] == "custom" {
		hasUs = isLocalOr9RouterWide(model["base_url"])
	}
	state := map[string]any{
		"model":       model,
		"configPath":  d.configPath(),
	}
	return true, hasUs, state, nil
}

func (d hermesDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	model := firstNonEmpty(sel.Model, modelOrDefault(sel))
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	base := normalizeBaseV1(sel.BaseURL)
	block := buildHermesModelBlock(model, base)

	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	}
	newYaml := upsertHermesModelBlock(existing, block)
	if err := os.WriteFile(path, []byte(newYaml), 0o644); err != nil {
		return CLIToolConfig{}, err
	}

	if apiKey != "" {
		envPath := d.envPath()
		env := ""
		if b, err := os.ReadFile(envPath); err == nil {
			env = string(b)
		}
		newEnv := upsertEnvLine(env, "OPENAI_API_KEY", apiKey)
		_ = os.WriteFile(envPath, []byte(newEnv), 0o644)
	}

	return CLIToolConfig{
		ConfigPath:    path,
		ConfigContent: newYaml,
		RunCommand:    "hermes",
	}, nil
}

func (d hermesDriver) reset(ctx context.Context) error {
	path := d.configPath()
	yaml := ""
	if b, err := os.ReadFile(path); err == nil {
		yaml = string(b)
	} else if os.IsNotExist(err) {
		return nil
	} else {
		return err
	}
	newYaml := removeHermesModelBlock(yaml)
	if err := os.WriteFile(path, []byte(newYaml), 0o644); err != nil {
		return err
	}
	envPath := d.envPath()
	if b, err := os.ReadFile(envPath); err == nil {
		newEnv := removeEnvLine(string(b), "OPENAI_API_KEY")
		_ = os.WriteFile(envPath, []byte(newEnv), 0o644)
	}
	return nil
}

// ---------------------------------------------------------------------------
// DeepSeek TUI
// 9router: src/app/api/cli-tools/deepseek-tui-settings/route.js
// ---------------------------------------------------------------------------
type deepseekTuiDriver struct{}

func (d deepseekTuiDriver) dir() string {
	return filepath.Join(userHomeDir(), ".deepseek")
}

func (d deepseekTuiDriver) configPath() string {
	return filepath.Join(d.dir(), "config.toml")
}

func deepseekHasUs(cfg map[string]any) bool {
	provider, _ := cfg["provider"].(string)
	if provider != "openai" {
		return false
	}
	providers, ok := cfg["providers"].(map[string]any)
	if !ok {
		return false
	}
	openai, ok := providers["openai"].(map[string]any)
	if !ok {
		return false
	}
	base, _ := openai["base_url"].(string)
	return isLocalOr9Router(base) || strings.Contains(base, "0.0.0.0")
}

func (d deepseekTuiDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("deepseek") && !fileExists(d.configPath()) {
		return false, false, nil, nil
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(d.configPath()); err == nil {
		_ = toml.Unmarshal(b, &cfg)
	}
	hasUs := deepseekHasUs(cfg)
	state := map[string]any{
		"config":     cfg,
		"configPath": d.configPath(),
	}
	return true, hasUs, state, nil
}

func (d deepseekTuiDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	model := firstNonEmpty(sel.Model, modelOrDefault(sel))
	if model == "" {
		return CLIToolConfig{}, fmt.Errorf("model is required")
	}
	base := normalizeBaseV1(sel.BaseURL)
	content := fmt.Sprintf("provider = \"openai\"\n[providers.openai]\nbase_url = %q\napi_key = %q\nmodel = %q\n",
		base, firstNonEmpty(apiKey, "sk_9router"), model)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return CLIToolConfig{}, err
	}
	return CLIToolConfig{
		ConfigPath:    path,
		ConfigContent: content,
		RunCommand:    "deepseek",
	}, nil
}

func (d deepseekTuiDriver) reset(ctx context.Context) error {
	path := d.configPath()
	if !fileExists(path) {
		return nil
	}
	return os.WriteFile(path, []byte("provider = \"deepseek\"\n"), 0o644)
}

// ---------------------------------------------------------------------------
// jcode
// 9router: src/app/api/cli-tools/jcode-settings/route.js
// ---------------------------------------------------------------------------
type jcodeDriver struct{}

func (jcodeDriver) configDir() string {
	return filepath.Join(userHomeDir(), ".jcode")
}

func (d jcodeDriver) configPath() string {
	return filepath.Join(d.configDir(), "config.toml")
}

func (jcodeDriver) envPath() string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(userHomeDir(), ".config")
	}
	return filepath.Join(xdg, "jcode", "provider-9router.env")
}

func (d jcodeDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	if !lookPath("jcode") && !fileExists(d.configDir()) {
		return false, false, nil, nil
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(d.configPath()); err == nil {
		_ = toml.Unmarshal(b, &cfg)
	}
	if cfg["providers"] == nil {
		cfg["providers"] = map[string]any{}
	}
	hasUs := false
	providers, ok := cfg["providers"].(map[string]any)
	if ok {
		if _, exists := providers["9router"]; exists {
			hasUs = true
		} else {
			for _, pv := range providers {
				if p, ok := pv.(map[string]any); ok {
					if base, _ := p["base_url"].(string); strings.Contains(base, "localhost:20128") {
						hasUs = true
						break
					}
				}
			}
		}
	}
	state := map[string]any{
		"config":     cfg,
		"configPath": d.configPath(),
	}
	return true, hasUs, state, nil
}

func (d jcodeDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	models := collectModels(sel)
	if len(models) == 0 {
		models = []string{"cc/claude-opus-4-7"}
	}
	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "sk_9router")

	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(b, &cfg)
	}
	if cfg["providers"] == nil {
		cfg["providers"] = map[string]any{}
	}
	providers := cfg["providers"].(map[string]any)
	providers["9router"] = map[string]any{
		"type":             "openai-compatible",
		"base_url":         base,
		"auth":             "bearer",
		"api_key_env":      "JCODE_9ROUTER_API_KEY",
		"env_file":         "provider-9router.env",
		"default_model":    models[0],
		"requires_api_key": true,
	}
	cfgBytes, err := toml.Marshal(cfg)
	if err != nil {
		return CLIToolConfig{}, err
	}
	if err := os.WriteFile(path, cfgBytes, 0o644); err != nil {
		return CLIToolConfig{}, err
	}

	// Write the provider env file.
	envPath := d.envPath()
	var envLines []string
	if b, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if eq := strings.Index(t, "="); eq > 0 {
				if strings.TrimSpace(t[:eq]) != "JCODE_9ROUTER_API_KEY" {
					envLines = append(envLines, line)
				}
			}
		}
	}
	envLines = append(envLines, fmt.Sprintf(`JCODE_9ROUTER_API_KEY="%s"`, key))
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(envLines, "\n")+"\n"), 0o644); err != nil {
		return CLIToolConfig{}, err
	}

	return CLIToolConfig{
		ConfigPath:    path,
		ConfigContent: string(cfgBytes),
		RunCommand:    "jcode --provider-profile 9router",
	}, nil
}

func (d jcodeDriver) reset(ctx context.Context) error {
	path := d.configPath()
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(b, &cfg)
	} else if os.IsNotExist(err) {
		return nil
	} else {
		return err
	}
	if providers, ok := cfg["providers"].(map[string]any); ok {
		delete(providers, "9router")
		if len(providers) == 0 {
			delete(cfg, "providers")
		}
	}
	cfgBytes, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, cfgBytes, 0o644); err != nil {
		return err
	}

	envPath := d.envPath()
	if b, err := os.ReadFile(envPath); err == nil {
		lines := []string{}
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				lines = append(lines, line)
				continue
			}
			if eq := strings.Index(t, "="); eq > 0 {
				if strings.TrimSpace(t[:eq]) == "JCODE_9ROUTER_API_KEY" {
					continue
				}
			}
			lines = append(lines, line)
		}
		_ = os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GitHub Copilot (VS Code chatLanguageModels.json)
// 9router: src/app/api/cli-tools/copilot-settings/route.js
// ---------------------------------------------------------------------------
type copilotDriver struct{}

func (d copilotDriver) configPath() string {
	home := userHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User", "chatLanguageModels.json")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "chatLanguageModels.json")
	default:
		return filepath.Join(home, ".config", "Code", "User", "chatLanguageModels.json")
	}
}

func (d copilotDriver) detect(ctx context.Context) (bool, bool, map[string]any, error) {
	config := []any{}
	_ = readJSONC(d.configPath(), &config)
	hasUs := false
	var currentModel, currentURL string
	for _, e := range config {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := entry["name"].(string); name == "9Router" {
			hasUs = true
			if models, ok := entry["models"].([]any); ok && len(models) > 0 {
				if m, ok := models[0].(map[string]any); ok {
					if id, _ := m["id"].(string); id != "" {
						currentModel = id
					}
					if url, _ := m["url"].(string); url != "" {
						currentURL = url
					}
				}
			}
			break
		}
	}
	state := map[string]any{
		"config":       config,
		"configPath":   d.configPath(),
		"currentModel": currentModel,
		"currentUrl":   currentURL,
	}
	installed := fileExists(d.configPath())
	return installed, hasUs, state, nil
}

func (d copilotDriver) apply(ctx context.Context, sel CLIToolSelection, apiKey string) (CLIToolConfig, error) {
	path := d.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CLIToolConfig{}, err
	}
	models := collectModels(sel)
	if len(models) == 0 {
		models = []string{"cc/claude-sonnet-5"}
	}
	base := normalizeBaseV1(sel.BaseURL)
	key := firstNonEmpty(apiKey, "sk_9router")
	endpointURL := base + "/chat/completions#models.ai.azure.com"

	entry := map[string]any{
		"name":   "9Router",
		"vendor": "azure",
		"apiKey": key,
		"models": make([]map[string]any, 0, len(models)),
	}
	modelList := entry["models"].([]map[string]any)
	for _, id := range models {
		modelList = append(modelList, map[string]any{
			"id":               id,
			"name":             id,
			"url":              endpointURL,
			"toolCalling":      true,
			"vision":           false,
			"maxInputTokens":   128000,
			"maxOutputTokens":  16000,
		})
	}
	entry["models"] = modelList

	config := []any{}
	_ = readJSONC(path, &config)
	idx := -1
	for i, e := range config {
		if entryMap, ok := e.(map[string]any); ok {
			if name, _ := entryMap["name"].(string); name == "9Router" {
				idx = i
				break
			}
		}
	}
	if idx >= 0 {
		config[idx] = entry
	} else {
		config = append(config, entry)
	}

	bs, err := json.MarshalIndent(config, "", " ")
	if err != nil {
		return CLIToolConfig{}, err
	}
	if err := os.WriteFile(path, bs, 0o644); err != nil {
		return CLIToolConfig{}, err
	}
	return CLIToolConfig{
		ConfigPath:    path,
		ConfigContent: string(bs),
		RunCommand:    "copilot",
	}, nil
}

func (d copilotDriver) reset(ctx context.Context) error {
	path := d.configPath()
	if !fileExists(path) {
		return nil
	}
	config := []any{}
	_ = readJSONC(path, &config)
	kept := make([]any, 0, len(config))
	for _, e := range config {
		if entry, ok := e.(map[string]any); ok {
			if name, _ := entry["name"].(string); name == "9Router" {
				continue
			}
		}
		kept = append(kept, e)
	}
	bs, err := json.MarshalIndent(kept, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bs, 0o644)
}
