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

// ExecuteStream performs a streaming chat completion.
func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	req.Body = codebuddyEnsureSystemMessage(req.Body)
	return e.OpenAIExecutor.ExecuteStream(ctx, req)
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
		if v, ok := delta["content"].(string); ok {
			content.WriteString(v)
		}
		if v, ok := delta["reasoning_content"].(string); ok {
			reasoning.WriteString(v)
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
