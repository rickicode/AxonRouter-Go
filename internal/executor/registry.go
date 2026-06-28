package executor

import (
	"fmt"
	"strings"
	"sync"
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
		return model[:idx], model[idx+1:]
	}
	return "", model
}

// RegisterDefaults registers all built-in executors.
func RegisterDefaults() {
	base := NewBaseExecutor()

	// OpenAI-compatible providers
	openaiExec := NewOpenAIExecutor(base)
	for _, p := range []string{"openai", "groq", "deepseek", "oc", "oc-zen", "oc-go", "mimocode", "mimo", "mimo-tp", "elevenlabs", "deepgram"} {
		GetRegistry().Register(p, FormatOpenAI, openaiExec)
	}

	// Claude
	claudeExec := NewClaudeExecutor(base)
	GetRegistry().Register("claude", FormatClaude, claudeExec)

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
