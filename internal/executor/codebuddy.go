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
// leakage. We strip that field from every SSE chunk before forwarding.
func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	req.Body = codebuddyEnsureSystemMessage(req.Body)
	upstream, err := e.OpenAIExecutor.ExecuteStream(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		for chunk := range upstream.Chunks {
			if chunk.Err == nil && len(chunk.Payload) > 0 {
				chunk.Payload = sanitizeCodeBuddyChunk(chunk.Payload)
			}
			out <- chunk
		}
	}()

	return &StreamResult{
		StatusCode: upstream.StatusCode,
		Headers:    upstream.Headers,
		Chunks:     out,
	}, nil
}

// sanitizeCodeBuddyChunk cleans up a CodeBuddy SSE chunk so it behaves like a
// standard OpenAI-compatible stream. CodeBuddy emits several non-standard
// fields (`extra_fields`, empty arrays, `usage` on every chunk,
// `reasoning_content`) that confuse clients such as OpenCode. We remove the
// noise while preserving real content and the final usage/finish_reason.
func sanitizeCodeBuddyChunk(payload []byte) []byte {
	trimmed := bytes.TrimSpace(payload)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return payload
	}
	data := bytes.TrimSpace(trimmed[5:])
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return payload
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return payload
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
			cleanDelta(delta)

			// If the delta is now empty and this is not the final chunk, drop the
			// delta from the choice to avoid emitting empty content events that some
			// clients render as stuttering.
			if len(delta) == 0 {
				delete(choice, "delta")
			}
		}

		// Drop null/empty non-standard choice-level fields that CodeBuddy includes.
		if lp, ok := choice["logprobs"]; ok && lp == nil {
			delete(choice, "logprobs")
		}
	}

	// CodeBuddy sends usage on every chunk; standard OpenAI only sends it on the
	// final chunk. Strip intermediate usage rows to avoid clients rendering a
	// thinking/progress marker per chunk.
	if !isFinalUsage {
		delete(parsed, "usage")
	}

	b, err := json.Marshal(parsed)
	if err != nil {
		return payload
	}
	return append([]byte("data: "), b...)
}

// cleanDelta removes junk fields from a streaming delta. CodeBuddy often
// includes empty/null `reasoning_content`, empty `refusal`, empty `tool_calls`,
// null `function_call`, and null `extra_fields` on chunks. Keeping them makes
// clients such as OpenCode render empty reasoning/thought placeholders or
// leak thinking text. We strip all `reasoning_content` here; exposing thinking
// cleanly requires a client that understands it or an explicit opt-in format.
func cleanDelta(delta map[string]any) {
	delete(delta, "reasoning_content")

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
			cleanDelta(delta)
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
