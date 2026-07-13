package usage

import (
	"testing"
)

func TestFallbackEstimate_EstimateTokensFromString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"empty", "", 0},
		{"short text", "hello world", 2},           // 11 runes / 4 = 2
		{"unicode", "你好世界", 1},                     // 4 runes / 4 = 1
		{"exactly 4", "abcd", 1},                    // 4 / 4 = 1
		{"three chars", "abc", 0},                   // 3 / 4 = 0
		{"longer text", "the quick brown fox jumps", 6}, // 26 / 4 = 6
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokensFromString(tt.input)
			if got != tt.want {
				t.Errorf("EstimateTokensFromString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFallbackEstimate_EstimateTokensFromRequest_OpenAIChat(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello! How are you?"}
		]
	}`)
	// messages: "You are a helpful assistant." = 28 chars, "Hello! How are you?" = 19 chars
	// No top-level "system" field.
	// Total: 47 → 47/4 = 11
	got := EstimateTokensFromRequest(body)
	if got != 11 {
		t.Errorf("EstimateTokensFromRequest(openai chat) = %d, want 11", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromRequest_ClaudeMessages(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-opus",
		"system": "You are Claude, a helpful AI assistant.",
		"messages": [
			{"role": "user", "content": "What is the capital of France?"}
		]
	}`)
	// system field: "You are Claude, a helpful AI assistant." = 39 chars
	// messages[0].content: "What is the capital of France?" = 32 chars
	// Total: 71 → 71/4 = 17
	got := EstimateTokensFromRequest(body)
	if got != 17 {
		t.Errorf("EstimateTokensFromRequest(claude messages) = %d, want 17", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromRequest_EmptyMessages(t *testing.T) {
	body := []byte(`{"model": "gpt-4", "messages": []}`)
	got := EstimateTokensFromRequest(body)
	if got != 0 {
		t.Errorf("EstimateTokensFromRequest(empty) = %d, want 0", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_OpenAIChat(t *testing.T) {
	body := []byte(`{
		"choices": [
			{
				"message": {
					"content": "Paris is the capital of France."
				}
			}
		]
	}`)
	// "Paris is the capital of France." = 31 chars → 31/4 = 7
	got := EstimateTokensFromResponse(body)
	if got != 7 {
		t.Errorf("EstimateTokensFromResponse(openai chat) = %d, want 7", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_OpenAIText(t *testing.T) {
	body := []byte(`{
		"choices": [
			{
				"text": "Once upon a time in a faraway land"
			}
		]
	}`)
	// "Once upon a time in a faraway land" = 34 chars → 34/4 = 8
	got := EstimateTokensFromResponse(body)
	if got != 8 {
		t.Errorf("EstimateTokensFromResponse(openai text) = %d, want 8", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_ClaudeText(t *testing.T) {
	body := []byte(`{
  "content": [
    {"type": "text", "text": "Hello world"}
  ]
}`)
	// Top-level content[0].text path should be recognized.
	textLen := len("Hello world")
	got := EstimateTokensFromResponse(body)
	want := int64(textLen / 4)
	if got != want {
		t.Errorf("EstimateTokensFromResponse(claude) = %d, want %d (text len %d / 4)", got, want, textLen)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_ClaudeMessages(t *testing.T) {
	body := []byte(`{
  "id": "msg_123",
  "type": "message",
  "content": [
    {"type": "text", "text": "The capital of France is Paris."}
  ]
}`)
	// Top-level content[0].text path should be recognized.
	text := "The capital of France is Paris."
	textLen := len(text)
	got := EstimateTokensFromResponse(body)
	want := int64(textLen / 4)
	if got != want {
		t.Errorf("EstimateTokensFromResponse(claude messages) = %d, want %d (text len %d / 4)", got, want, textLen)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_Fallback(t *testing.T) {
	body := []byte(`{"some":"unknown response"}`)
	bodyLen := len(body)
	got := EstimateTokensFromResponse(body)
	want := int64(bodyLen / 4)
	if got != want {
		t.Errorf("EstimateTokensFromResponse(fallback) = %d, want %d (body len %d / 4)", got, want, bodyLen)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_OutputText(t *testing.T) {
	body := []byte(`{"output_text": "Short summary."}`)
	// "Short summary." = 15 chars → 15/4 = 3
	got := EstimateTokensFromResponse(body)
	if got != 3 {
		t.Errorf("EstimateTokensFromResponse(output_text) = %d, want 3", got)
	}
}

func TestFallbackEstimate_EstimateTokensFromResponse_ResponseOutput(t *testing.T) {
	body := []byte(`{
		"response": {
			"output": [
				{
					"content": [
						{"text": "Nested content path"}
					]
				}
			]
		}
	}`)
	// "Nested content path" = 19 chars → 19/4 = 4
	got := EstimateTokensFromResponse(body)
	if got != 4 {
		t.Errorf("EstimateTokensFromResponse(response.output) = %d, want 4", got)
	}
}
