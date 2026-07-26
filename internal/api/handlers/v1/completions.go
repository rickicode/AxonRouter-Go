package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Completions handles POST /v1/completions, the legacy OpenAI completions
// surface. It converts the request into a chat-completions payload, reuses the
// existing /v1/chat/completions execution path, then translates the response
// back into completions format.
func (h *Handler) Completions(c *gin.Context) {
	body, err := readBody(c)
	if err != nil {
		writeReadBodyError(c, err)
		return
	}

	chatBody := convertCompletionsRequestToChatCompletions(body)
	stream := executor.IsStreamRequest(chatBody)

	rewrite := func(req *http.Request) {
		req.Body = io.NopCloser(bytes.NewReader(chatBody))
		req.URL.Path = "/v1/chat/completions"
		req.ContentLength = int64(len(chatBody))
	}

	if stream {
		// Stream conversion is done by proxying the chat-completions stream
		// through a thin adapter that rewrites each chat chunk into completions
		// format on the fly.
		rewrite(c.Request)
		c.Request = c.Request.WithContext(c.Request.Context())
		streamResponseWriter := &completionsStreamWriter{ResponseWriter: c.Writer}
		c.Writer = streamResponseWriter
		h.ChatCompletions(c)
		return
	}

	// Non-streaming: capture the entire chat-completions response, convert it,
	// and write it out.
	bw := newBufferedResponseWriter(c.Writer)
	c.Writer = bw
	rewrite(c.Request)
	h.ChatCompletions(c)

	c.Writer = bw.ResponseWriter
	if bw.status >= 400 {
		bw.ResponseWriter.WriteHeader(bw.status)
		bw.ResponseWriter.Write(bw.body.Bytes())
		return
	}

	contentType := bw.Header().Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	for k, v := range bw.Header() {
		if k == "Content-Length" {
			continue
		}
		bw.ResponseWriter.Header()[k] = v
	}
	out := convertChatCompletionsResponseToCompletions(bw.body.Bytes())
	bw.ResponseWriter.Header().Set("Content-Type", contentType)
	bw.ResponseWriter.Header().Del("Content-Length")
	bw.ResponseWriter.WriteHeader(bw.status)
	bw.ResponseWriter.Write(out)
}

// bufferedResponseWriter captures the response body and status so the
// completions handler can translate a chat-completions response back into
// completions format.
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	status int
}

