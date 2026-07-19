package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DevinCLIExecutor routes chat requests through the local Devin CLI
// using the ACP JSON-RPC protocol.
type DevinCLIExecutor struct {
	*BaseExecutor
}

// NewDevinCLIExecutor creates a new Devin CLI executor.
func NewDevinCLIExecutor(base *BaseExecutor) *DevinCLIExecutor {
	return &DevinCLIExecutor{BaseExecutor: base}
}

// Execute runs a non-streaming completion by collecting streamed output.
func (e *DevinCLIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	result, err := e.ExecuteStream(ctx, req)
	if err != nil {
		return nil, err
	}

	id := "chatcmpl-devin-" + uuid.New().String()
	created := time.Now().Unix()
	var content strings.Builder

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		text := extractDeltaText(chunk.Payload)
		if text != "" {
			content.WriteString(text)
		}
		if metaID, metaCreated := extractChunkMeta(chunk.Payload); metaID != "" {
			id = metaID
			created = metaCreated
		}
	}

	model := ExtractModel(req.Model)
	body := &openAICompletion{ID: id, Object: "chat.completion", Created: created, Model: model}
	body.Choices = []openAIChoice{{Index: 0, Message: openAIMessage{Role: "assistant", Content: content.String()}, FinishReason: "stop"}}
	body.Usage = openAIUsage{Estimated: true}

	b, _ := json.Marshal(body)
	return &Response{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: b}, nil
}

// ExecuteStream runs the Devin CLI and streams ACP updates as OpenAI chunks.
func (e *DevinCLIExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	model := ExtractModel(req.Model)
	promptText, _ := FlatPromptFromMessages(req.Body)

	bin, err := resolveDevinBin()
	if err != nil {
		return nil, fmt.Errorf("devin binary not found: %w", err)
	}

	env := []string{}
	if req.APIKey != "" {
		env = append(env, "WINDSURF_API_KEY="+req.APIKey)
	}

	inv := CLIInvocation{
		Command:          []string{bin, "acp", "--agent-type", "summarizer"},
		Env:              env,
		Stdin:            buildACPInput(model, promptText),
		Timeout:          120 * time.Second,
		GracefulShutdown: 2 * time.Second,
	}

	output := RunCLI(ctx, inv)
	if output.Err != nil && output.ExitCode != 0 {
		return nil, fmt.Errorf("devin cli failed (exit %d): %v: %s", output.ExitCode, output.Err, strings.TrimSpace(output.Stderr))
	}

	chunks := make(chan StreamChunk, 64)
	result := &StreamResult{Chunks: chunks, StatusCode: 200, Headers: http.Header{"Content-Type": []string{"text/event-stream"}}}

	go func() {
		defer close(chunks)
		e := devinEmitter{model: model, responseID: "chatcmpl-devin-" + uuid.New().String(), created: time.Now().Unix(), chunks: chunks}
		e.emitFromStdout(output.Stdout)
	}()

	return result, nil
}

func resolveDevinBin() (string, error) {
	home, _ := os.UserHomeDir()

	var known []string
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		known = append(known, filepath.Join(localAppData, "devin", "cli", "bin", "devin.exe"))
	} else {
		known = append(known, filepath.Join(home, ".local", "share", "devin", "bin", "devin"))
		known = append(known, filepath.Join(home, ".devin", "bin", "devin"))
	}

	bin, err := ResolveBin("devin", "CLI_DEVIN_BIN", known)
	if err != nil {
		return "", err
	}
	return bin, nil
}

// --- helpers -----------------------------------------------------------------

type openAICompletion struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int         `json:"index"`
	Delta        openAIMessage `json:"delta,omitempty"`
	Message      openAIMessage `json:"message,omitempty"`
	FinishReason string      `json:"finish_reason"`
}

type openAIMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Estimated        bool `json:"estimated,omitempty"`
}

type acpMsg struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
	Result  map[string]any `json:"result"`
	Error   map[string]any `json:"error"`
}

type devinEmitter struct {
	model      string
	responseID string
	created    int64
	chunks     chan<- StreamChunk
	roleSent   bool
	finished   bool
}

func (e *devinEmitter) emitEvent(payload any) {
	b, _ := json.Marshal(payload)
	e.chunks <- StreamChunk{Payload: []byte("data: " + string(b) + "\n\n")}
}

func (e *devinEmitter) emitDelta(text string) {
	if !e.roleSent {
		e.roleSent = true
		e.emitEvent(map[string]any{
			"id": e.responseID, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}},
		})
	}
	e.emitEvent(map[string]any{
		"id": e.responseID, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
	})
}

