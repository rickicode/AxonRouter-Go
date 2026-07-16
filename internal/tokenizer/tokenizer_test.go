package tokenizer

import (
	"testing"
)

func TestCodecForModel(t *testing.T) {
	cases := []struct {
		model   string
		wantErr bool
	}{
		{"gpt-4o", false},
		{"gpt-4", false},
		{"gpt-3.5-turbo", false},
		{"o1-mini", false},
		{"deepseek-chat", false},
		{"", false},
	}

	for _, tc := range cases {
		enc, err := CodecForModel(tc.model)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CodecForModel(%q) expected error", tc.model)
			}
			continue
		}
		if err != nil {
			t.Errorf("CodecForModel(%q) unexpected error: %v", tc.model, err)
			continue
		}
		if enc == nil {
			t.Errorf("CodecForModel(%q) returned nil codec", tc.model)
		}
	}
}

func TestCountOpenAIChatTokens(t *testing.T) {
	payload := []byte(`{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello world"}
  ]
}`)

	enc, err := CodecForModel("gpt-4o")
	if err != nil {
		t.Fatalf("CodecForModel failed: %v", err)
	}

	count, err := CountOpenAIChatTokens(enc, payload)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens failed: %v", err)
	}
	if count <= 0 {
		t.Fatalf("expected positive token count, got %d", count)
	}

	// Longer content should produce a strictly larger count.
	longer := []byte(`{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "Hello world this is a much longer piece of text for token counting"}
  ]
}`)
	longerCount, err := CountOpenAIChatTokens(enc, longer)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens failed: %v", err)
	}
	if longerCount <= count {
		t.Errorf("expected longer content to yield more tokens (%d > %d)", longerCount, count)
	}
}

func TestCountOpenAIChatTokens_EmptyPayload(t *testing.T) {
	enc, err := CodecForModel("gpt-4o")
	if err != nil {
		t.Fatalf("CodecForModel failed: %v", err)
	}
	count, err := CountOpenAIChatTokens(enc, []byte("{}"))
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tokens for empty payload, got %d", count)
	}
}

func TestCountOpenAIChatTokens_NilEncoder(t *testing.T) {
	_, err := CountOpenAIChatTokens(nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err == nil {
		t.Error("expected error for nil encoder")
	}
}
