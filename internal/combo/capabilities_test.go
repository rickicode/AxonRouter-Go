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

func TestDetectRequiredCapabilities_IgnoresPriorImageTurn(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":[{"type":"image_url","image_url":{"url":"[image]"}}]},
		{"role":"assistant","content":"I see the image."},
		{"role":"user","content":"just a plain text follow-up"}
	]}`)
	caps := DetectRequiredCapabilities(body)
	if caps.Vision {
		t.Fatalf("vision should not be required for a text-only trailing turn")
	}
}

func TestDetectRequiredCapabilities_TrailingTurnWithImage(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":"hello"},
		{"role":"assistant","content":"hi"},
		{"role":"user","content":[{"type":"image_url","image_url":{"url":"[image]"}}]}
	]}`)
	caps := DetectRequiredCapabilities(body)
	if !caps.Vision {
		t.Fatalf("vision should be required when the trailing turn contains an image")
	}
}

func TestDetectRequiredCapabilities_TrailingUserItems(t *testing.T) {
	// Returns everything when no assistant message exists.
	items := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "user", "content": "there"},
	}
	got := trailingUserItems(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}

	// Splits after the last assistant message.
	items = []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "content": "hello"},
		map[string]any{"role": "user", "content": "bye"},
	}
	got = trailingUserItems(items)
	if len(got) != 1 {
		t.Fatalf("expected 1 trailing item, got %d", len(got))
	}

	// Handles missing role fields gracefully.
	items = []any{
		map[string]any{"content": "no role"},
	}
	got = trailingUserItems(items)
	if len(got) != 1 {
		t.Fatalf("expected 1 trailing item without role, got %d", len(got))
	}

	// Empty slice returns empty.
	if len(trailingUserItems(nil)) != 0 {
		t.Fatalf("expected empty result for nil input")
	}
}
