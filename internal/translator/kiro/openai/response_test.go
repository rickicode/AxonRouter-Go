package openai

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestKiroToOpenAIResponse_SSETextFallback(t *testing.T) {
	input := []byte(`data: {"assistantResponseEvent": {"content": "assistant response"}}`)
	out := convertKiroResponseToOpenAIStream(context.Background(), "test-model", nil, nil, input, nil)
	if len(out) == 0 {
		t.Fatalf("expected output chunks, got none")
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	got := string(out[0])
	if !strings.HasPrefix(got, "data: ") {
		t.Fatalf("expected SSE data prefix, got %q", got)
	}
	if !strings.Contains(got, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected chat.completion.chunk, got %q", got)
	}
	if !strings.Contains(got, `"content":"assistant response"`) {
		t.Fatalf("expected assistant response content, got %q", got)
	}
	if !bytes.HasSuffix(out[0], []byte("\n\n")) {
		t.Fatalf("expected trailing double newline, got %q", got)
	}
}

func TestKiroToOpenAIResponse_BinaryPassthrough(t *testing.T) {
	input := []byte{0x00, 0x01, 0x02, 0x03}
	out := convertKiroResponseToOpenAIStream(context.Background(), "test-model", nil, nil, input, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	want := append(input, []byte("\n\n")...)
	if !bytes.Equal(out[0], want) {
		t.Fatalf("expected %q, got %q", want, out[0])
	}
}

func TestKiroToOpenAIResponse_OpenAIFormatPassthrough(t *testing.T) {
	input := []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}`)
	out := convertKiroResponseToOpenAIStream(context.Background(), "test-model", nil, nil, input, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	want := []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n")
	if !bytes.Equal(out[0], want) {
		t.Fatalf("expected %q, got %q", want, out[0])
	}
}
