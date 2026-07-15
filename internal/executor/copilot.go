package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	copilotDefaultAuthURL = "https://api.github.com/copilot_internal/v2/token"
	copilotDefaultAPIBase = "https://api.githubcopilot.com"

	copilotAPIVersion              = "2026-06-01"
	copilotEditorVersion           = "vscode/1.126.0"
	copilotChatPluginVersion       = "copilot-chat/0.54.0"
	copilotChatUserAgent           = "GitHubCopilotChat/0.54.0"
	copilotRefreshUserAgent        = "GithubCopilot/1.0"
	copilotRefreshPluginVersion    = "copilot/1.388.0"
	copilotIntegrationID           = "vscode-chat"
	copilotOpenAIIntent            = "conversation-panel"
	copilotUserAgentLibraryVersion = "electron-fetch"

	copilotTokenSkew = 5 * time.Minute
)

// copilotToken is the short-lived bearer token returned by GitHub's Copilot
// token exchange endpoint.
type copilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// CopilotExecutor routes chat completion requests to GitHub Copilot. It handles
// the OAuth-token → Copilot-token exchange, token caching, and the extra
// headers Copilot's OpenAI-compatible proxy expects.
type CopilotExecutor struct {
	*OpenAIExecutor
	mu     sync.RWMutex
	tokens map[string]*copilotToken
}

// NewCopilotExecutor creates a new GitHub Copilot executor.
func NewCopilotExecutor(base *BaseExecutor) *CopilotExecutor {
	return &CopilotExecutor{
		OpenAIExecutor: NewOpenAIExecutor(base),
		tokens:         make(map[string]*copilotToken),
	}
}

// Execute performs a non-streaming chat completion via GitHub Copilot.
func (e *CopilotExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	body := e.prepareBody(req.Body, false)
	url, err := e.buildURL(token, req.ProviderSpecificData, body)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, body, req.Headers, false)
	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
	}
	return resp, nil
}

// ExecuteStream performs a streaming chat completion via GitHub Copilot.
func (e *CopilotExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	body := e.prepareBody(req.Body, true)
	url, err := e.buildURL(token, req.ProviderSpecificData, body)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, body, req.Headers, true)
	return e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, req.StreamConfig)
}

// prepareBody applies stream flag, strips provider prefix, and sanitizes the
// request for GitHub Copilot's OpenAI-compatible endpoints.
func (e *CopilotExecutor) prepareBody(body []byte, stream bool) []byte {
	body = JSONSet(body, "stream", stream)
	body = stripCopilotModelPrefix(body)
	body = sanitizeCopilotBody(body)
	return body
}

// Embeddings is not supported by GitHub Copilot.
func (e *CopilotExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: embeddings endpoint not supported")
}

// Images is not supported by GitHub Copilot.
func (e *CopilotExecutor) Images(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: images endpoint not supported")
}

// Responses is not supported by GitHub Copilot.
func (e *CopilotExecutor) Responses(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: responses endpoint not supported")
}

// ResponsesStream is not supported by GitHub Copilot.
func (e *CopilotExecutor) ResponsesStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, errors.New("github copilot: responses endpoint not supported")
}

// Models fetches the available Copilot models from the active endpoint.
func (e *CopilotExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	url, err := openAIEndpoint(copilotAPIBase(token), "models", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, nil, req.Headers, false)
	resp, err := e.DoRequest(ctx, "GET", url, headers, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("copilot models error %d: %s", resp.StatusCode, string(resp.Body))
	}
	return resp, nil
}

// ensureToken returns a cached Copilot token or fetches a new one using the
// request's access token (preferred) or API key (migration fallback).
func (e *CopilotExecutor) ensureToken(req *Request) (*copilotToken, error) {
	oauth := req.AccessToken
	if oauth == "" {
		oauth = req.APIKey // migration fallback for connections stored before OAuth
	}
	if oauth == "" {
		return nil, errors.New("github copilot: missing OAuth token; add it as the connection access token")
	}

	now := time.Now().Unix()
	skew := int64(copilotTokenSkew.Seconds())

	e.mu.RLock()
	tok := e.tokens[oauth]
	e.mu.RUnlock()
	if tok != nil && tok.ExpiresAt > now+skew {
		return tok, nil
	}

	// Prefer a Copilot token persisted in provider-specific data if it is not
	// near expiry; otherwise refresh via GitHub's internal token endpoint.
	if psdTok := req.ProviderSpecificData["copilotToken"]; psdTok != "" {
		if expStr := req.ProviderSpecificData["copilotTokenExpiresAt"]; expStr != "" {
			if expiresAt, err := strconv.ParseInt(expStr, 10, 64); err == nil && expiresAt > now+skew {
				return &copilotToken{Token: psdTok, ExpiresAt: expiresAt}, nil
			}
		}
	}

	return e.fetchToken(oauth)
}

