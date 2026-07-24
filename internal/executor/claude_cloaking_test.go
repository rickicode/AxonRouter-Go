package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/tidwall/gjson"
)

func TestApplyCloakingInjectsSystemAndUserID(t *testing.T) {
	ctx := ContextWithUserAgent(context.Background(), "axon-test/1.0")
	cfg := &config.Config{}
	payload := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)

	out, err := applyCloaking(ctx, cfg, payload, "claude-opus-4", "sk-test")
	if err != nil {
		t.Fatalf("applyCloaking error: %v", err)
	}

	billing := gjson.GetBytes(out, "system.0.text").String()
	if !strings.HasPrefix(billing, "x-anthropic-billing-header:") {
		t.Fatalf("expected billing header, got %q", billing)
	}
	if gjson.GetBytes(out, "metadata.user_id").String() == "" {
		t.Fatal("expected fake user_id to be injected")
	}
}

func TestApplyCloakingDisabled(t *testing.T) {
	ctx := ContextWithUserAgent(context.Background(), "axon-test/1.0")
	cfg := &config.Config{DisableClaudeCloakMode: true}
	payload := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)

	out, err := applyCloaking(ctx, cfg, payload, "claude-opus-4", "sk-test")
	if err != nil {
		t.Fatalf("applyCloaking error: %v", err)
	}
	if gjson.GetBytes(out, "system.0.text").Exists() {
		t.Fatal("expected no cloaking when disabled")
	}
}

func TestApplyCloakingObfuscatesSensitiveWords(t *testing.T) {
	ctx := ContextWithUserAgent(context.Background(), "axon-test/1.0")
	cfg := &config.Config{ClaudeCloakSensitiveWords: []string{"secret"}}
	payload := []byte(`{"messages":[{"role":"user","content":"this is a secret message"}]}`)

	out, err := applyCloaking(ctx, cfg, payload, "claude-opus-4", "sk-test")
	if err != nil {
		t.Fatalf("applyCloaking error: %v", err)
	}
	content := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(content, "\u200B") {
		t.Fatalf("expected zero-width space obfuscation, got %q", content)
	}
}

func TestParseEntrypointFromUA(t *testing.T) {
	if got := parseEntrypointFromUA("claude-cli/2.1.63 (external, vscode)"); got != "vscode" {
		t.Fatalf("expected vscode, got %q", got)
	}
	if got := parseEntrypointFromUA("unknown"); got != "cli" {
		t.Fatalf("expected cli fallback, got %q", got)
	}
}

func TestGenerateFakeUserID(t *testing.T) {
	id := generateFakeUserID()
	if !isValidUserID(id) {
		t.Fatalf("invalid fake user id: %s", id)
	}
}
