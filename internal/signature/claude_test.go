package signature

import (
	"encoding/base64"
	"testing"
)

func TestStripInvalidClaudeThinkingBlocks(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hello","signature":"Ev8"},{"type":"text","text":"ok"}]}]}`)
	out := string(StripInvalidClaudeThinkingBlocks(payload))
	if !contains(out, `"text":"ok"`) {
		t.Errorf("valid text part should be kept: %s", out)
	}
	if contains(out, `"thinking"`) {
		t.Errorf("invalid thinking block should be stripped: %s", out)
	}
}

func TestHasClaudeThinkingSignaturePrefix(t *testing.T) {
	if !HasClaudeThinkingSignaturePrefix("claude#EwogICJ0ZXN0IjogdHJ1ZQ") {
		t.Error("expected valid E-prefixed signature with cache prefix")
	}
	if HasClaudeThinkingSignaturePrefix("notasignature") {
		t.Error("expected invalid signature")
	}
}

func TestNormalizeClaudeThinkingSignature(t *testing.T) {
	// Build a valid single-layer E signature: decoded starts with 0x12.
	inner := append([]byte{0x12, 0x01, 0x02}, []byte("payload")...)
	sig := "E" + base64.StdEncoding.EncodeToString(inner)
	normalized, err := NormalizeClaudeThinkingSignature(sig)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if normalized[0] != 'R' {
		t.Errorf("expected R-form, got %q", normalized)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
