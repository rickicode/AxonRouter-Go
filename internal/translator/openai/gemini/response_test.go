package gemini

import "testing"

func TestSanitizeGeminiToolID(t *testing.T) {
	if got := sanitizeGeminiToolID("bad.id/1", 2, "msg-1"); got != "badid1" {
		t.Fatalf("got %q, want %q", got, "badid1")
	}
	if got := sanitizeGeminiToolID("", 2, "msg.1"); got != "call_msg1_2" {
		t.Fatalf("got %q, want %q", got, "call_msg1_2")
	}
}

func TestNormalizeGeminiArguments(t *testing.T) {
	if got := normalizeGeminiArguments(`{"city":"Paris"}`); got != `{"city":"Paris"}` {
		t.Fatalf("got %q", got)
	}
	if got := normalizeGeminiArguments(`not-json`); got != `{}` {
		t.Fatalf("got %q, want {}", got)
	}
}
