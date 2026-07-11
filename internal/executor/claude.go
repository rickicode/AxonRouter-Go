package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator/providers"
)

// ClaudeExecutor handles Anthropic Claude API.
type ClaudeExecutor struct {
	*BaseExecutor
}

// NewClaudeExecutor creates a new Claude executor.
func NewClaudeExecutor(base *BaseExecutor) *ClaudeExecutor {
	return &ClaudeExecutor{BaseExecutor: base}
}

// prepareClaudeBody applies Anthropic-specific body defaults and constraints.
// Returns the modified body and any betas extracted from it.
// (matches CLIProxyAPI: ensureModelMaxTokens, disableThinkingIfToolChoiceForced,
// normalizeClaudeTemperatureForThinking, extractAndRemoveBetas)
func prepareClaudeBody(body []byte) ([]byte, []string) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, nil
	}

	// 1. Default max_tokens to 1024 if not set (Anthropic API requires it)
	if _, ok := m["max_tokens"]; !ok {
		m["max_tokens"] = 1024
	}

	// 2. Disable thinking on forced tool_choice (any/tool)
	// Anthropic rejects thinking + forced tool_choice
	if tc, ok := m["tool_choice"].(map[string]any); ok {
		if t, ok := tc["type"].(string); ok && (t == "any" || t == "tool") {
			delete(m, "thinking")
			if oc, ok := m["output_config"].(map[string]any); ok {
				delete(oc, "effort")
				if len(oc) == 0 {
					delete(m, "output_config")
				}
			}
		}
	}

	// 3. Normalize temperature to 1 when thinking is enabled
	// Anthropic rejects temperatures other than 1 with thinking
	if thinking, ok := m["thinking"].(map[string]any); ok {
		if t, ok := thinking["type"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "enabled", "adaptive", "auto":
				if temp, ok := m["temperature"].(float64); !ok || temp != 1 {
					m["temperature"] = 1
				}
			}
		}
	}

	// 4. Extract and remove betas from body (will be sent as anthropic-beta header)
	var betas []string
	if raw, ok := m["betas"]; ok {
		delete(m, "betas")
		switch v := raw.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					betas = append(betas, strings.TrimSpace(s))
				}
			}
		case string:
			if s := strings.TrimSpace(v); s != "" {
				betas = append(betas, s)
			}
		}
	}

	out, _ := json.Marshal(m)
	return out, betas
}

// claudeBetaHeader builds the anthropic-beta header value from body-extracted betas
// and any client-provided header. Client header takes precedence if present.
func claudeBetaHeader(bodyBetas []string, reqHeaders map[string]string) string {
	if clientBeta := strings.TrimSpace(reqHeaders["anthropic-beta"]); clientBeta != "" {
		return clientBeta
	}
	if clientBeta := strings.TrimSpace(reqHeaders["Anthropic-Beta"]); clientBeta != "" {
		return clientBeta
	}
	if len(bodyBetas) > 0 {
		return strings.Join(bodyBetas, ",")
	}
	return ""
}

// Execute performs a non-streaming Claude messages request.
func (e *ClaudeExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}

	body, betas := prepareClaudeBody(req.Body)
	// Ensure stream is false
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}
	if beta := claudeBetaHeader(betas, req.Headers); beta != "" {
		headers["anthropic-beta"] = beta
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       providers.TranslateClaude(resp.StatusCode, resp.Body),
			RawBody:    resp.Body,
		}
	}

	return resp, nil
}

// ExecuteStream performs a streaming Claude messages request.
func (e *ClaudeExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}

	body, betas := prepareClaudeBody(req.Body)
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":      "application/json",
		"Accept":            "text/event-stream",
		"Cache-Control":     "no-cache",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}
	if beta := claudeBetaHeader(betas, req.Headers); beta != "" {
		headers["anthropic-beta"] = beta
	}

	result, err := e.DoStreamRequest(ctx, "POST", url, headers, body)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.Body = providers.TranslateClaude(upErr.StatusCode, upErr.RawBody)
		}
	}
	return result, err
}

// CountTokens performs token counting.
func (e *ClaudeExecutor) CountTokens(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages/count_tokens"
	}

	body, betas := prepareClaudeBody(req.Body)

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}
	if beta := claudeBetaHeader(betas, req.Headers); beta != "" {
		headers["anthropic-beta"] = beta
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("claude count_tokens error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}
