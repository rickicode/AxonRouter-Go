package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestRemapOAuthToolNames(t *testing.T) {
	body := []byte(`{"tools":[{"name":"bash"},{"name":"read"}],"messages":[{"role":"user","content":[{"type":"tool_use","name":"glob","tool_use_id":"1"}]}]}`)

	out, reverseMap := remapOAuthToolNames(body)
	if len(reverseMap) != 3 {
		t.Fatalf("expected reverse map with 3 entries, got %v", reverseMap)
	}
	if gjson.GetBytes(out, "tools.0.name").String() != "Bash" {
		t.Fatalf("expected Bash, got %s", gjson.GetBytes(out, "tools.0.name").String())
	}
	if gjson.GetBytes(out, "messages.0.content.0.name").String() != "Glob" {
		t.Fatalf("expected Glob, got %s", gjson.GetBytes(out, "messages.0.content.0.name").String())
	}
	if reverseMap["Bash"] != "bash" || reverseMap["Glob"] != "glob" {
		t.Fatalf("unexpected reverse map: %v", reverseMap)
	}
}

func TestReverseRemapOAuthToolNames(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","name":"Bash","tool_use_id":"1"},{"type":"tool_reference","tool_name":"Glob"}]}`)
	reverseMap := map[string]string{"Bash": "bash", "Glob": "glob"}

	out := reverseRemapOAuthToolNames(body, reverseMap)
	if gjson.GetBytes(out, "content.0.name").String() != "bash" {
		t.Fatalf("expected bash, got %s", gjson.GetBytes(out, "content.0.name").String())
	}
	if gjson.GetBytes(out, "content.1.tool_name").String() != "glob" {
		t.Fatalf("expected glob, got %s", gjson.GetBytes(out, "content.1.tool_name").String())
	}
}

func TestReverseRemapOAuthToolNamesFromStreamLine(t *testing.T) {
	line := []byte("data: {\"content_block\":{\"type\":\"tool_use\",\"name\":\"Bash\"}}")
	reverseMap := map[string]string{"Bash": "bash"}

	out := reverseRemapOAuthToolNamesFromStreamLine(line, reverseMap)
	if gjson.GetBytes(out, "content_block.name").String() != "bash" {
		t.Fatalf("expected bash in stream line, got %s", out)
	}
}
