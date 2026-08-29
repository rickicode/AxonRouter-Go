package normalize

import "testing"

func TestValidateClientSSENormalizesToolCall(t *testing.T) {
	out, ok := ValidateClientSSE([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"id":"bad.id","function":{"name":"read"}}]}}]}`))
	if !ok {
		t.Fatal("expected valid SSE")
	}
	want := `data: {"choices":[{"delta":{"tool_calls":[{"id":"badid","function":{"name":"read"},"type":"function"}]}}]}`
	if string(out[:len(want)]) != want {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestValidateClientSSERejectsMalformedJSON(t *testing.T) {
	if _, ok := ValidateClientSSE([]byte(`data: {broken`)); ok {
		t.Fatal("expected malformed SSE to be rejected")
	}
}

func TestValidateClientJSONNormalizesToolCall(t *testing.T) {
	out, ok := ValidateClientJSON([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"bad.id"}]}}]}`))
	if !ok || string(out) != `{"choices":[{"message":{"tool_calls":[{"id":"badid","type":"function"}]}}]}` {
		t.Fatalf("unexpected output: %s", out)
	}
}
