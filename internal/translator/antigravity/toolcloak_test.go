package antigravity

import (
	"slices"
	"testing"
)

func TestAGDecoyToolNames_Matches9Router(t *testing.T) {
	want := []string{
		"browser_subagent", "command_status", "find_by_name",
		"generate_image", "grep_search", "list_dir",
		"list_resources", "mcp_sequential-thinking_sequentialthinking",
		"multi_replace_file_content", "notify_user", "read_resource",
		"read_terminal", "read_url_content", "replace_file_content",
		"run_command", "search_web", "send_command_input",
		"task_boundary", "view_content_chunk", "view_file", "write_to_file",
	}

	if len(AGDecoyToolNames) != 21 {
		t.Fatalf("expected 21 decoy tools, got %d", len(AGDecoyToolNames))
	}
	if !slices.Equal(AGDecoyToolNames, want) {
		t.Errorf("AGDecoyToolNames mismatch\ngot:  %v\nwant: %v", AGDecoyToolNames, want)
	}
}

func TestAGDecoyToolNamesSet(t *testing.T) {
	for _, name := range AGDecoyToolNames {
		if !IsAGDecoyTool(name) {
			t.Errorf("expected %q to be in AGDecoyToolNamesSet", name)
		}
	}
	if IsAGDecoyTool("not_a_decoy_ide") {
		t.Error("expected non-decoy name to not be in set")
	}
}

func TestAGDefaultToolNames(t *testing.T) {
	if len(AGDefaultToolNames) != 20 {
		t.Fatalf("expected 20 native AG default tools, got %d", len(AGDefaultToolNames))
	}
	for name := range AGDefaultToolNames {
		if !IsAGDefaultTool(name) {
			t.Errorf("expected %q to be an AG default tool", name)
		}
	}
}

func TestShouldCloak(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"native AG tool", "run_command", false},
		{"already cloaked", "my_tool_ide", false},
		{"custom tool", "my_custom_tool", true},
		{"MCP decoy", "mcp_sequential-thinking_sequentialthinking", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldCloak(c.in); got != c.want {
				t.Errorf("ShouldCloak(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestCloakName(t *testing.T) {
	if got := CloakName("run_command"); got != "run_command" {
		t.Errorf("CloakName(run_command) = %q, want run_command", got)
	}
	if got := CloakName("custom_tool"); got != "custom_tool_ide" {
		t.Errorf("CloakName(custom_tool) = %q, want custom_tool_ide", got)
	}
	if got := CloakName("custom_tool_ide"); got != "custom_tool_ide" {
		t.Errorf("CloakName(custom_tool_ide) = %q, want custom_tool_ide", got)
	}
}

func TestUncloakName(t *testing.T) {
	if got := UncloakName("custom_tool_ide"); got != "custom_tool" {
		t.Errorf("UncloakName(custom_tool_ide) = %q, want custom_tool", got)
	}
	if got := UncloakName("custom_tool"); got != "custom_tool" {
		t.Errorf("UncloakName(custom_tool) = %q, want custom_tool", got)
	}
}

func TestWasCloaked(t *testing.T) {
	if !WasCloaked("custom_tool_ide") {
		t.Error("expected WasCloaked(custom_tool_ide) = true")
	}
	if WasCloaked("custom_tool") {
		t.Error("expected WasCloaked(custom_tool) = false")
	}
}

func TestShouldCloakForClient_Copilot(t *testing.T) {
	cases := []struct {
		name, toolName, client string
		wantCloak              bool
	}{
		{"copilot native AG tool", "run_command", CopilotToolClient, false},
		{"copilot custom tool", "my_custom_tool", CopilotToolClient, true},
		{"copilot already cloaked", "my_tool_ide", CopilotToolClient, false},
		{"non-copilot native AG tool", "run_command", "other", false},
		{"non-copilot custom tool", "my_custom_tool", "other", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShouldCloakForClient(c.toolName, c.client)
			if got != c.wantCloak {
				t.Errorf("ShouldCloakForClient(%q, %q) = %v, want %v", c.toolName, c.client, got, c.wantCloak)
			}
		})
	}
}

func TestCloakNameForClient_Copilot(t *testing.T) {
	if got := CloakNameForClient("run_command", CopilotToolClient); got != "run_command" {
		t.Errorf("CloakNameForClient(run_command, copilot) = %q, want run_command", got)
	}
	if got := CloakNameForClient("custom_tool", CopilotToolClient); got != "custom_tool_ide" {
		t.Errorf("CloakNameForClient(custom_tool, copilot) = %q, want custom_tool_ide", got)
	}
}

func TestIsCopilotNativeTool(t *testing.T) {
	if !IsCopilotNativeTool("run_command") {
		t.Error("expected run_command to be a native Copilot tool name")
	}
	if IsCopilotNativeTool("custom_tool") {
		t.Error("expected custom_tool to not be a native Copilot tool name")
	}
}
