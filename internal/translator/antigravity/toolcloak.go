// Package antigravity provides tool cloaking for Antigravity (Google Cloud Code Assist).
// Matches OmniRoute open-sse/config/toolCloaking.ts.
//
// Purpose: Antigravity detects third-party clients by tool names. Cloaking renames
// tools with _ide suffix and injects decoy tools to match the native Antigravity IDE
// tool fingerprint, preventing bans and usage penalties.
package antigravity

import "strings"

const AGToolSuffix = "_ide"

// agDefaultTools are native Antigravity tool names that should NOT be cloaked.
// Matches OmniRoute AG_DEFAULT_TOOL_NAMES.
var agDefaultTools = map[string]bool{
	"browser_subagent": true, "command_status": true, "find_by_name": true,
	"generate_image": true, "grep_search": true, "list_dir": true,
	"list_resources": true, "multi_replace_file_content": true, "notify_user": true,
	"read_resource": true, "read_terminal": true, "read_url_content": true,
	"replace_file_content": true, "run_command": true, "search_web": true,
	"send_command_input": true, "task_boundary": true, "view_content_chunk": true,
	"view_file": true, "write_to_file": true,
}

// AGDecoyToolNames are decoy tools injected into requests to match native Antigravity IDE fingerprint.
// Matches OmniRoute AG_DECOY_TOOL_NAMES.
var AGDecoyToolNames = []string{
	"browser_subagent", "command_status", "find_by_name",
	"generate_image", "grep_search", "list_dir",
	"list_resources", "multi_replace_file_content", "notify_user",
	"read_resource", "read_terminal", "read_url_content",
	"replace_file_content", "run_command", "search_web",
	"send_command_input", "task_boundary", "view_content_chunk",
	"view_file", "write_to_file",
	"mcp_sequential_thinking_sequentialthinking",
}

// ShouldCloak returns true if the tool name should be cloaked with _ide suffix.
func ShouldCloak(name string) bool {
	return name != "" && !agDefaultTools[name] && !strings.HasSuffix(name, AGToolSuffix)
}

// CloakName adds the _ide suffix if the tool should be cloaked.
func CloakName(name string) string {
	if ShouldCloak(name) {
		return name + AGToolSuffix
	}
	return name
}

// UncloakName removes the _ide suffix if present.
func UncloakName(name string) string {
	return strings.TrimSuffix(name, AGToolSuffix)
}

// WasCloaked returns true if the tool name has the _ide suffix.
func WasCloaked(name string) bool {
	return strings.HasSuffix(name, AGToolSuffix)
}

// ToolNameMap tracks cloaked→original tool name mappings for response uncloaking.
type ToolNameMap struct {
	m map[string]string // cloaked → original
}

// NewToolNameMap creates a new tool name map.
func NewToolNameMap() *ToolNameMap {
	return &ToolNameMap{m: make(map[string]string)}
}

// Record stores a cloaked→original mapping.
func (t *ToolNameMap) Record(cloaked, original string) {
	if cloaked != original {
		t.m[cloaked] = original
	}
}

// Original returns the original name for a cloaked name, or the name itself if not cloaked.
func (t *ToolNameMap) Original(cloaked string) string {
	if orig, ok := t.m[cloaked]; ok {
		return orig
	}
	return cloaked
}

// Len returns the number of tracked mappings.
func (t *ToolNameMap) Len() int {
	return len(t.m)
}

// StripEnumDescriptions recursively removes enumDescriptions from a JSON schema.
// Antigravity API rejects requests carrying that field with HTTP 400.
// Matches OmniRoute open-sse/config/toolCloaking.ts:63-87.
func StripEnumDescriptions(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	result := make(map[string]any, len(schema))
	for k, v := range schema {
		if k == "enumDescriptions" {
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			result[k] = StripEnumDescriptions(sub)
		} else if arr, ok := v.([]any); ok {
			newArr := make([]any, len(arr))
			for i, item := range arr {
				if sub, ok := item.(map[string]any); ok {
					newArr[i] = StripEnumDescriptions(sub)
				} else {
					newArr[i] = item
				}
			}
			result[k] = newArr
		} else {
			result[k] = v
		}
	}
	return result
}
