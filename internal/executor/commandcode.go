package executor

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
)

// commandcodeDefaultBaseURL is the upstream CommandCode AI base URL.
const commandcodeDefaultBaseURL = "https://api.commandcode.ai"

// commandcodeMaxTokens is the hard server-side ceiling enforced by CommandCode's
// /alpha/generate endpoint. Client-supplied positive values above this are
// clamped down; absent/non-positive values are omitted.
const commandcodeMaxTokens = 200_000

// CommandCodeExecutor routes OpenAI-compatible chat requests to CommandCode AI.
// It performs the CommandCode-specific request transform (model stays as-is,
// max_tokens clamped, system messages merged) and uses the generic OpenAI
// executor for the HTTP call.
type CommandCodeExecutor struct {
	*OpenAIExecutor
}

// NewCommandCodeExecutor creates a new CommandCode executor.
func NewCommandCodeExecutor(base *BaseExecutor) *CommandCodeExecutor {
	return &CommandCodeExecutor{OpenAIExecutor: NewOpenAIExecutor(base)}
}

// chatURL returns the CommandCode /alpha/generate endpoint.
func (e *CommandCodeExecutor) chatURL(baseURL string) string {
	return strings.TrimRight(commandcodeBaseURL(baseURL), "/") + "/alpha/generate"
}

// Execute performs a non-streaming chat completion.
func (e *CommandCodeExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	url := e.chatURL(modified.BaseURL)
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, modified.APIKey, modified.AccessToken)
	resp, err := e.DoRequest(ctx, "POST", url, headers, modified.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody(modified.Provider)
		return nil, upErr
	}
	return resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *CommandCodeExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	url := e.chatURL(modified.BaseURL)
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, modified.APIKey, modified.AccessToken)
	return e.DoStreamRequestWithConfig(ContextWithProvider(ctx, modified.Provider), "POST", url, headers, modified.Body, modified.StreamConfig)
}

// Models lists available CommandCode models from the upstream endpoint.
func (e *CommandCodeExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	baseURL := commandcodeBaseURL(req.BaseURL)
	url := strings.TrimRight(baseURL, "/") + "/provider/v1/models"
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	return e.DoRequest(ctx, "GET", url, headers, nil)
}

// prepareRequest applies CommandCode-specific normalization and returns a request
// pointer that must not be mutated by callers other than through the returned copy.
func (e *CommandCodeExecutor) prepareRequest(req *Request) (*Request, error) {
	cp := cloneRequest(req)
	// Model: CommandCode expects the full upstream model ID. The handler already
	// strips gateway prefix, so leave the model as-is.
	c := providercfg.CompatibilityFor(cp.Provider)
	// Use a compatibility config that strips the gateway prefix if the client
	// somehow still includes it (e.g. combo fallback).
	c.StripProviderPrefix = "commandcode/"
	body := sanitizeRequestWithCompatibility(cp.Body, c)
	body = normalizeCommandCodeBody(body)
	body = JSONSet(body, "model", ExtractModel(gjson.GetBytes(body, "model").String()))
	// CommandCode only accepts chat/completions-style calls on /alpha/generate.
	cp.Body = body
	cp.BaseURL = commandcodeBaseURL(cp.BaseURL)
	return cp, nil
}

// commandcodeBaseURL returns the effective CommandCode base URL.
func commandcodeBaseURL(baseURL string) string {
	if baseURL == "" {
		return commandcodeDefaultBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

// normalizeCommandCodeBody applies minimal CommandCode request normalization:
//  1. Merges system/developer messages into a single top-level system string.
//  2. Clamps a client-supplied max_tokens to commandcodeMaxTokens.
//  3. Removes empty/nil tool blocks.
func normalizeCommandCodeBody(body []byte) []byte {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	// Merge system + developer messages.
	var system []string
	if msgs, ok := req["messages"].([]any); ok {
		var out []any
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				out = append(out, m)
				continue
			}
			role, _ := msg["role"].(string)
			switch role {
			case "system", "developer":
				system = append(system, contentText(msg["content"]))
			default:
				out = append(out, m)
			}
		}
		req["messages"] = out
	}
	if len(system) > 0 {
		req["system"] = strings.Join(system, "\n\n")
	}

	// Clamp max_tokens.
	if mt, ok := req["max_tokens"]; ok {
		switch v := mt.(type) {
		case float64:
			if v > 0 && v <= commandcodeMaxTokens {
				req["max_tokens"] = int64(v)
			} else if v > commandcodeMaxTokens {
				req["max_tokens"] = commandcodeMaxTokens
			} else {
				delete(req, "max_tokens")
			}
		default:
			delete(req, "max_tokens")
		}
	}

	// Omit empty tools.
	if tools, ok := req["tools"].([]any); ok && len(tools) == 0 {
		delete(req, "tools")
	}

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	// Keep any passthrough fields that json round-trip might have changed.
	// Re-apply explicit stream choice (OpenAIExecutor will enforce later).
	return out
}

func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		texts := make([]string, 0, len(x))
		for _, part := range x {
			if m, ok := part.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n")
	case map[string]any:
		if t, ok := x["text"].(string); ok {
			return t
		}
	}
	return ""
}
