package provider

import (
	"reflect"
	"testing"
)

func TestServiceKindConstants(t *testing.T) {
	want := map[string]string{
		"llm":        ServiceKindLLM,
		"embedding":  ServiceKindEmbedding,
		"image":      ServiceKindImage,
		"imageToText": ServiceKindImageToText,
		"tts":        ServiceKindTTS,
		"stt":        ServiceKindSTT,
		"webSearch":  ServiceKindWebSearch,
		"webFetch":   ServiceKindWebFetch,
		"video":      ServiceKindVideo,
		"music":      ServiceKindMusic,
	}
	for expected, got := range want {
		if got != expected {
			t.Errorf("constant mismatch: want %q, got %q", expected, got)
		}
	}
}

func TestHasServiceKind(t *testing.T) {
	kinds := []string{ServiceKindLLM, ServiceKindEmbedding}
	if !HasServiceKind(kinds, ServiceKindLLM) {
		t.Errorf("expected HasServiceKind to find %q", ServiceKindLLM)
	}
	if HasServiceKind(kinds, ServiceKindImage) {
		t.Errorf("did not expect HasServiceKind to find %q", ServiceKindImage)
	}
	if HasServiceKind(nil, ServiceKindLLM) {
		t.Error("expected HasServiceKind to return false for nil slice")
	}
	if HasServiceKind(kinds, "LLM") {
		t.Error("expected HasServiceKind to be case-sensitive")
	}
}

func TestDefaultServiceKinds(t *testing.T) {
	got := DefaultServiceKinds()
	want := []string{ServiceKindLLM}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultServiceKinds() = %v, want %v", got, want)
	}
}