func newBufferedResponseWriter(w gin.ResponseWriter) *bufferedResponseWriter {
	return &bufferedResponseWriter{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		status:         http.StatusOK,
	}
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func (w *bufferedResponseWriter) Flush() {}

// completionsStreamWriter adapts a chat-completions SSE stream into a
// completions SSE stream by rewriting each chunk on the fly.
type completionsStreamWriter struct {
	gin.ResponseWriter

	wroteHeader bool
}

func (w *completionsStreamWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *completionsStreamWriter) Write(b []byte) (int, error) {
	line := string(b)
	if !strings.HasPrefix(line, "data: ") {
		return w.ResponseWriter.Write(b)
	}
	payload := strings.TrimPrefix(line, "data: ")
	payload = strings.TrimSpace(payload)
	if payload == "[DONE]" {
		return w.ResponseWriter.Write(b)
	}
	converted := convertChatCompletionsStreamChunkToCompletions([]byte(payload))
	if converted == nil {
		return len(b), nil
	}
	out := []byte("data: " + string(converted) + "\n\n")
	return w.ResponseWriter.Write(out)
}

func (w *completionsStreamWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *completionsStreamWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// convertCompletionsRequestToChatCompletions maps the legacy OpenAI completions
// payload to the chat-completions shape used by AxonRouter's routing layer.
func convertCompletionsRequestToChatCompletions(raw []byte) []byte {
	root := gjson.ParseBytes(raw)
	prompt := root.Get("prompt").String()
	if prompt == "" {
		prompt = "Complete this:"
	}

	out := []byte(`{"model":"","messages":[{"role":"user","content":""}]}`)

	if model := root.Get("model"); model.Exists() {
		out, _ = sjson.SetBytes(out, "model", model.String())
	}
	out, _ = sjson.SetBytes(out, "messages.0.content", prompt)

	out = copyNumber(raw, out, "max_tokens", root)
	out = copyNumber(raw, out, "temperature", root)
	out = copyNumber(raw, out, "top_p", root)
	out = copyNumber(raw, out, "frequency_penalty", root)
	out = copyNumber(raw, out, "presence_penalty", root)
	out = copyBool(raw, out, "stream", root)
	out = copyBool(raw, out, "logprobs", root)
	out = copyInt(raw, out, "top_logprobs", root)
	out = copyBool(raw, out, "echo", root)

	if stop := root.Get("stop"); stop.Exists() {
		out, _ = sjson.SetRawBytes(out, "stop", []byte(stop.Raw))
	}

	return out
}

func copyNumber(raw, out []byte, key string, root gjson.Result) []byte {
	if v := root.Get(key); v.Exists() {
		if v.Type == gjson.Number {
			out, _ = sjson.SetBytes(out, key, v.Float())
		} else if v.Type == gjson.String {
			out, _ = sjson.SetBytes(out, key, v.String())
		} else {
			out, _ = sjson.SetBytes(out, key, v.Value())
		}
	}
	return out
}

func copyInt(raw, out []byte, key string, root gjson.Result) []byte {
	if v := root.Get(key); v.Exists() {
		out, _ = sjson.SetBytes(out, key, v.Int())
	}
	return out
}

func copyBool(raw, out []byte, key string, root gjson.Result) []byte {
	if v := root.Get(key); v.Exists() {
		out, _ = sjson.SetBytes(out, key, v.Bool())
	}
	return out
}

// convertChatCompletionsResponseToCompletions converts a non-streaming
// chat-completions response back into the legacy completions envelope.
func convertChatCompletionsResponseToCompletions(raw []byte) []byte {
	root := gjson.ParseBytes(raw)
	out := []byte(`{"id":"","object":"text_completion","created":0,"model":"","choices":[]}`)

	if id := root.Get("id"); id.Exists() {
		out, _ = sjson.SetBytes(out, "id", id.String())
	}
	if created := root.Get("created"); created.Exists() {
		out, _ = sjson.SetBytes(out, "created", created.Int())
	}
	if model := root.Get("model"); model.Exists() {
		out, _ = sjson.SetBytes(out, "model", model.String())
	}
	if usage := root.Get("usage"); usage.Exists() {
		out, _ = sjson.SetRawBytes(out, "usage", []byte(usage.Raw))
	}

	var choices []map[string]any
	if chatChoices := root.Get("choices"); chatChoices.Exists() && chatChoices.IsArray() {
		chatChoices.ForEach(func(_, choice gjson.Result) bool {
			cc := map[string]any{
				"index": choice.Get("index").Int(),
			}
			if message := choice.Get("message"); message.Exists() {
				if content := message.Get("content"); content.Exists() {
					cc["text"] = content.String()
				}
			}
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				cc["finish_reason"] = finishReason.String()
			}
			if logprobs := choice.Get("logprobs"); logprobs.Exists() {
				cc["logprobs"] = logprobs.Value()
			}
			choices = append(choices, cc)
			return true
		})
	}
	if len(choices) > 0 {
		choicesJSON, _ := json.Marshal(choices)
		out, _ = sjson.SetRawBytes(out, "choices", choicesJSON)
	}
	return out
}

// convertChatCompletionsStreamChunkToCompletions converts a single streaming
// chat-completions chunk to a completions chunk. Returns nil for chunks that
// carry no meaningful text so the stream stays clean.
func convertChatCompletionsStreamChunkToCompletions(raw []byte) []byte {
	root := gjson.ParseBytes(raw)
	if !root.Get("choices").Exists() {
		return raw
	}

	hasContent := false
	out := []byte(`{"id":"","object":"text_completion","created":0,"model":"","choices":[]}`)

	if id := root.Get("id"); id.Exists() {
		out, _ = sjson.SetBytes(out, "id", id.String())
	}
	if created := root.Get("created"); created.Exists() {
		out, _ = sjson.SetBytes(out, "created", created.Int())
	}
	if model := root.Get("model"); model.Exists() {
		out, _ = sjson.SetBytes(out, "model", model.String())
	}
	if usage := root.Get("usage"); usage.Exists() {
		out, _ = sjson.SetRawBytes(out, "usage", []byte(usage.Raw))
	}

	var choices []map[string]any
	if chatChoices := root.Get("choices"); chatChoices.IsArray() {
		chatChoices.ForEach(func(_, choice gjson.Result) bool {
			cc := map[string]any{
				"index": choice.Get("index").Int(),
			}
			if delta := choice.Get("delta"); delta.Exists() {
				if content := delta.Get("content"); content.Exists() {
					text := content.String()
					if text != "" {
						hasContent = true
					}
					cc["text"] = text
				}
			}
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				fr := finishReason.String()
				if fr != "" {
					hasContent = true
				}
				cc["finish_reason"] = fr
			}
			if logprobs := choice.Get("logprobs"); logprobs.Exists() {
				cc["logprobs"] = logprobs.Value()
				hasContent = true
			}
			choices = append(choices, cc)
			return true
		})
	}
	if len(choices) > 0 {
		choicesJSON, _ := json.Marshal(choices)
		out, _ = sjson.SetRawBytes(out, "choices", choicesJSON)
	}
	if !hasContent && len(choices) > 0 {
		return nil
	}
	return out
}
