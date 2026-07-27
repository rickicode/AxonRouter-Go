package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CloudflareExecutor wraps OpenAIExecutor with Cloudflare Workers AI-specific
// request sanitization and timeout defaults.
type CloudflareExecutor struct {
	*OpenAIExecutor
}

// NewCloudflareExecutor creates a dedicated Cloudflare executor.
func NewCloudflareExecutor(base *OpenAIExecutor) *CloudflareExecutor {
	return &CloudflareExecutor{OpenAIExecutor: base}
}

// cloneRequest returns a shallow copy of req with mutable fields snapped.
func cloneRequest(req *Request) *Request {
	cp := *req
	return &cp
}

// isCloudflareVisionModel reports whether a model is a CF vision/image-to-text
// model that must be routed through the native /ai/run/{model} endpoint with
// Cloudflare's own messages schema.
func isCloudflareVisionModel(model string) bool {
	m := normalizeCloudflareModelName(model)
	switch {
	case strings.HasPrefix(m, "@cf/meta/llama-3.2-11b-vision-instruct"):
		return true
	case strings.HasPrefix(m, "@cf/llava-hf/llava-1.5-7b-hf"):
		return true
	}
	return false
}

// normalizeCloudflareModelName ensures the full @cf/ prefix is present.
// It accepts gateway IDs like "cf/meta/llama-3.2-11b-vision-instruct",
// "meta/llama-3.2-11b-vision-instruct", or already-prefixed "@cf/...".
func normalizeCloudflareModelName(model string) string {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, "@cf/") {
		return model
	}
	model = strings.TrimPrefix(model, "cf/")
	return "@cf/" + model
}

// Execute sanitizes the request using the provider's compatibility config and
// delegates to the underlying OpenAI executor. Vision models are routed to CF's
// native /ai/run endpoint.
func (e *CloudflareExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	cp.Provider = provider

	if model := gjson.GetBytes(cp.Body, "model").String(); isCloudflareVisionModel(model) {
		return e.executeVision(ctx, cp, false)
	}

	c := providercfg.CompatibilityFor(provider)
	cp.Body = sanitizeRequestWithCompatibility(cp.Body, c)
	cp.Body = cfInjectReasoningControl(cp.Body)
	resp, err := e.OpenAIExecutor.Execute(ctx, cp)
	translateIfCloudflare(err)
	if resp != nil {
		normalizeCloudflareResponse(resp)
	}
	return resp, err
}

// ExecuteStream sanitizes the request using the provider's compatibility config
// and delegates to the underlying OpenAI executor, normalizing the response so
// that Cloudflare's non-standard `delta.reasoning` field is rewritten to the
// OpenAI-compatible `delta.reasoning_content` field. Vision models currently
// fall back to non-streaming native execution and are delivered as a single
// synthetic SSE chunk.
func (e *CloudflareExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	cp.Provider = provider

	if model := gjson.GetBytes(cp.Body, "model").String(); isCloudflareVisionModel(model) {
		resp, err := e.executeVision(ctx, cp, true)
		if err != nil {
			return nil, err
		}
		chunks := make(chan StreamChunk, 1)
		chunks <- StreamChunk{Payload: []byte("data: " + string(resp.Body) + "\n\ndata: [DONE]\n\n")}
		close(chunks)
		return &StreamResult{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Chunks:     chunks,
		}, nil
	}

	c := providercfg.CompatibilityFor(provider)
	cp.Body = sanitizeRequestWithCompatibility(cp.Body, c)
	cp.Body = cfInjectReasoningControl(cp.Body)
	result, err := e.OpenAIExecutor.ExecuteStream(ctx, cp)
	translateIfCloudflare(err)
	if err != nil {
		return nil, err
	}
	return &StreamResult{
		StatusCode: result.StatusCode,
		Headers:    result.Headers,
		Chunks:     normalizeCloudflareStream(result.Chunks),
	}, nil
}

// executeVision sends the request to Cloudflare's native /ai/run/{model}
// endpoint, translating the OpenAI chat shape into CF's native schema and
// unwrapping the result envelope.
func (e *CloudflareExecutor) executeVision(ctx context.Context, req *Request, stream bool) (*Response, error) {
	rawModel := gjson.GetBytes(req.Body, "model").String()
	model := normalizeCloudflareModelName(rawModel)

	baseURL := strings.TrimRight(req.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.cloudflare.com/client/v4"
	}
	// If the configured base_url points at the OpenAI-compatible /ai/v1 path,
	// convert it back to the native /ai/run base.
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	baseURL = strings.TrimSuffix(baseURL, "/ai/v1")

	accountID, err := cfAccountID(req)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/ai/run/%s", baseURL, accountID, url.PathEscape(model))

	body, err := translateImageContent(req.Body)
	if err != nil {
		return nil, err
	}
	body = JSONSet(body, "stream", stream)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

	resp, err := e.DoRequest(ctx, "POST", endpoint, headers, body)
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
		upErr.TranslateErrorBody("cf")
		return nil, upErr
	}

	resp.Body = normalizeCloudflareVisionResponse(resp.Body, model)
	return resp, nil
}

