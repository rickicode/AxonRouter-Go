package signature

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestSanitizeClaudeMessagesForClaudeUpstream_DropsGPTThinkingSignature(t *testing.T) {
	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"codex reasoning","signature":"gAAAAABopenai-encrypted-content"},
				{"type":"text","text":"Answer"}
			]},
			{"role":"user","content":[{"type":"text","text":"next"}]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.DroppedBlocks == 0 {
		t.Fatalf("expected at least one dropped block, got report=%+v", report)
	}
	if strings.Contains(string(out), "gAAAAABopenai-encrypted-content") || strings.Contains(string(out), "codex reasoning") {
		t.Fatalf("GPT thinking block should have been removed: %s", string(out))
	}
	content := gjson.GetBytes(out, "messages.0.content").Array()
	if len(content) != 1 {
		t.Fatalf("expected 1 remaining block, got %d: %s", len(content), string(out))
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_DropsEmptyThinkingPlaceholder(t *testing.T) {
	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"assistant","content":[
				{"type":"thinking","text":"","signature":""},
				{"type":"text","text":"Answer"}
			]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.DroppedBlocks == 0 {
		t.Fatalf("expected empty thinking placeholder to be dropped, got report=%+v", report)
	}
	if strings.Contains(string(out), `"type":"thinking"`) {
		t.Fatalf("empty thinking placeholder should have been removed: %s", string(out))
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_NormalizesClaudeSignature(t *testing.T) {
	// Build a minimal valid strict Claude signature:
	// top-level field 2 -> container field 1 -> channelBlock field 1 varint 11.
	channelBlock := protowire.AppendTag(nil, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 11)
	container := protowire.AppendTag(nil, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)
	payload := protowire.AppendTag(nil, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	// Ensure the decoded payload starts with the Claude marker byte 0x12, which
	// AppendTag already wrote for field 2.
	if payload[0] != 0x12 {
		t.Fatalf("payload does not start with Claude marker: 0x%02x", payload[0])
	}
	sig := base64.StdEncoding.EncodeToString(payload)

	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"thought","signature":"` + sig + `"},
				{"type":"text","text":"Answer"}
			]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.Preserved == 0 {
		t.Fatalf("expected Claude signature to be preserved, got report=%+v", report)
	}
	keptSig := gjson.GetBytes(out, "messages.0.content.0.signature").String()
	if keptSig != sig {
		t.Fatalf("expected preserved signature %q, got %q", sig, keptSig)
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_StripsToolSignatures(t *testing.T) {
	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"assistant","content":[
				{"type":"tool_use","id":"tu_1","name":"weather","input":{},"signature":"bad-sig","model":"gemini-2.5-pro"}
			]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.DroppedSignatures == 0 {
		t.Fatalf("expected tool signatures to be dropped, got report=%+v", report)
	}
	if strings.Contains(string(out), "bad-sig") {
		t.Fatalf("tool signature should have been stripped: %s", string(out))
	}
	if gjson.GetBytes(out, "messages.0.content.0.model").Exists() {
		t.Fatalf("tool provenance field should have been stripped: %q", string(out))
	}
	if gjson.GetBytes(out, "messages.0.content.0.signature").Exists() {
		t.Fatalf("tool signature should have been stripped: %q", string(out))
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_DropsEmptyMessages(t *testing.T) {
	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"codex reasoning","signature":"gAAAAABopenai-encrypted-content"}
			]},
			{"role":"user","content":[{"type":"text","text":"next"}]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.DroppedBlocks == 0 {
		t.Fatalf("expected thinking block to be dropped, got report=%+v", report)
	}
	messages := gjson.GetBytes(out, "messages").Array()
	if len(messages) != 1 {
		t.Fatalf("expected 1 remaining message, got %d: %s", len(messages), string(out))
	}
	if messages[0].Get("role").String() != "user" {
		t.Fatalf("expected remaining role user, got %q", messages[0].Get("role").String())
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_LeavesPlainTextUnchanged(t *testing.T) {
	input := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role":"user","content":[{"type":"text","text":"hello"}]}
		]
	}`)

	out, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-3-5-sonnet-20241022")
	if report.Preserved+report.DroppedBlocks+report.DroppedSignatures+report.ReplacedSignatures != 0 {
		t.Fatalf("expected no changes for plain text, got report=%+v", report)
	}
	if string(out) != string(input) {
		t.Fatalf("expected output to be unchanged, got %s", string(out))
	}
}
