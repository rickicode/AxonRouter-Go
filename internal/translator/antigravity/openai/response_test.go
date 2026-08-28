package openai

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity"
)

// TestCloakedToolNameMap_RestoresOriginal verifies that the response uncloak map
// reconstructs the exact original client tool name (including characters that
// required sanitization, and regardless of whether the name was suffixed with _ide)
// from the cloaked name Antigravity returns. This closes the parity gap vs 9Router's
// toolNameMap / the Claude response path's buildToolNameMap.
//
// The expected cloaked form is derived with the same formula the request translator
// uses (CloakName(SanitizeFunctionName(name))) so the test proves the round-trip rather
// than hard-coding sanitization details.
func TestCloakedToolNameMap_RestoresOriginal(t *testing.T) {
	req := []byte(`{
		"tools": [
			{"type": "function", "function": {"name": "read_file"}},
			{"type": "function", "function": {"name": "my-tool"}},
			{"type": "function", "function": {"name": "search web"}},
			{"type": "function", "function": {"name": "Do_Thing.42"}}
		]
	}`)

	m := CloakedToolNameMap(req)
	if m == nil {
		t.Fatal("expected non-nil map for request with tools")
	}

	originals := []string{"read_file", "my-tool", "search web", "Do_Thing.42"}
	for _, original := range originals {
		cloaked := antigravity.CloakName(SanitizeFunctionName(original))
		got := RestoreCloakedToolName(m, cloaked)
		if got != original {
			t.Errorf("round-trip failed: original %q -> Antigravity returns %q -> restored %q (want %q)",
				original, cloaked, got, original)
		}
	}
}

// TestCloakedToolNameMap_NoTools verifies a request without tools yields a nil map.
func TestCloakedToolNameMap_NoTools(t *testing.T) {
	req := []byte(`{"messages": [{"role": "user", "content": "hi"}]}`)
	if m := CloakedToolNameMap(req); m != nil {
		t.Errorf("expected nil map for request without tools, got %v", m)
	}
	if m := CloakedToolNameMap(nil); m != nil {
		t.Errorf("expected nil map for nil input, got %v", m)
	}
}

// TestRestoreCloakedToolName_UnknownName verifies names not present in the map
// (e.g. an AG native decoy the upstream somehow echoed) pass through unchanged.
func TestRestoreCloakedToolName_UnknownName(t *testing.T) {
	m := map[string]string{"search_ide": "search"}
	if got := RestoreCloakedToolName(m, "list_dir"); got != "list_dir" {
		t.Errorf("expected unknown name to pass through, got %q", got)
	}
	if got := RestoreCloakedToolName(nil, "search_ide"); got != "search_ide" {
		t.Errorf("expected passthrough on nil map, got %q", got)
	}
}
