package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func codebuddyHeaders(headers map[string]string, provider string) {
	if provider != "codebuddy" {
		return
	}
	headers["User-Agent"] = "CLI/2.63.2 CodeBuddy/2.63.2"
	headers["X-Product"] = "SaaS"
	headers["X-IDE-Type"] = "CLI"
	headers["X-IDE-Name"] = "CLI"
	headers["X-Domain"] = "www.codebuddy.ai"
	headers["x-requested-with"] = "XMLHttpRequest"
	headers["x-codebuddy-request"] = "1"
}

// codebuddyEnsureSystemMessage prepends a default system message if the
// request body does not already contain one. CodeBuddy's /v2/chat/completions
// endpoint rejects requests whose messages array starts with a user message
// ("Parse message failed"), so a leading system message is required.
func codebuddyEnsureSystemMessage(body []byte) []byte {
	if !gjson.GetBytes(body, "messages").Exists() {
		return body
	}
	firstRole := gjson.GetBytes(body, "messages.0.role").String()
	if firstRole == "system" {
		return body
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(gjson.GetBytes(body, "messages").Raw), &arr); err != nil {
		return body
	}
	arr = append([]map[string]any{{"role": "system", "content": "You are a helpful assistant."}}, arr...)
	out, err := sjson.SetBytes(body, "messages", arr)
	if err != nil {
		return body
	}
	return out
}

// CodeBuddyExecutor wraps the generic OpenAI-compatible executor with
// CodeBuddy-specific request/response handling.
type CodeBuddyExecutor struct {
	*OpenAIExecutor
}

// NewCodeBuddyExecutor creates a new CodeBuddy executor.
func NewCodeBuddyExecutor(base *BaseExecutor) *CodeBuddyExecutor {
	return &CodeBuddyExecutor{OpenAIExecutor: NewOpenAIExecutor(base)}
}

// Execute performs a non-streaming chat completion. CodeBuddy's upstream only
// supports streaming chat completions, so we always call the streaming endpoint
// and aggregate the SSE chunks back into a single completion response.
func (e *CodeBuddyExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	req.Body = codebuddyEnsureSystemMessage(req.Body)
	result, err := e.ExecuteStream(ctx, req)
	if err != nil {
		return nil, err
	}

	body, err := aggregateCodeBuddyStream(result.Chunks)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: result.StatusCode,
		Body:       body,
		Headers:    result.Headers,
	}, nil
}

// ExecuteStream performs a streaming chat completion. CodeBuddy's upstream can
// include a `reasoning_content` field in the delta; some clients (e.g. OpenCode)
// render it as part of the visible response and show it as choppy "thinking"
// leakage. We aggregate the reasoning text into a single block before any
// content and emit it as one reasoning delta, then continue with standard
// content chunks.
func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	req.Body = codebuddyEnsureSystemMessage(req.Body)
	upstream, err := e.OpenAIExecutor.ExecuteStream(ctx, req)
	if err != nil {
		return nil, err
	}

	return &StreamResult{
		StatusCode: upstream.StatusCode,
		Headers:    upstream.Headers,
		Chunks:     normalizeCodeBuddyStream(upstream.Chunks),
	}, nil
}

