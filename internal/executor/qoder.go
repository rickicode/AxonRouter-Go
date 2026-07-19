package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/sjson"
)

// QoderExecutor routes requests to Qoder.
// PAT tokens (pt-*) use the local qodercli subprocess.
// Other tokens are forwarded to the DashScope OpenAI-compatible endpoint.
type QoderExecutor struct {
	*BaseExecutor
	openai *OpenAIExecutor
}

// NewQoderExecutor creates a new Qoder executor.
func NewQoderExecutor(base *BaseExecutor, openai *OpenAIExecutor) *QoderExecutor {
	return &QoderExecutor{BaseExecutor: base, openai: openai}
}

// Execute handles non-streaming Qoder requests.
func (e *QoderExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	token := effectiveQoderToken(req)
	if isQoderPAT(token) {
		return e.executeViaQoderCli(ctx, req, token, false)
	}
	return e.openai.Execute(ctx, e.transformHTTPRequest(req))
}

// ExecuteStream handles streaming Qoder requests.
func (e *QoderExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	token := effectiveQoderToken(req)
	if isQoderPAT(token) {
		return e.executeViaQoderCliStream(ctx, req, token)
	}
	return e.openai.ExecuteStream(ctx, e.transformHTTPRequest(req))
}

func effectiveQoderToken(req *Request) string {
	for _, v := range []string{req.APIKey, req.AccessToken} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("QODER_PERSONAL_ACCESS_TOKEN"))
}

func isQoderPAT(token string) bool {
	return strings.HasPrefix(token, "pt-")
}

func (e *QoderExecutor) executeViaQoderCli(ctx context.Context, req *Request, token string, stream bool) (*Response, error) {
	result, err := e.runQoderCli(ctx, req, token)
	if err != nil {
		return nil, err
	}

	model := ExtractModel(req.Model)
	id := "chatcmpl-qoder-" + uuid.New().String()

	body := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":   0,
			"message": map[string]any{"role": "assistant", "content": result},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"estimated": true},
	}

	b, _ := json.Marshal(body)
	return &Response{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: b}, nil
}

func (e *QoderExecutor) executeViaQoderCliStream(ctx context.Context, req *Request, token string) (*StreamResult, error) {
	result, err := e.runQoderCli(ctx, req, token)
	if err != nil {
		return nil, err
	}

	chunks := make(chan StreamChunk, 64)
	res := &StreamResult{Chunks: chunks, StatusCode: 200, Headers: http.Header{"Content-Type": []string{"text/event-stream"}}}

	go func() {
		defer close(chunks)
		model := ExtractModel(req.Model)
		responseID := "chatcmpl-qoder-" + uuid.New().String()
		created := time.Now().Unix()

		chunks <- StreamChunk{Payload: []byte(sseJSON(map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}},
		}))}

		for _, word := range splitWords(result) {
			chunks <- StreamChunk{Payload: []byte(sseJSON(map[string]any{
				"id":      responseID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": word + " "}, "finish_reason": nil}},
			}))}
		}

		chunks <- StreamChunk{Payload: []byte(sseJSON(map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		}))}
		chunks <- StreamChunk{Payload: []byte("data: [DONE]\n\n")}
	}()

	return res, nil
}

func (e *QoderExecutor) runQoderCli(ctx context.Context, req *Request, token string) (string, error) {
	model := ExtractModel(req.Model)
	level := mapQoderModelToLevel(model)
	prompt, _ := FlatPromptFromMessages(req.Body)

	bin, err := ResolveBin("qodercli", "CLI_QODER_BIN", nil)
	if err != nil {
		return "", fmt.Errorf("qodercli binary not found: %w", err)
	}

	configDir := qoderConfigDir()
	_ = os.MkdirAll(configDir, 0o755)

	inv := CLIInvocation{
		Command: []string{bin, "--print", "--output-format", "json", "--model", level, "--tools", "", "--config-dir", configDir},
		Env:     []string{"QODER_PERSONAL_ACCESS_TOKEN=" + token},
		Stdin:   []byte(prompt),
		Timeout: 45 * time.Second,
		WorkDir: configDir,
	}

	output := RunCLI(ctx, inv)
	if output.TimedOut {
		return "", fmt.Errorf("qodercli timed out")
	}
	if output.Err != nil {
		return "", fmt.Errorf("qodercli failed (exit %d): %v: %s", output.ExitCode, output.Err, strings.TrimSpace(output.Stderr))
	}

	return parseQoderCliResult(output.Stdout)
}

func parseQoderCliResult(stdout string) (string, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return "", fmt.Errorf("qodercli produced no output")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		lines := strings.Split(trimmed, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(lines[i])
			if !strings.HasPrefix(candidate, "{") {
				continue
			}
			if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
				break
			}
		}
	}

	if parsed == nil {
		return "", fmt.Errorf("qodercli output is not valid JSON: %s", trimmed[:min(len(trimmed), 200)])
	}

	result := ""
	if r, ok := parsed["result"].(string); ok {
		result = r
	}

	isError := false
	if v, ok := parsed["is_error"].(bool); ok {
		isError = v
	}
	if !isError {
		if subtype, ok := parsed["subtype"].(string); ok && strings.EqualFold(subtype, "error") {
			isError = true
		}
	}
	if isError {
		return "", fmt.Errorf("qodercli error: %s", result)
	}
	return result, nil
}

func mapQoderModelToLevel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return "auto"
	}
	if strings.Contains(m, "deepseek-r1") {
		return "ultimate"
	}
	if strings.Contains(m, "glm") {
		return "gm51model"
	}
	if strings.Contains(m, "minimax") {
		return "mmodel"
	}
	if strings.Contains(m, "qwen3-max") {
		return "performance"
	}
	if strings.Contains(m, "kimi-k2") {
		return "kmodel"
	}
	if strings.Contains(m, "qwen3-coder") || strings.Contains(m, "qoder-rome") {
		return "qmodel"
	}
	return "auto"
}

func qoderConfigDir() string {
	if v := strings.TrimSpace(os.Getenv("QODER_CLI_CONFIG_DIR")); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".axonrouter", "qoder-cli")
}

func (e *QoderExecutor) transformHTTPRequest(req *Request) *Request {
	transformed := *req
	model := ExtractModel(req.Model)
	body := req.Body
	switch model {
	case "qwen3.5-plus", "qwen3.6-plus":
		body, _ = sjson.SetBytes(body, "model", "coder-model")
	case "vision-model":
		body, _ = sjson.SetBytes(body, "model", "qwen3-vl-plus")
	}
	transformed.Body = body
	return &transformed
}

func splitWords(text string) []string {
	words := strings.Fields(text)
	var result []string
	var current strings.Builder
	for i, word := range words {
		current.WriteString(word)
		if (i+1)%5 == 0 || i == len(words)-1 {
			result = append(result, current.String())
			current.Reset()
		} else {
			current.WriteByte(' ')
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// expose model override so tests can inspect it.
func qoderHTTPModel(model string) string {
	switch ExtractModel(model) {
	case "qwen3.5-plus", "qwen3.6-plus":
		return "coder-model"
	case "vision-model":
		return "qwen3-vl-plus"
	default:
		return ExtractModel(model)
	}
}