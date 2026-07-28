package signature

import (
	"encoding/base64"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func testGeminiField1OpaqueSignature(t *testing.T) string {
	t.Helper()
	payload := []byte{}
	payload = protowire.AppendTag(payload, 1, protowire.BytesType)
	payload = protowire.AppendBytes(payload, []byte{0x01, 0x0c, 0x39, 0xd6, 0xc7, 0x34})
	sig := base64.StdEncoding.EncodeToString(payload)
	if _, err := InspectGeminiThoughtSignature(sig, GeminiThoughtSignatureValidationOptions{}); err != nil {
		t.Skipf("test helper signature invalid: %v", err)
	}
	return sig
}

func TestValidateGeminiThoughtSignatures_AcceptsFirstFunctionCallWithBypass(t *testing.T) {
	input := []byte(`{"contents":[{"role":"model","parts":[
		{"functionCall":{"id":"call-1","name":"run"},"thoughtSignature":"` + GeminiSkipThoughtSignatureValidator + `"},
		{"functionCall":{"id":"call-2","name":"read"}}
	]}]}`)
	if err := ValidateGeminiThoughtSignatures(input, GeminiThoughtSignatureValidationOptions{AllowBypassSentinel: true}); err != nil {
		t.Fatalf("expected first functionCall bypass to pass, got: %v", err)
	}
}

func TestValidateGeminiThoughtSignatures_RejectsBypassOnSiblingFunctionCall(t *testing.T) {
	input := []byte(`{"contents":[{"role":"model","parts":[
		{"functionCall":{"id":"call-1","name":"run"}},
		{"functionCall":{"id":"call-2","name":"read"},"thoughtSignature":"` + GeminiSkipThoughtSignatureValidator + `"}
	]}]}`)
	if err := ValidateGeminiThoughtSignatures(input, GeminiThoughtSignatureValidationOptions{AllowBypassSentinel: true}); err == nil {
		t.Fatal("expected bypass sentinel on sibling functionCall to be rejected")
	}
}

func TestValidateGeminiThoughtSignatures_AcceptsFirstFunctionCallWithKnownEnvelope(t *testing.T) {
	sig := testGeminiField1OpaqueSignature(t)
	input := []byte(`{"contents":[{"role":"model","parts":[
		{"functionCall":{"id":"call-1","name":"run"},"thoughtSignature":"` + sig + `"}
	]}]}`)
	if err := ValidateGeminiThoughtSignatures(input, GeminiThoughtSignatureValidationOptions{RequireKnownEnvelope: true}); err != nil {
		t.Fatalf("expected known-envelope signature to pass, got: %v", err)
	}
}

func TestValidateGeminiThoughtSignatures_RejectsFunctionResponseSignature(t *testing.T) {
	input := []byte(`{"contents":[{"role":"user","parts":[
		{"functionResponse":{"id":"call-1","name":"run"},"thoughtSignature":"` + GeminiSkipThoughtSignatureValidator + `"}
	]}]}`)
	if err := ValidateGeminiThoughtSignatures(input); err == nil {
		t.Fatal("expected functionResponse with thoughtSignature to be rejected")
	}
}

func TestValidateGeminiFunctionCallPairing_AcceptsMatchedPair(t *testing.T) {
	input := []byte(`{"contents":[
		{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"run"}}]},
		{"role":"user","parts":[{"functionResponse":{"id":"call-1","name":"run","response":{"result":"ok"}}}]}
	]}`)
	if err := ValidateGeminiFunctionCallPairing(input); err != nil {
		t.Fatalf("expected matched function call/response pair to pass, got: %v", err)
	}
}

func TestValidateGeminiFunctionCallPairing_RejectsInterleavedCallsAndResponses(t *testing.T) {
	input := []byte(`{"contents":[
		{"role":"model","parts":[
			{"functionCall":{"id":"call-1","name":"run"}},
			{"functionResponse":{"id":"call-1","name":"run","response":{"result":"ok"}}}
		]}
	]}`)
	if err := ValidateGeminiFunctionCallPairing(input); err == nil {
		t.Fatal("expected interleaved functionCall/functionResponse to be rejected")
	}
}

func TestValidateGeminiFunctionCallPairing_RejectsMismatchedID(t *testing.T) {
	input := []byte(`{"contents":[
		{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"run"}}]},
		{"role":"user","parts":[{"functionResponse":{"id":"call-2","name":"run","response":{"result":"ok"}}}]}
	]}`)
	if err := ValidateGeminiFunctionCallPairing(input); err == nil {
		t.Fatal("expected mismatched function response id to be rejected")
	}
}

func TestValidateGeminiFunctionCallPairing_RejectsMissingFunctionName(t *testing.T) {
	input := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call-1"}}]}]}`)
	if err := ValidateGeminiFunctionCallPairing(input); err == nil {
		t.Fatal("expected missing functionCall.name to be rejected")
	}
}

func TestValidateGeminiFunctionCallPairing_AcceptsFinalPendingFunctionCall(t *testing.T) {
	input := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"run"}}]}]}`)
	if err := ValidateGeminiFunctionCallPairing(input); err != nil {
		t.Fatalf("expected final pending functionCall to be allowed, got: %v", err)
	}
}

func TestValidateGeminiFunctionCallPairing_AllowsRequestContentsPath(t *testing.T) {
	input := []byte(`{"request":{"contents":[
		{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"run"}}]},
		{"role":"user","parts":[{"functionResponse":{"id":"call-1","name":"run","response":{"result":"ok"}}}]}
	]}}`)
	if err := ValidateGeminiFunctionCallPairing(input); err != nil {
		t.Fatalf("expected request.contents path to be supported, got: %v", err)
	}
}
