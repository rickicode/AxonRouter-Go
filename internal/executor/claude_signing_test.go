package executor

import (
	"regexp"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSignAnthropicMessagesBody(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.63.abc; cc_entrypoint=cli; cch=00000; "}],"messages":[{"role":"user","content":"hi"}]}`)

	signed := signAnthropicMessagesBody(body)
	billing := gjson.GetBytes(signed, "system.0.text").String()
	if !regexp.MustCompile(`cch=[0-9a-f]{5};`).MatchString(billing) {
		t.Fatalf("expected signed cch in billing header, got %q", billing)
	}
	cch := gjson.GetBytes(signed, "system.0.text").String()
	if cch == string(body) {
		t.Fatal("expected body to be modified")
	}
}

func TestSignAnthropicMessagesBodyNoBillingHeader(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if got := signAnthropicMessagesBody(body); !equalBytes(got, body) {
		t.Fatalf("expected body unchanged without billing header, got %s", got)
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
