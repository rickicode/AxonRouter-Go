package kiro

import (
	"testing"
)

func TestSanitizeTools_StripsUnsupportedKeywords(t *testing.T) {
	tool := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "do_something",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code": map[string]any{
						"type":        "string",
						"description": "some code",
						"anyOf": []any{
							map[string]any{"type": "string"},
						},
					},
				},
				"additionalProperties": map[string]any{"type": "string"},
				"required":             []any{},
			},
		},
	}

	out, nameMap, err := SanitizeTools([]any{tool})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	fn := out[0].(map[string]any)["function"].(map[string]any)
	params := fn["parameters"].(map[string]any)
	if _, ok := params["additionalProperties"]; ok {
		t.Error("additionalProperties should have been removed")
	}
	props := params["properties"].(map[string]any)
	code := props["code"].(map[string]any)
	if _, ok := code["anyOf"]; ok {
		t.Error("anyOf should have been removed")
	}
	if _, ok := params["required"]; ok {
		t.Error("empty required should have been removed")
	}
	if len(nameMap) != 1 || nameMap["do_something"] != "do_something" {
		t.Errorf("unexpected nameMap: %v", nameMap)
	}
}

func TestNormalizeToolName_LongName(t *testing.T) {
	nameMap := make(map[string]string)
	longName := "this_is_a_very_long_tool_name_that_exceeds_the_sixty_four_character_limit_set_by_kiro_api"
	normalized := NormalizeToolName(longName, nameMap)
	if len(normalized) > ToolNameMaxLength {
		t.Errorf("normalized name too long: %d > %d", len(normalized), ToolNameMaxLength)
	}
	if nameMap[normalized] != longName {
		t.Errorf("nameMap did not preserve original name: %v", nameMap)
	}
}