// normalizeCodeBuddyStream rewrites a CodeBuddy SSE stream into a standard
// OpenAI-compatible stream. It performs three tasks:
//   1. Aggregates `reasoning_content` deltas that arrive before the first real
//      content delta into a single reasoning delta. This avoids clients
//      rendering a separate "Thought: Xms" placeholder for each tiny reasoning
//      token.
//   2. Removes CodeBuddy-specific noise (`extra_fields`, null `function_call`,
//      empty `refusal`, empty `tool_calls`, null `logprobs`).
//   3. Strips intermediate `usage` rows; only the final usage chunk is kept.
func normalizeCodeBuddyStream(in <-chan StreamChunk) chan StreamChunk {
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
			// Errors and non-data lines pass through untouched.
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

			// Capture top-level metadata from the first real chunk we see.
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

			choices, _ := parsed["choices"].([]any)
			isFinalUsage := len(choices) == 0
			var choice map[string]any
			if len(choices) > 0 {
				choice, _ = choices[0].(map[string]any)
				if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
					isFinalUsage = true
				}
			}

			if choice != nil {
				if delta, ok := choice["delta"].(map[string]any); ok {
					content, _ := delta["content"].(string)
					reasoningText, _ := delta["reasoning_content"].(string)

					// Buffer reasoning text that arrives before the first real content
					// token. Drop the noisy upstream chunk.
					if content == "" && reasoningText != "" && !reasoningFlushed {
						reasoning.WriteString(reasoningText)
						continue
					}

					// As soon as real content starts, flush the aggregated reasoning
					// as a single delta before forwarding the content chunk.
					if reasoning.Len() > 0 && !reasoningFlushed {
						if flushed := flushReasoning(); flushed != nil {
							out <- *flushed
						}
					}

					// Remove reasoning_content from the current chunk so it is not
					// emitted again (and strip any other noise fields).
					delete(delta, "reasoning_content")
					cleanDelta(delta)

					// If the delta is now empty and this is not the final chunk, drop
					// the delta to avoid emitting empty content events that some
					// clients render as stuttering.
					if len(delta) == 0 && !isFinalUsage {
						delete(choice, "delta")
					}
				}

				// Drop null/empty non-standard choice-level fields.
				if lp, ok := choice["logprobs"]; ok && lp == nil {
					delete(choice, "logprobs")
				}
			}

			// CodeBuddy sends usage on every chunk; standard OpenAI only sends it
			// on the final chunk. Strip intermediate usage rows.
			if !isFinalUsage {
				delete(parsed, "usage")
			}

			b, err := json.Marshal(parsed)
			if err != nil {
				out <- chunk
				continue
			}
			out <- StreamChunk{Payload: append([]byte("data: "), b...)}
		}

		// Stream ended with reasoning but no content; flush what we have.
		if reasoning.Len() > 0 && !reasoningFlushed {
			if flushed := flushReasoning(); flushed != nil {
				out <- *flushed
			}
		}
	}()

	return out
}

// cleanDelta removes junk fields from a streaming delta. CodeBuddy often
// includes null `function_call`, empty `refusal`, empty `tool_calls`, and null
// `extra_fields` on chunks. Keeping them makes clients such as OpenCode render
// empty reasoning/thought placeholders or leak thinking text.
func cleanDelta(delta map[string]any) {
	if v, ok := delta["extra_fields"]; ok && v == nil {
		delete(delta, "extra_fields")
	}
	if v, ok := delta["function_call"]; ok && v == nil {
		delete(delta, "function_call")
	}
	if v, ok := delta["refusal"]; ok {
		if s, _ := v.(string); s == "" {
			delete(delta, "refusal")
		}
	}
	if v, ok := delta["tool_calls"]; ok {
		if a, _ := v.([]any); len(a) == 0 {
			delete(delta, "tool_calls")
		}
	}
}

// aggregateCodeBuddyStream reads SSE chunks from the channel and builds a single
// non-streaming chat.completion response. Each chunk payload is an SSE line from
// the base executor (e.g. `data: {"choices":...}` or `data: [DONE]`).
func aggregateCodeBuddyStream(ch <-chan StreamChunk) ([]byte, error) {
	var content strings.Builder
	var reasoning strings.Builder
	var usage map[string]any
	var finishReason string
	var id, model string
	var created int64

	for chunk := range ch {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		line := bytes.TrimSpace(chunk.Payload)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunkJSON map[string]any
		if err := json.Unmarshal(data, &chunkJSON); err != nil {
			continue
		}
		if id == "" {
			if v, ok := chunkJSON["id"].(string); ok {
				id = v
			}
		}
		if model == "" {
			if v, ok := chunkJSON["model"].(string); ok {
				model = v
			}
		}
		if created == 0 {
			if v, ok := chunkJSON["created"].(float64); ok {
				created = int64(v)
			}
		}

		choices, _ := chunkJSON["choices"].([]any)
		if len(choices) == 0 {
			if u, ok := chunkJSON["usage"].(map[string]any); ok {
				usage = u
			}
			continue
		}

		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if delta != nil {
			if v, ok := delta["content"].(string); ok {
				content.WriteString(v)
			}
			if v, ok := delta["reasoning_content"].(string); ok && v != "" {
				reasoning.WriteString(v)
			}
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}
		if u, ok := chunkJSON["usage"].(map[string]any); ok {
			usage = u
		}
	}

	msg := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		msg["reasoning_content"] = reasoning.String()
	}
	out := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finishReason}},
		"usage":   usage,
	}
	return json.Marshal(out)
}
