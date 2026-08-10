package executor

import (
	"encoding/json"
	"testing"
)

func TestInjectKiroProfileArn_BuilderID(t *testing.T) {
	body := []byte(`{"conversationState":{"currentMessage":{"userInputMessage":{"content":"hi"}}}}`)
	psd := map[string]string{"authMethod": "builder-id"}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["profileArn"] != kiroDefaultProfileARNBuilderID {
		t.Fatalf("profileArn = %v, want %v", raw["profileArn"], kiroDefaultProfileARNBuilderID)
	}
}

func TestInjectKiroProfileArn_SocialGitHub(t *testing.T) {
	body := []byte(`{"conversationState":{}}`)
	psd := map[string]string{"authMethod": "github"}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["profileArn"] != kiroDefaultProfileARNSocial {
		t.Fatalf("profileArn = %v, want %v", raw["profileArn"], kiroDefaultProfileARNSocial)
	}
}

func TestInjectKiroProfileArn_RespectsExistingBodyProfileArn(t *testing.T) {
	body := []byte(`{"conversationState":{},"profileArn":"arn:aws:codewhisperer:us-east-1:111122223333:profile/EXISTING"}`)
	psd := map[string]string{"authMethod": "builder-id"}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["profileArn"] != "arn:aws:codewhisperer:us-east-1:111122223333:profile/EXISTING" {
		t.Fatalf("existing profileArn overwritten: %v", raw["profileArn"])
	}
}

func TestInjectKiroProfileArn_ApiKeyNoDefault(t *testing.T) {
	body := []byte(`{"conversationState":{}}`)
	psd := map[string]string{"authMethod": "api_key"}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("api_key body changed: %s", out)
	}
}

func TestInjectKiroProfileArn_PSDProfileArnInjectedIntoBody(t *testing.T) {
	body := []byte(`{"conversationState":{}}`)
	psd := map[string]string{
		"authMethod": "builder-id",
		"profileArn": "arn:aws:codewhisperer:us-east-1:111122223333:profile/EXISTING",
	}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["profileArn"] != psd["profileArn"] {
		t.Fatalf("profileArn = %v, want %v", raw["profileArn"], psd["profileArn"])
	}
}

func TestInjectKiroProfileArn_ApiKeyNoDefaultWhenNoPSD(t *testing.T) {
	body := []byte(`{"conversationState":{}}`)
	psd := map[string]string{"authMethod": "api_key"}
	out, err := injectKiroProfileArn(body, psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("api_key body without psd profileArn changed: %s", out)
	}
}
