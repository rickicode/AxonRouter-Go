package executor

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/rickicode/AxonRouter-Go/internal/executor/translator/providers"
)

// ProviderFormat identifies the native API format of a provider.
type ProviderFormat string

const (
	FormatOpenAI          ProviderFormat = "openai"
	FormatClaude          ProviderFormat = "claude"
	FormatGemini          ProviderFormat = "gemini"
	FormatOpenAIResponses ProviderFormat = "openai-responses"
	FormatAntigravity     ProviderFormat = "antigravity"
	FormatKiro            ProviderFormat = "kiro"
)

// Registry maps provider prefixes to executors.
type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
	formats   map[string]ProviderFormat
}

var globalRegistry = &Registry{
	executors: make(map[string]Executor),
	formats:   make(map[string]ProviderFormat),
}

// GetRegistry returns the global executor registry.
func GetRegistry() *Registry {
	return globalRegistry
}

// Register adds an executor for a provider prefix.
func (r *Registry) Register(prefix string, format ProviderFormat, exec Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[prefix] = exec
	r.formats[prefix] = format
}

// Get returns the executor for a provider prefix.
func (r *Registry) Get(prefix string) (Executor, ProviderFormat, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exec, ok := r.executors[prefix]
	if !ok {
		return nil, "", false
	}
	format := r.formats[prefix]
	return exec, format, true
}

// GetByModel resolves a model string like "openai/gpt-4o" to its executor.
func (r *Registry) GetByModel(model string) (Executor, ProviderFormat, string, error) {
	prefix, modelName := SplitModel(model)
	exec, format, ok := r.Get(prefix)
	if !ok {
		return nil, "", "", fmt.Errorf("unknown provider prefix: %s", prefix)
	}
	return exec, format, modelName, nil
}

// List returns all registered provider prefixes.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prefixes := make([]string, 0, len(r.executors))
	for p := range r.executors {
		prefixes = append(prefixes, p)
	}
	return prefixes
}

// SplitModel splits "prefix/model" into prefix and model name.
// If no prefix, returns empty prefix and full model.
func SplitModel(model string) (prefix, name string) {
	if idx := strings.Index(model, "/"); idx >= 0 {
		prefix = model[:idx]
		name = model[idx+1:]
		// Strip leading "@" — CF models use "@cf/vendor/model" format
		prefix = strings.TrimPrefix(prefix, "@")
		return prefix, name
	}
	return "", model
}

// sharedBase is the BaseExecutor instance shared by all registered executors.
// It owns the idle-connection pools that must be flushed when proxy state changes.
var sharedBase *BaseExecutor

// CloseIdleConnections flushes idle keep-alive connections across the shared
// base executor (default + all cached proxy clients). Safe to call any time.
func CloseIdleConnections() {
	if sharedBase != nil {
		sharedBase.CloseIdleConnections()
	}
}

// RegisterDefaults registers all built-in executors.
func RegisterDefaults() {
	base := NewBaseExecutor()
	sharedBase = base

	// OpenAI-compatible providers
	openaiExec := NewOpenAIExecutor(base)
	for _, p := range []string{"openai", "groq", "deepseek", "oc", "oc-zen", "oc-go", "mimocode", "mimo-tp", "openrouter", "elevenlabs", "deepgram"} {
		GetRegistry().Register(p, FormatOpenAI, openaiExec)
	}

	// Cloudflare Workers AI uses dedicated executor for sanitization.
	cfExec := NewCloudflareExecutor(openaiExec)
	GetRegistry().Register("cf", FormatOpenAI, cfExec)
	translator.Register("cf", translator.Func(providers.TranslateCloudflare))

	// Provider-specific error translators.
	translator.Register("claude", translator.Func(providers.TranslateClaude))
	translator.Register("zai", translator.Func(providers.TranslateClaude))
	translator.Register("gemini", translator.Func(providers.TranslateGemini))
	translator.Register("ag", translator.Func(providers.TranslateAntigravity))
	translator.Register("cx", translator.Func(providers.TranslateCodex))

	// Claude + compatible providers
	claudeExec := NewClaudeExecutor(base)
	for _, p := range []string{"claude", "zai"} {
		GetRegistry().Register(p, FormatClaude, claudeExec)
	}

	// Gemini
	geminiExec := NewGeminiExecutor(base)
	GetRegistry().Register("gemini", FormatGemini, geminiExec)

	// Codex (openai-responses format)
	codexExec := NewCodexExecutor(base)
	GetRegistry().Register("cx", FormatOpenAIResponses, codexExec)

	// Antigravity
	agExec := NewAntigravityExecutor(base)
	GetRegistry().Register("ag", FormatAntigravity, agExec)

	// Kiro
	kiroExec := NewKiroExecutor(base)
	GetRegistry().Register("kiro", FormatKiro, kiroExec)
}

// RegisterCustomProviders registers all user-added custom providers from the DB
// so they become routable and appear in /v1/models. Must run after RegisterDefaults.
func RegisterCustomProviders(db *sql.DB) {
	reg := GetRegistry()
	base := NewBaseExecutor()
	openaiExec := NewOpenAIExecutor(base)
	claudeExec := NewClaudeExecutor(base)
	geminiExec := NewGeminiExecutor(base)
	agExec := NewAntigravityExecutor(base)
	kiroExec := NewKiroExecutor(base)

	rows, err := db.Query(`SELECT id, format FROM provider_types WHERE is_custom = 1`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, format string
		if err := rows.Scan(&id, &format); err != nil {
			continue
		}
		registerCustomProvider(reg, openaiExec, claudeExec, geminiExec, agExec, kiroExec, id, format)
	}
}

// registerCustomProvider maps a provider format to the reusable built-in executor
// and translator so the custom provider routes and translates like a built-in.
func registerCustomProvider(reg *Registry, openaiExec, claudeExec, geminiExec, agExec, kiroExec Executor, id, format string) {
	switch format {
	case "anthropic", "claude":
		reg.Register(id, FormatClaude, claudeExec)
		translator.Register(id, translator.Func(providers.TranslateClaude))
	case "gemini":
		reg.Register(id, FormatGemini, geminiExec)
		translator.Register(id, translator.Func(providers.TranslateGemini))
	case "antigravity":
		reg.Register(id, FormatAntigravity, agExec)
		translator.Register(id, translator.Func(providers.TranslateAntigravity))
	case "kiro":
		reg.Register(id, FormatKiro, kiroExec)
		translator.Register(id, translator.Func(providers.TranslateKiro))
	default: // openai, openai-responses, and unknown -> OpenAI-compatible
		reg.Register(id, FormatOpenAI, openaiExec)
	}
}

// Unregister removes a provider prefix from the registry (used on custom provider delete).
func (r *Registry) Unregister(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.executors, prefix)
	delete(r.formats, prefix)
}
