package combo

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

func TestDetectRequiredCapabilities_Vision(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Vision {
		t.Fatalf("expected vision capability required")
	}
}

func TestDetectRequiredCapabilities_ResponsesInputImage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"input_image","detail":"auto"}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Vision {
		t.Fatalf("expected vision capability required for input_image")
	}
}

func TestDetectRequiredCapabilities_ResponsesInputFilePDF(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"input_file","file":{"filename":"report.pdf"}}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.PDF {
		t.Fatalf("expected PDF capability required for input_file PDF")
	}
}

func TestDetectRequiredCapabilities_Tools(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"x"}}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Tools {
		t.Fatalf("expected tools capability required")
	}
}

func TestDetectRequiredCapabilities_GeminiContents_Vision(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"ABC123"}}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Vision {
		t.Fatalf("expected vision capability required for Gemini inlineData image")
	}
}

func TestDetectRequiredCapabilities_GeminiContents_Audio(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"audio/wav","data":"ABC123"}}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.AudioInput {
		t.Fatalf("expected audio_input capability required for Gemini inlineData audio")
	}
}

func TestDetectRequiredCapabilities_GeminiContents_PDF(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"application/pdf","fileUri":"gs://bucket/report.pdf"}}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.PDF {
		t.Fatalf("expected PDF capability required for Gemini fileData PDF")
	}
}

func TestDetectRequiredCapabilities_GeminiContents_TextOnly(t *testing.T) {
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	caps := DetectRequiredCapabilities(body)
	if caps.Vision || caps.AudioInput || caps.VideoInput || caps.PDF || caps.Tools {
		t.Fatalf("expected no media capability required for text-only Gemini content")
	}
}

func TestDetectRequiredCapabilities_AntigravityContents_Vision(t *testing.T) {
	body := []byte(`{"project":"demo","request":{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/jpeg","data":"RAWBASE64"}}]}]}}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Vision {
		t.Fatalf("expected vision capability required for Antigravity request.contents image")
	}
}

func TestReorderStepsByCapabilities_PrioritizesVision(t *testing.T) {
	database := newComboTestDB(t)
	seedConnectionForCombo(t, database, "conn-1")

	steps := []db.ComboStep{
		{ID: "a", ModelID: "openai/gpt-3.5-turbo", Priority: 1},
		{ID: "b", ModelID: "openai/gpt-4o", Priority: 2},
	}
	required := models.ModelCapabilities{Vision: true}

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewHandler(database, store, elig)

	out := h.ReorderStepsByCapabilities(steps, required)
	if len(out) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(out))
	}
	if out[0].ModelID != "openai/gpt-4o" {
		t.Fatalf("expected vision-capable model first, got %s", out[0].ModelID)
	}
}
