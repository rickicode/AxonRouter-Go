package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/signature"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
// normalizeClaudeSamplingForUpstream, ensureClaudeThinkingDisplay, extractAndRemoveBetas)
func prepareClaudeBody(body []byte) ([]byte, []string) {
	// 1. Default max_tokens to 1024 if not set (Anthropic API requires it)
	if !gjson.GetBytes(body, "max_tokens").Exists() {
		body, _ = sjson.SetBytes(body, "max_tokens", 1024)
	}

	// 2. Disable thinking on forced tool_choice (any/tool) before any other
	// thinking-related normalization. Anthropic rejects thinking + forced tool_choice.
	body = disableThinkingIfToolChoiceForced(body)

	// 3. Remove sampling params that conflict with thinking-enabled requests.
	body = normalizeClaudeSamplingForUpstream(body)

	// 4. Default thinking.display to "summarized" so thinking text is visible.
	body = ensureClaudeThinkingDisplay(body)

	// 5. Extract and remove betas from body (will be sent as anthropic-beta header)
	betas, body := extractAndRemoveBetas(body)
	return body, betas
}

// extractAndRemoveBetas extracts the "betas" array from the body and removes it.
// Returns the extracted betas as a string slice and the modified body.
// Matches CLIProxyAPI internal/runtime/executor/claude_executor.go.
func extractAndRemoveBetas(body []byte) ([]string, []byte) {
	betasResult := gjson.GetBytes(body, "betas")
	if !betasResult.Exists() {
		return nil, body
	}
	var betas []string
	if betasResult.IsArray() {
		for _, item := range betasResult.Array() {
			if s := strings.TrimSpace(item.String()); s != "" {
				betas = append(betas, s)
			}
		}
	} else if s := strings.TrimSpace(betasResult.String()); s != "" {
		betas = append(betas, s)
	}
	body, _ = sjson.DeleteBytes(body, "betas")
	return betas, body
}

// baseClaudeBetas are the experimental Claude betas that are always sent in the
// Anthropic-Beta header so experimental features stay active upstream.
// Keep in sync with CLIProxyAPI's base betas list.
var baseClaudeBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"interleaved-thinking-2025-05-14",
	"prompt-caching-scope-2026-01-05",
	"redact-thinking-2026-02-12",
	"token-efficient-tools-2026-03-28",
}

// sanitizeClaudeBody applies provider-aware signature sanitization so that
// cross-provider conversation history remains valid for a Claude upstream.
func sanitizeClaudeBody(body []byte, modelName string) []byte {
	sanitized, report := signature.SanitizeClaudeMessagesForClaudeUpstream(body, modelName)
	if total := report.Preserved + report.DroppedBlocks + report.DroppedSignatures + report.ReplacedSignatures; total > 0 {
		logging.Logger.Debug(
			"Claude message signature sanitization report",
			"target_provider", report.TargetProvider,
			"preserved", report.Preserved,
			"dropped_blocks", report.DroppedBlocks,
			"dropped_signatures", report.DroppedSignatures,
			"replaced_signatures", report.ReplacedSignatures,
		)
	}
	return sanitized
}

// claudeBetaHeader builds the Anthropic-Beta header value. Base betas are always
// included; client-provided header betas and body-extracted betas are merged on
// top and deduplicated.
func claudeBetaHeader(bodyBetas []string, reqHeaders map[string]string) string {
	seen := make(map[string]struct{}, len(baseClaudeBetas)+len(bodyBetas))
	var merged []string

	addBeta := func(b string) {
		b = strings.TrimSpace(b)
		if b == "" {
			return
		}
		if _, ok := seen[b]; ok {
			return
		}
		seen[b] = struct{}{}
		merged = append(merged, b)
	}

	for _, b := range baseClaudeBetas {
		addBeta(b)
	}

	if reqHeaders != nil {
		for _, key := range []string{"anthropic-beta", "Anthropic-Beta"} {
			for _, b := range strings.Split(reqHeaders[key], ",") {
				addBeta(b)
			}
		}
	}

	for _, b := range bodyBetas {
		addBeta(b)
	}

	if len(merged) == 0 {
		return ""
	}
	return strings.Join(merged, ",")
}

// Execute performs a non-streaming Claude messages request.
func (e *ClaudeExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}

	body, betas := prepareClaudeBody(req.Body)
	body = sanitizeClaudeBody(body, req.Model)
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
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
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
	body = sanitizeClaudeBody(body, req.Model)
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
			upErr.TranslateErrorBody(req.Provider)
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
	body = sanitizeClaudeBody(body, req.Model)

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
