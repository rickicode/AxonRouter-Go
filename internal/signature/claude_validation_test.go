package signature

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func testAnthropicNativeSignature(t *testing.T) string {
	t.Helper()
	payload := buildClaudeSignaturePayload(t, 12, uint64Ptr(2), "claude-sonnet-4-6", true)
	signature := base64.StdEncoding.EncodeToString(payload)
	if len(signature) < 50 {
		t.Fatalf("test signature too short: %d", len(signature))
	}
	return signature
}

func testAntigravityClaudeSignature(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte(testAnthropicNativeSignature(t)))
}

func buildClaudeSignaturePayload(t *testing.T, channelID uint64, field2 *uint64, modelText string, includeField7 bool) []byte {
	t.Helper()
	channelBlock := []byte{}
	channelBlock = protowire.AppendTag(channelBlock, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, channelID)
	if field2 != nil {
		channelBlock = protowire.AppendTag(channelBlock, 2, protowire.VarintType)
		channelBlock = protowire.AppendVarint(channelBlock, *field2)
	}
	if modelText != "" {
		channelBlock = protowire.AppendTag(channelBlock, 6, protowire.BytesType)
		channelBlock = protowire.AppendString(channelBlock, modelText)
	}
	if includeField7 {
		channelBlock = protowire.AppendTag(channelBlock, 7, protowire.VarintType)
		channelBlock = protowire.AppendVarint(channelBlock, 0)
	}

	container := []byte{}
	container = protowire.AppendTag(container, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)
	container = protowire.AppendTag(container, 2, protowire.BytesType)
	container = protowire.AppendBytes(container, bytes.Repeat([]byte{0x11}, 12))
	container = protowire.AppendTag(container, 3, protowire.BytesType)
	container = protowire.AppendBytes(container, bytes.Repeat([]byte{0x22}, 12))
	container = protowire.AppendTag(container, 4, protowire.BytesType)
	container = protowire.AppendBytes(container, bytes.Repeat([]byte{0x33}, 48))

	payload := []byte{}
	payload = protowire.AppendTag(payload, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	payload = protowire.AppendTag(payload, 3, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)
	return payload
}

func uint64Ptr(v uint64) *uint64 { return &v }

func TestValidateClaudeThinkingSignatures_AcceptsSingleAndDoubleLayer(t *testing.T) {
	raw := testAnthropicNativeSignature(t)
	double := testAntigravityClaudeSignature(t)
	input := []byte(`{"messages":[{"role":"assistant","content":[
		{"type":"thinking","thinking":"one","signature":"` + raw + `"},
		{"type":"thinking","thinking":"two","signature":"claude#` + double + `"}
	]}]}`)
	if err := ValidateClaudeThinkingSignatures(input, ClaudeSignatureValidationOptions{Strict: true}); err != nil {
		t.Fatalf("expected valid Claude signatures to pass, got: %v", err)
	}
}

func TestValidateClaudeThinkingSignatures_RejectsMissingSignature(t *testing.T) {
	input := []byte(`{"messages":[{"role":"assistant","content":[
		{"type":"thinking","thinking":"unsigned"}
	]}]}`)
	if err := ValidateClaudeThinkingSignatures(input); err == nil {
		t.Fatal("expected missing signature to be rejected")
	} else if !strings.Contains(err.Error(), "missing thinking signature") {
		t.Fatalf("expected missing signature error, got: %v", err)
	}
}

func TestValidateClaudeThinkingSignatures_RejectsOversizedSignature(t *testing.T) {
	sig := strings.Repeat("A", MaxClaudeThinkingSignatureLen+1)
	input := []byte(`{"messages":[{"role":"assistant","content":[
		{"type":"thinking","thinking":"t","signature":"` + sig + `"}
	]}]}`)
	if err := ValidateClaudeThinkingSignatures(input); err == nil {
		t.Fatal("expected oversized signature to be rejected")
	}
}

func TestValidateClaudeThinkingSignatures_RejectsNonREPrefix(t *testing.T) {
	payload := bytes.Repeat([]byte{0x34}, 48)
	sig := base64.StdEncoding.EncodeToString(payload)
	input := []byte(`{"messages":[{"role":"assistant","content":[
		{"type":"thinking","thinking":"t","signature":"` + sig + `"}
	]}]}`)
	if err := ValidateClaudeThinkingSignatures(input); err == nil {
		t.Fatal("expected non-R/E signature to be rejected")
	}
}

func TestInspectClaudeDoubleLayerSignature_RoundTrip(t *testing.T) {
	raw := testAnthropicNativeSignature(t)
	double := testAntigravityClaudeSignature(t)
	tree, err := InspectClaudeDoubleLayerSignature(double)
	if err != nil {
		t.Fatalf("InspectClaudeDoubleLayerSignature failed: %v", err)
	}
	if tree.EncodingLayers != 2 {
		t.Fatalf("expected 2 encoding layers, got %d", tree.EncodingLayers)
	}
	if tree.ChannelID != 12 {
		t.Fatalf("expected channel 12, got %d", tree.ChannelID)
	}
	if tree.RoutingClass != "routing_class_12" {
		t.Fatalf("unexpected routing class: %s", tree.RoutingClass)
	}

	treeSingle, err := InspectClaudeSingleLayerSignature(raw)
	if err != nil {
		t.Fatalf("InspectClaudeSingleLayerSignature failed: %v", err)
	}
	if treeSingle.EncodingLayers != 1 {
		t.Fatalf("expected 1 encoding layer, got %d", treeSingle.EncodingLayers)
	}
}

func TestInspectClaudeSignatures_RejectMalformed(t *testing.T) {
	tests := []struct {
		name string
		sig  string
	}{
		{"empty", ""},
		{"not base64", "E!!!invalid!!!"},
		{"R inner not E", base64.StdEncoding.EncodeToString([]byte("F" + strings.Repeat("a", 60)))},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if _, err := InspectClaudeSingleLayerSignature(tt.sig); err == nil {
				t.Fatal("expected malformed single-layer signature to be rejected")
			}
		})
	}
}
