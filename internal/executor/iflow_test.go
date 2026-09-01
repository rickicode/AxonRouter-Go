package executor

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestIFlowHeaders(t *testing.T) {
	headers := map[string]string{}
	iflowHeaders(headers, "iflow", map[string]string{"api_key": "test-api-key"})

	if headers["User-Agent"] != "iFlow-Cli" {
		t.Errorf("User-Agent = %q, want iFlow-Cli", headers["User-Agent"])
	}
	if !strings.HasPrefix(headers["session-id"], "session-") {
		t.Errorf("session-id = %q, want prefix session-", headers["session-id"])
	}
	if headers["x-iflow-timestamp"] == "" {
		t.Error("x-iflow-timestamp missing")
	}
	if headers["x-iflow-signature"] == "" {
		t.Error("x-iflow-signature missing")
	}
	if headers["Authorization"] != "Bearer test-api-key" {
		t.Errorf("Authorization = %q, want Bearer test-api-key", headers["Authorization"])
	}

	// Signature must be valid hex HMAC-SHA256 of userAgent:sessionID:timestamp.
	payload := "iFlow-Cli:" + headers["session-id"] + ":" + headers["x-iflow-timestamp"]
	ts, _ := strconv.ParseInt(headers["x-iflow-timestamp"], 10, 64)
	want := createIFlowSignature("iFlow-Cli", headers["session-id"], ts, "test-api-key")
	if headers["x-iflow-signature"] != want {
		t.Errorf("signature = %q, want %q (payload %q)", headers["x-iflow-signature"], want, payload)
	}
	if len(headers["x-iflow-signature"]) != 64 {
		t.Errorf("signature length = %d, want 64 hex chars", len(headers["x-iflow-signature"]))
	}
}

func TestIFlowHeadersNoAPIKey(t *testing.T) {
	headers := map[string]string{}
	iflowHeaders(headers, "iflow", nil)

	if headers["x-iflow-signature"] != "" {
		t.Errorf("signature = %q, want empty when no apiKey", headers["x-iflow-signature"])
	}
	if headers["Authorization"] != "" {
		t.Errorf("Authorization = %q, want empty when no apiKey", headers["Authorization"])
	}
}

func TestIFlowHeadersOtherProviderNoop(t *testing.T) {
	headers := map[string]string{"Existing": "yes"}
	iflowHeaders(headers, "openai", map[string]string{"api_key": "x"})
	if len(headers) != 1 || headers["Existing"] != "yes" {
		t.Errorf("non-iflow provider must be a no-op, got %v", headers)
	}
}

func TestIFlowTransformRequest(t *testing.T) {
	// Streaming iFlow request gets stream_options injected.
	body := []byte(`{"model":"qwen3-coder-plus","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	out := iflowTransformRequest(body, "iflow", true)
	if !gjson.GetBytes(out, "stream_options.include_usage").Bool() {
		t.Error("stream_options.include_usage not injected for iflow streaming request")
	}

	// Already-present stream_options is left untouched.
	out2 := iflowTransformRequest([]byte(`{"stream_options":{"include_usage":false}}`), "iflow", true)
	if gjson.GetBytes(out2, "stream_options.include_usage").Bool() {
		t.Error("existing stream_options should be preserved")
	}

	// Non-iflow providers are not touched.
	body3 := []byte(`{"stream":true}`)
	if string(iflowTransformRequest(body3, "openai", true)) != string(body3) {
		t.Error("non-iflow provider must not be transformed")
	}
}

func TestRandomUUID(t *testing.T) {
	u := randomUUID()
	parts := strings.Split(u, "-")
	if len(parts) != 5 {
		t.Fatalf("uuid %q has %d parts, want 5", u, len(parts))
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Errorf("uuid %q has wrong part lengths", u)
	}
	if parts[2][0] != '4' {
		t.Errorf("uuid %q version nibble = %q, want 4", u, parts[2][0])
	}
	// Variant bits: first hex char of part 3 must be 8/9/a/b.
	switch parts[3][0] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("uuid %q variant = %q, want 8/9/a/b", u, parts[3][0])
	}
}

func TestIFlowTransformRequestParsesJSON(t *testing.T) {
	out := iflowTransformRequest([]byte(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`), "iflow", true)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["stream_options"] == nil {
		t.Error("stream_options missing after transform")
	}
}