func (e *devinEmitter) finish() {
	if e.finished {
		return
	}
	e.finished = true
	e.emitEvent(map[string]any{
		"id": e.responseID, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	})
	e.chunks <- StreamChunk{Payload: []byte("data: [DONE]\n\n")}
}

func (e *devinEmitter) emitError(message string) {
	e.chunks <- StreamChunk{Err: fmt.Errorf("devin acp: %s", message)}
}

func (e *devinEmitter) emitFromStdout(stdout string) {
	defer e.finish()

	var sessionID string
	state := "init"
	lines := strings.Split(stdout, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg acpMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Error != nil {
			e.emitError(fmt.Sprintf("%v", msg.Error))
			return
		}

		switch state {
		case "init":
			if msg.ID == 1 && msg.Result != nil {
				state = "session"
			}
		case "session":
			if msg.ID == 2 && msg.Result != nil {
				sid, _ := msg.Result["sessionId"].(string)
				if sid == "" {
					e.emitError("session/new returned no sessionId")
					return
				}
				sessionID = sid
				state = "prompt"
			}
		case "prompt":
			if msg.Method == "session/update" || msg.Method == "$/update" {
				typ, _ := msg.Params["type"].(string)
				switch typ {
				case "message_delta", "text_delta", "content_delta":
					text := coalesceStrings(msg.Params["content"], msg.Params["delta"], msg.Params["text"])
					if text != "" {
						e.emitDelta(text)
					}
				case "message_stop", "stop", "done":
					return
				case "error":
					e.emitError(coalesceStrings(msg.Params["message"], msg.Params["error"]))
					return
				}
			}
			if msg.ID == 3 && msg.Result != nil && !e.roleSent {
				text := extractResultText(msg.Result)
				if text != "" {
					e.emitDelta(text)
				}
			}
		}
	}

	_ = sessionID
}

func extractResultText(result map[string]any) string {
	if s, ok := result["content"].(string); ok {
		return s
	}
	if s, ok := result["text"].(string); ok {
		return s
	}
	if msg, ok := result["message"].(map[string]any); ok {
		if s, ok := msg["content"].(string); ok {
			return s
		}
	}
	if msgs, ok := result["messages"].([]any); ok {
		var parts []string
		for _, m := range msgs {
			mm, ok := m.(map[string]any)
			if !ok || mm["role"] != "assistant" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%v", mm["content"]))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func coalesceStrings(values ...any) string {
	for _, v := range values {
		switch s := v.(type) {
		case string:
			return s
		case nil:
			continue
		default:
			return fmt.Sprintf("%v", s)
		}
	}
	return ""
}

func extractDeltaText(payload []byte) string {
	if !strings.HasPrefix(string(payload), "data: ") {
		return ""
	}
	data := strings.TrimPrefix(string(payload), "data: ")
	data = strings.TrimSuffix(data, "\n\n")
	if data == "[DONE]" {
		return ""
	}
	var chunk openAICompletion
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return ""
	}
	if len(chunk.Choices) == 0 {
		return ""
	}
	return chunk.Choices[0].Delta.Content
}

func extractChunkMeta(payload []byte) (string, int64) {
	if !strings.HasPrefix(string(payload), "data: ") {
		return "", 0
	}
	data := strings.TrimPrefix(string(payload), "data: ")
	data = strings.TrimSuffix(data, "\n\n")
	var chunk openAICompletion
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return "", 0
	}
	return chunk.ID, chunk.Created
}

func buildACPInput(model, prompt string) []byte {
	init := map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "initialize",
		"params": map[string]any{
			"protocolVersion": "0.3",
			"clientInfo":      map[string]any{"name": "axonrouter", "version": "1.0"},
			"capabilities":    map[string]any{},
		},
	}
	newSession := map[string]any{
		"jsonrpc": "2.0", "id": 2,
		"method": "session/new",
		"params": map[string]any{"cwd": ".", "model": model},
	}
	promptReq := map[string]any{
		"jsonrpc": "2.0", "id": 3,
		"method": "session/prompt",
		"params": map[string]any{"sessionId": "__SESSION_ID__", "content": []map[string]any{{"type": "text", "text": prompt}}},
	}
	b1, _ := json.Marshal(init)
	b2, _ := json.Marshal(newSession)
	b3, _ := json.Marshal(promptReq)
	return append(append(append(b1, '\n'), append(b2, '\n')...), append(b3, '\n')...)
}