// fetchToken calls GitHub's Copilot token exchange endpoint.
func (e *CopilotExecutor) fetchToken(oauthToken string) (*copilotToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotDefaultAuthURL, nil)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "token "+oauthToken)
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("User-Agent", copilotRefreshUserAgent)
	hreq.Header.Set("Editor-Version", copilotEditorVersion)
	hreq.Header.Set("Editor-Plugin-Version", copilotRefreshPluginVersion)

	client := http.DefaultClient
	if e.Client != nil {
		client = e.Client
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token exchange returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tok copilotToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("copilot token exchange parse error: %w", err)
	}
	if tok.ExpiresAt == 0 {
		tok.ExpiresAt = time.Now().Unix() + 3600
	}

	e.mu.Lock()
	e.tokens[oauthToken] = &tok
	e.mu.Unlock()

	return &tok, nil
}

// copilotHeaders builds the request headers Copilot's OpenAI-compatible proxy
// expects. stream selects the Accept header; reqHeaders may supply x-initiator.
func (e *CopilotExecutor) copilotHeaders(token string, body []byte, reqHeaders map[string]string, stream bool) map[string]string {
	initiator := copilotInitiator(body, reqHeaders)

	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}

	return map[string]string{
		"Accept":                              accept,
		"Content-Type":                        "application/json",
		"Authorization":                       "Bearer " + token,
		"User-Agent":                          copilotChatUserAgent,
		"Editor-Version":                      copilotEditorVersion,
		"Editor-Plugin-Version":               copilotChatPluginVersion,
		"Copilot-Integration-Id":              copilotIntegrationID,
		"Openai-Intent":                       copilotOpenAIIntent,
		"X-GitHub-Api-Version":                copilotAPIVersion,
		"X-VSCode-User-Agent-Library-Version": copilotUserAgentLibraryVersion,
		"X-Initiator":                         initiator,
	}
}

// copilotInitiator returns "agent" when the trailing message role is assistant
// or when the client explicitly sends x-initiator: agent. Otherwise it returns
// "user".
func copilotInitiator(body []byte, reqHeaders map[string]string) string {
	if v := getHeader(reqHeaders, "x-initiator"); v == "agent" || v == "user" {
		return v
	}
	if len(body) > 0 {
		msgs := gjson.GetBytes(body, "messages")
		if msgs.IsArray() {
			arr := msgs.Array()
			if len(arr) > 0 {
				if r := arr[len(arr)-1].Get("role").String(); r == "assistant" {
					return "agent"
				}
			}
		}
	}
	return "user"
}

// getHeader performs a case-insensitive lookup in a header map.
func getHeader(headers map[string]string, key string) string {
	lower := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return ""
}

// copilotAPIBase returns the upstream API host from the token, or the public
// GitHub Copilot default if missing.
func copilotAPIBase(tok *copilotToken) string {
	if tok != nil && tok.Endpoints.API != "" {
		return tok.Endpoints.API
	}
	return copilotDefaultAPIBase
}

// buildURL picks the upstream endpoint. Codex and other models tagged for the
// Responses API are routed to /responses, unless they are Claude/Gemini
// variants which Copilot does not support on that endpoint.
func (e *CopilotExecutor) buildURL(tok *copilotToken, psd map[string]string, body []byte) (string, error) {
	base := copilotAPIBase(tok)
	model := ExtractModel(gjson.GetBytes(body, "model").String())

	if psd == nil {
		psd = map[string]string{}
	}

	if shouldUseResponsesEndpoint(model, psd) {
		if respBase := psd["responsesBaseUrl"]; respBase != "" {
			respBase = strings.TrimRight(respBase, "/")
			return respBase, nil
		}
		u := strings.TrimRight(base, "/")
		u = strings.TrimSuffix(u, "/chat/completions")
		return openAIEndpoint(u, "responses", psd)
	}
	return openAIEndpoint(base, "chat/completions", psd)
}

// shouldUseResponsesEndpoint decides whether the model should hit Copilot's
// /responses endpoint. It mirrors OmniRoute's logic: target_format=responses
// (from models.json) or a codex name, but never Claude or Gemini variants.
func shouldUseResponsesEndpoint(model string, psd map[string]string) bool {
	if models.GetModelTargetFormat("copilot", model) == "openai-responses" {
		return true
	}
	m := strings.ToLower(model)
	if strings.Contains(m, "gemini") || strings.Contains(m, "claude") {
		return false
	}
	if strings.Contains(m, "codex") {
		return true
	}
	switch m {
	case "gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5-mini",
		"mai-code-1-flash", "oswe-vscode-prime":
		return true
	}
	return false
}

// stripCopilotModelPrefix removes the "copilot/" prefix from model IDs so the
// upstream Copilot proxy receives bare model names like "gpt-4o".
func stripCopilotModelPrefix(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		return body
	}
	clean := strings.TrimPrefix(model, "copilot/")
	if clean == model {
		return body
	}
	out, err := sjson.SetBytes(body, "model", clean)
	if err != nil {
		log.Printf("WARN: failed to rewrite copilot model id: %v", err)
		return body
	}
	return out
}

