package executor

import (
	"bytes"
	"context"
	"encoding/json"
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

// Execute sanitizes the request using the provider's compatibility config and
// delegates to the underlying OpenAI executor.
func (e *CloudflareExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	c := providercfg.CompatibilityFor(provider)
	cp.Provider = provider
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
// OpenAI-compatible `delta.reasoning_content` field.
func (e *CloudflareExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	cp := cloneRequest(req)
	provider := req.Provider
	if provider == "" {
		provider = "cf"
	}
	c := providercfg.CompatibilityFor(provider)
	cp.Provider = provider
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