// cfAccountID resolves the Cloudflare account ID from request PSD, then env var.
func cfAccountID(req *Request) (string, error) {
	id := req.ProviderSpecificData["accountId"]
	if id == "" {
		id = httpAccountIDFromEnv()
	}
	if id == "" {
		return "", fmt.Errorf(
			"cloudflare Workers AI requires an Account ID. " +
				"Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var. " +
				"Find it at: https://dash.cloudflare.com (right sidebar)")
	}
	return id, nil
}

var httpAccountIDFromEnv = func() string {
	if v := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); v != "" {
		return v
	}
	return ""
}

// translateImageContent converts OpenAI-compatible message content into the
// format Cloudflare's native /ai/run vision models expect. For any message whose
// content is an array, text blocks become {type:"text", text:"..."} and
// image_url blocks become {type:"image", image:"<base64 payload>"}.
func translateImageContent(body []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return body, nil
	}
	messagesRaw, ok := doc["messages"].([]any)
	if !ok {
		return body, nil
	}

	outMsgs := make([]any, 0, len(messagesRaw))
	for _, raw := range messagesRaw {
		msg, ok := raw.(map[string]any)
		if !ok {
			outMsgs = append(outMsgs, raw)
			continue
		}
		role, _ := msg["role"].(string)
		content, ok := msg["content"].([]any)
		if !ok {
			outMsgs = append(outMsgs, msg)
			continue
		}

		// System messages stay text-only. Vision models typically expect user
		// turns to carry image payloads.
		if role == "system" {
			var textParts []string
			for _, block := range content {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := b["type"].(string); t == "text" {
					if s, ok := b["text"].(string); ok {
						textParts = append(textParts, s)
					}
				}
			}
			newMsg := make(map[string]any, len(msg))
			for k, v := range msg {
				newMsg[k] = v
			}
			newMsg["content"] = strings.Join(textParts, "")
			outMsgs = append(outMsgs, newMsg)
			continue
		}

		newBlocks := make([]any, 0, len(content))
		for _, block := range content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch t, _ := b["type"].(string); t {
			case "text":
				text, _ := b["text"].(string)
				newBlocks = append(newBlocks, map[string]any{"type": "text", "text": text})
			case "image_url":
				imageURL, _ := b["image_url"].(map[string]any)
				urlStr, _ := imageURL["url"].(string)
				data, err := fetchOrDecodeImage(urlStr)
				if err != nil {
					return nil, fmt.Errorf("failed to read image_url %q: %w", truncateURL(urlStr), err)
				}
				newBlocks = append(newBlocks, map[string]any{"type": "image", "image": base64.StdEncoding.EncodeToString(data)})
			default:
				// Forward other blocks unchanged.
				newBlocks = append(newBlocks, b)
			}
		}
		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		newMsg["content"] = newBlocks
		outMsgs = append(outMsgs, newMsg)
	}

	doc["messages"] = outMsgs
	out, err := json.Marshal(doc)
	if err != nil {
		return body, nil
	}
	return out, nil
}

// fetchOrDecodeImage returns the raw image bytes for an image_url string.
// It supports data URLs (base64) and remote HTTP(S) URLs.
func fetchOrDecodeImage(urlStr string) ([]byte, error) {
	if urlStr == "" {
		return nil, fmt.Errorf("empty image_url")
	}
	if strings.HasPrefix(urlStr, "data:") {
		return decodeDataURL(urlStr)
	}
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		resp, err := http.Get(urlStr)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("unsupported image_url scheme")
}

// decodeDataURL parses a data URL like data:image/png;base64,... and returns
// the decoded bytes.
func decodeDataURL(urlStr string) ([]byte, error) {
	comma := strings.Index(urlStr, ",")
	if comma < 0 {
		return nil, fmt.Errorf("invalid data URL")
	}
	meta := urlStr[5:comma]
	payload := urlStr[comma+1:]
	if strings.Contains(meta, "base64") {
		return base64.StdEncoding.DecodeString(payload)
	}
	// Support percent-encoded data URLs.
	unescaped, err := url.QueryUnescape(payload)
	if err != nil {
		return nil, err
	}
	return []byte(unescaped), nil
}