// sanitizeCopilotBody applies Copilot-specific request cleanups:
// - drop reasoning fields from assistant messages
// - inject response_format instructions for Claude models (Copilot rejects the param)
// - strip temperature for gpt-5.4 models and claude-opus-4 models
// - drop thinking/reasoning_effort for Copilot Claude models (except opus/sonnet 4.6)
// - cap tools to 128
// - serialize unknown content part types as text and drop empty parts
// - drop trailing assistant prefill(s)
func sanitizeCopilotBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	model, _ := req["model"].(string)

	if msgs, ok := req["messages"].([]any); ok {
		for i, raw := range msgs {
			msg, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			if strings.EqualFold(role, "assistant") {
				delete(msg, "reasoning_text")
				delete(msg, "reasoning_content")
			}
			if content, ok := msg["content"].([]any); ok {
				msg["content"] = sanitizeCopilotContentParts(content)
			}
			msgs[i] = msg
		}
		req["messages"] = dropTrailingAssistantPrefill(msgs)
	}

	if isGpt54Model(model) {
		delete(req, "temperature")
	}
	if isClaudeOpus4(model) {
		delete(req, "temperature")
	}
	if isCopilotClaudeNoReasoning(model) {
		delete(req, "thinking")
		delete(req, "reasoning_effort")
	}

	injectCopilotResponseFormat(req, model)

	if tools, ok := req["tools"].([]any); ok && len(tools) > 128 {
		req["tools"] = tools[:128]
	}

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

func isGpt54Model(model string) bool {
	m := strings.ToLower(ExtractModel(model))
	return strings.Contains(m, "gpt-5.4")
}

func isClaudeOpus4(model string) bool {
	m := strings.ToLower(ExtractModel(model))
	return strings.Contains(m, "claude-opus-4")
}

// isCopilotClaudeNoReasoning matches Copilot-hosted Claude models that reject
// thinking/reasoning_effort, matching OmniRoute's stripUnsupportedParams rule:
// claude variants EXCEPT opus/sonnet 4.6.
func isCopilotClaudeNoReasoning(model string) bool {
	m := strings.ToLower(ExtractModel(model))
	if !strings.Contains(m, "claude") {
		return false
	}
	if strings.Contains(m, "4.6") && (strings.Contains(m, "opus") || strings.Contains(m, "sonnet")) {
		return false
	}
	return true
}

func injectCopilotResponseFormat(req map[string]any, model string) {
	if !strings.Contains(strings.ToLower(model), "claude") {
		return
	}
	raw, ok := req["response_format"]
	if !ok || raw == nil {
		return
	}
	format, ok := raw.(map[string]any)
	if !ok {
		delete(req, "response_format")
		return
	}
	typ, _ := format["type"].(string)
	var instruction string
	switch typ {
	case "json_object":
		instruction = "Respond only with valid JSON. Do not include any text before or after the JSON object."
	case "json_schema":
		var schema string
		if js, ok := format["json_schema"].(map[string]any); ok {
			if s, ok := js["schema"]; ok {
				b, _ := json.Marshal(s)
				schema = string(b)
			}
		}
		if schema != "" {
			instruction = fmt.Sprintf("Respond only with valid JSON matching this schema:\n%s\nDo not include any text before or after the JSON.", schema)
		} else {
			instruction = "Respond only with valid JSON. Do not include any text before or after the JSON object."
		}
	default:
		delete(req, "response_format")
		return
	}

	msgs, ok := req["messages"].([]any)
	if !ok {
		delete(req, "response_format")
		return
	}

	systemIdx := -1
	for i, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if strings.EqualFold(role, "system") {
			systemIdx = i
			break
		}
	}

	if systemIdx >= 0 {
		msg := msgs[systemIdx].(map[string]any)
		appendTextToMessage(msg, instruction)
		msgs[systemIdx] = msg
	} else {
		msgs = append([]any{map[string]any{"role": "system", "content": instruction}}, msgs...)
	}
	req["messages"] = msgs
	delete(req, "response_format")
}

func appendTextToMessage(msg map[string]any, text string) {
	switch content := msg["content"].(type) {
	case string:
		msg["content"] = content + "\n\n" + text
	case []any:
		msg["content"] = append(content, map[string]any{"type": "text", "text": text})
	default:
		msg["content"] = text
	}
}

func sanitizeCopilotContentParts(parts []any) any {
	var clean []any
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "text", "image_url":
			clean = append(clean, part)
		default:
			var text string
			switch v := part["text"].(type) {
			case string:
				text = v
			case nil:
			default:
				if v != nil {
					b, _ := json.Marshal(v)
					text = string(b)
				}
			}
			if text == "" {
				if v, ok := part["thinking"].(string); ok {
					text = v
				}
			}
			if text == "" {
				b, _ := json.Marshal(part)
				text = string(b)
			}
			if text != "" {
				clean = append(clean, map[string]any{"type": "text", "text": text})
			}
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// dropTrailingAssistantPrefill removes trailing assistant messages so the
// conversation ends with a non-assistant message, which Copilot's
// /chat/completions endpoint requires. It never empties the array.
func dropTrailingAssistantPrefill(messages []any) []any {
	end := len(messages)
	for end > 1 {
		msg, ok := messages[end-1].(map[string]any)
		if !ok {
			break
		}
		role, _ := msg["role"].(string)
		if !strings.EqualFold(role, "assistant") {
			break
		}
		end--
	}
	return messages[:end]
}
