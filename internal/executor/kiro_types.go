package executor

import (
	"bytes"
	"encoding/json"
	"sync"
)

// ---------- OpenAI-shaped response types ----------

type kiroOpenAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type kiroOpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function kiroOpenAIFunction `json:"function"`
}

type kiroOpenAIToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function kiroOpenAIFunction `json:"function,omitempty"`
}

type kiroOpenAIDelta struct {
	Role             string                    `json:"role,omitempty"`
	Content          string                    `json:"content,omitempty"`
	ReasoningContent string                    `json:"reasoning_content,omitempty"`
	ToolCalls        []kiroOpenAIToolCallDelta `json:"tool_calls,omitempty"`
}

type kiroOpenAIStreamChoice struct {
	Index        int             `json:"index"`
	Delta        kiroOpenAIDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type kiroOpenAIUsage struct {
	PromptTokens             int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens         int64 `json:"completion_tokens,omitempty"`
	TotalTokens              int64 `json:"total_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
}

type kiroOpenAIStreamChunk struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []kiroOpenAIStreamChoice `json:"choices"`
	Usage   *kiroOpenAIUsage         `json:"usage,omitempty"`
}

type kiroOpenAIChatMessage struct {
	Role             string               `json:"role"`
	Content          string               `json:"content,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	ToolCalls        []kiroOpenAIToolCall `json:"tool_calls,omitempty"`
}

type kiroOpenAICompletionChoice struct {
	Index        int                   `json:"index"`
	Message      kiroOpenAIChatMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type kiroOpenAICompletion struct {
	ID      string                       `json:"id"`
	Object  string                       `json:"object"`
	Created int64                        `json:"created"`
	Model   string                       `json:"model"`
	Choices []kiroOpenAICompletionChoice `json:"choices"`
	Usage   kiroOpenAIUsage              `json:"usage,omitempty"`
}

// ---------- Kiro upstream event types ----------

type kiroAssistantResponseEvent struct {
	Content string `json:"content"`
}

type kiroReasoningText struct {
	Text string `json:"text"`
}

type kiroReasoningContentEvent struct {
	ReasoningText kiroReasoningText `json:"reasoningText"`
}

type kiroToolUseEvent struct {
	ToolUseId string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type kiroUsageEvent struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

type kiroMetricsEvent struct {
	InputTokens                  int64 `json:"inputTokens"`
	OutputTokens                 int64 `json:"outputTokens"`
	CacheReadInputTokens         int64 `json:"cacheReadInputTokens"`
	CacheReadInputTokensSnake    int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens     int64 `json:"cacheCreationInputTokens"`
	CacheCreationInputTokensSnake int64 `json:"cache_creation_input_tokens"`
}

type kiroMeteringEvent struct {
	MetricsEvent kiroMetricsEvent `json:"metricsEvent"`
}

type kiroContextUsageEvent struct {
	ContextUsagePercentage float64 `json:"contextUsagePercentage"`
}

type kiroUpstreamEnvelope struct {
	ConversationState            json.RawMessage `json:"conversationState,omitempty"`
	ProfileArn                   string          `json:"profileArn,omitempty"`
	InferenceConfig              json.RawMessage `json:"inferenceConfig,omitempty"`
	AdditionalModelRequestFields json.RawMessage `json:"additionalModelRequestFields,omitempty"`
	AgentMode                    json.RawMessage `json:"agentMode,omitempty"`
	SystemPrompt                 string          `json:"systemPrompt,omitempty"`
}

// pool for reused bytes.Buffer used to encode SSE chunks. The returned byte
// slice is copied out before the buffer is returned, so it is safe to put back.
var kiroChunkBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// encodeSSEChunk marshals a typed OpenAI stream chunk into an SSE line:
// "data: <json>\n\n". It uses a pooled bytes.Buffer to avoid per-chunk allocator
// overhead from json.Marshal on a map[string]any.
func encodeSSEChunk(chunk any) []byte {
	buf := kiroChunkBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString("data: ")
	enc := json.NewEncoder(buf)
	_ = enc.Encode(chunk)
	// json.Encoder writes a trailing newline; the SSE format needs a blank line
	// after the data line, so append one more newline.
	buf.WriteByte('\n')
	out := append([]byte(nil), buf.Bytes()...)
	kiroChunkBufferPool.Put(buf)
	return out
}