func truncateURL(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// normalizeCloudflareVisionResponse unwraps Cloudflare's {result:{response:...}}
// envelope into an OpenAI chat.completion shape. If the body does not look
// like a vision result envelope, it is returned unchanged.
func normalizeCloudflareVisionResponse(body []byte, model string) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	result := gjson.GetBytes(body, "result")
	if !result.Exists() {
		return body
	}

	content := ""
	switch v := result.Value().(type) {
	case map[string]any:
		if resp, ok := v["response"].(string); ok {
			content = resp
		} else if respMsg, ok := v["response"].(map[string]any); ok {
			content, _ = respMsg["response"].(string)
		}
	case string:
		content = v
	}
	if content == "" {
		return body
	}

	out := map[string]any{
		"id":      "cf-vision-response",
		"object":  "chat.completion",
		"created": 0,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return body
	}
	return b
}
// normalizeCloudflareStream rewrites Cloudflare Workers AI streaming chunks
// into the standard OpenAI shape. It aggregates multiple `reasoning` deltas that
// arrive before the first content delta into a single `reasoning_content`
// delta, mirroring the CodeBuddy normalizer. All other chunks (including
// errors, keep-alive lines, and `data: [DONE]`) pass through unchanged.
func normalizeCloudflareStream(in <-chan StreamChunk) chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		var reasoning strings.Builder
		var meta struct {
			id, model string
			created   int64
			set       bool
		}
		reasoningFlushed := false
		flushReasoning := func() *StreamChunk {
			if reasoning.Len() == 0 {
				return nil
			}
			reasoningFlushed = true
			chunk := map[string]any{
				"id":      meta.id,
				"object":  "chat.completion.chunk",
				"created": meta.created,
				"model":   meta.model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"role": "assistant", "reasoning_content": reasoning.String()},
					"finish_reason": "",
				}},
			}
			reasoning.Reset()
			b, _ := json.Marshal(chunk)
			return &StreamChunk{Payload: append([]byte("data: "), b...)}
		}
		for chunk := range in {
			if chunk.Err != nil || len(chunk.Payload) == 0 {
				out <- chunk
				continue
			}
			line := bytes.TrimSpace(chunk.Payload)
			if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
				out <- chunk
				continue
			}
			data := bytes.TrimSpace(line[5:])
			if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
				out <- chunk
				continue
			}
			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				out <- chunk
				continue
			}
			if !meta.set {
				if v, ok := parsed["id"].(string); ok {
					meta.id = v
				}
				if v, ok := parsed["model"].(string); ok {
					meta.model = v
				}
				if v, ok := parsed["created"].(float64); ok {
					meta.created = int64(v)
				}
				meta.set = true
			}
			choices, ok := parsed["choices"].([]any)
			if !ok || len(choices) == 0 {
				out <- chunk
				continue
			}
			choice, ok := choices[0].(map[string]any)
			if !ok {
				out <- chunk
				continue
			}
			delta, ok := choice["delta"].(map[string]any)
			if !ok {
				out <- chunk
				continue
			}
			content, hasContent := delta["content"].(string)
			reasoningText, hasReasoning := delta["reasoning"].(string)
			// Empty reasoning fields should be left untouched, matching the
			// upstream contract.
			if hasReasoning && reasoningText == "" {
				if reasoning.Len() > 0 && !reasoningFlushed && hasContent && content != "" {
					if flushed := flushReasoning(); flushed != nil {
						out <- *flushed
					}
				}
				out <- chunk
				continue
			}
			if hasReasoning && reasoningText != "" {
				// Buffer the reasoning text until we see the first real content delta.
				if content == "" && !reasoningFlushed {
					reasoning.WriteString(reasoningText)
					continue
				}
				// Flush any buffered reasoning before emitting the current chunk.
				if reasoning.Len() > 0 && !reasoningFlushed {
					if flushed := flushReasoning(); flushed != nil {
						out <- *flushed
					}
				}
				// Rewrite the upstream field to the OpenAI-compatible one.
				delete(delta, "reasoning")
				delta["reasoning_content"] = reasoningText
			} else if hasContent && content != "" && reasoning.Len() > 0 && !reasoningFlushed {
				// First real content with no reasoning on this chunk: flush buffered
				// reasoning first so clients see thinking before the answer.
				if flushed := flushReasoning(); flushed != nil {
					out <- *flushed
				}
			}
			// We only need to remarshal when reasoning was rewritten.
			if hasReasoning && reasoningText != "" {
				b, err := json.Marshal(parsed)
				if err != nil {
					out <- chunk
					continue
				}
				out <- StreamChunk{Payload: append([]byte("data: "), b...)}
				continue
			}
			out <- chunk
		}
		if reasoning.Len() > 0 && !reasoningFlushed {
			if flushed := flushReasoning(); flushed != nil {
				out <- *flushed
			}
		}
	}()
	return out
}

// normalizeCloudflareResponse rewrites a non-streaming chat completion response
// from Cloudflare Workers AI so that any choices[0].message.reasoning field is
// renamed to the OpenAI-standard choices[0].message.reasoning_content field.
// When reasoning_content already exists, the original value is preserved and
// the non-standard reasoning field is removed to avoid duplication. Status
// codes, headers, and non-JSON bodies are left untouched.
func normalizeCloudflareResponse(resp *Response) {
	if resp == nil || !gjson.ValidBytes(resp.Body) {
		return
	}
	r := gjson.GetBytes(resp.Body, "choices.0.message.reasoning")
	if !r.Exists() {
		return
	}
	body := resp.Body
	if !gjson.GetBytes(resp.Body, "choices.0.message.reasoning_content").Exists() {
		body, _ = sjson.SetBytes(body, "choices.0.message.reasoning_content", r.Value())
	}
	body, _ = sjson.DeleteBytes(body, "choices.0.message.reasoning")
	resp.Body = body
}

func translateIfCloudflare(err error) {
	if err == nil {
		return
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		return
	}
	upErr.TranslateErrorBody("cf")
}
