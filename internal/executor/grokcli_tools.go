package executor

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// grokCLINamespaceToolName returns the namespace identifier for a namespace
// wrapper tool. It accepts either the standard Responses "name" field or the
// legacy "namespace" field used by older Grok CLI payloads.
func grokCLINamespaceToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("namespace").String())
}

// collectGrokCLINamespaceToolRefs returns a map from qualified tool name to the
// original namespace/name pair for every tool nested under a "namespace" tool
// in the top-level tools array or inside input items of type "additional_tools".
func collectGrokCLINamespaceToolRefs(body []byte) map[string]grokCLINamespaceToolRef {
	refs := make(map[string]grokCLINamespaceToolRef)
	if !gjson.ValidBytes(body) {
		return refs
	}
	collect := func(tools gjson.Result) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		for _, tool := range tools.Array() {
			if strings.TrimSpace(tool.Get("type").String()) != "namespace" {
				continue
			}
			namespaceName := grokCLINamespaceToolName(tool)
			if namespaceName == "" {
				continue
			}
			for _, nestedTool := range tool.Get("tools").Array() {
				toolName := strings.TrimSpace(nestedTool.Get("name").String())
				qualifiedName := grokCLIQualifyNamespaceToolName(namespaceName, toolName)
				if qualifiedName == "" {
					continue
				}
				refs[qualifiedName] = grokCLINamespaceToolRef{
					namespace: namespaceName,
					name:      toolName,
				}
			}
		}
	}
	collect(gjson.GetBytes(body, "tools"))
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for _, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
				collect(item.Get("tools"))
			}
		}
	}
	return refs
}

// grokCLIEffectiveDeclaredToolType returns the tool type that will actually be
// sent upstream after normalization. Client custom tools are rewritten to
// function declarations.
func grokCLIEffectiveDeclaredToolType(toolType string) string {
	if strings.TrimSpace(toolType) == "custom" {
		return "function"
	}
	return strings.TrimSpace(toolType)
}

// grokCLIClientToolKey returns a single-string key for a client-declared tool
// identity. Keys are stored as "type|namespace|name" using the declared
// (short) name, not the qualified upstream name.
func grokCLIClientToolKey(toolType, namespace, name string) string {
	return grokCLIEffectiveDeclaredToolType(toolType) + "|" + namespace + "|" + name
}

// collectGrokCLIClientDeclaredKeys snapshots the (type, namespace, name) identity
// of every client-declared callable tool before namespace wrappers are
// flattened and custom tools are rewritten to function declarations.
func collectGrokCLIClientDeclaredKeys(body []byte) map[string]bool {
	keys := make(map[string]bool)
	if !gjson.ValidBytes(body) {
		return keys
	}
	collect := func(tools gjson.Result) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		for _, tool := range tools.Array() {
			toolType := strings.TrimSpace(tool.Get("type").String())
			switch toolType {
		case "namespace":
			namespaceName := grokCLINamespaceToolName(tool)
			if namespaceName == "" {
				continue
			}

				for _, nestedTool := range tool.Get("tools").Array() {
					nestedType := strings.TrimSpace(nestedTool.Get("type").String())
					if nestedType != "function" && nestedType != "custom" {
						continue
					}
					toolName := strings.TrimSpace(nestedTool.Get("name").String())
					if toolName == "" {
						continue
					}
					keys[grokCLIClientToolKey(nestedType, namespaceName, toolName)] = true
				}
			case "function", "custom":
				toolName := strings.TrimSpace(tool.Get("name").String())
				if toolName == "" {
					continue
				}
				keys[grokCLIClientToolKey(toolType, "", toolName)] = true
			}
		}
	}
	collect(gjson.GetBytes(body, "tools"))
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for _, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
				collect(item.Get("tools"))
			}
		}
	}
	return keys
}

// grokcliFlattenNamespaceTools removes namespace wrappers, qualifies the names
// of tools declared inside namespaces, unwraps function-wrapped declarations,
// and rewrites custom tools to function declarations. The returned body is a
// newly marshaled copy when changes are required.
func grokcliFlattenNamespaceTools(body []byte, refs map[string]grokCLINamespaceToolRef) []byte {
	if refs == nil {
		refs = collectGrokCLINamespaceToolRefs(body)
	}
	if !gjson.ValidBytes(body) {
		return body
	}
	normalizeAtPath := func(path string) bool {
		tools := gjson.GetBytes(body, path)
		if !tools.Exists() || !tools.IsArray() {
			return true
		}
		filtered := []byte(`[]`)
		changed := false
		for _, tool := range tools.Array() {
			toolType := strings.TrimSpace(tool.Get("type").String())
		if toolType == "namespace" {
			changed = true
			namespaceName := grokCLINamespaceToolName(tool)
			for _, nestedTool := range tool.Get("tools").Array() {
					raw, ok := grokCLIFlattenTool(nestedTool, namespaceName)
					if !ok {
						continue
					}
					updated, errSet := sjson.SetRawBytes(filtered, "-1", raw)
					if errSet != nil {
						return false
					}
					filtered = updated
				}
				continue
			}
			raw, ok := grokCLIFlattenTool(tool, "")
			if !ok {
				continue
			}
			updated, errSet := sjson.SetRawBytes(filtered, "-1", raw)
			if errSet != nil {
				return false
			}
			filtered = updated
		}
		if !changed {
			return true
		}
		updated, errSet := sjson.SetRawBytes(body, path, filtered)
		if errSet != nil {
			return false
		}
		body = updated
		return true
	}

	if !normalizeAtPath("tools") {
		return body
	}
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for idx, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) != "additional_tools" {
				continue
			}
			if !normalizeAtPath("input." + strconv.Itoa(idx) + ".tools") {
				return body
			}
		}
	}
	return body
}

// grokCLIQualifyNamespaceToolName returns namespace__name, avoiding double
// qualification and leaving MCP-style names unchanged.
func grokCLIQualifyNamespaceToolName(namespaceName, toolName string) string {
	namespaceName = strings.TrimSpace(namespaceName)
	toolName = strings.TrimSpace(toolName)
	if namespaceName == "" || toolName == "" || strings.HasPrefix(toolName, "mcp__") {
		return toolName
	}
	prefix := namespaceName
	if !strings.HasSuffix(prefix, "__") {
		prefix += "__"
	}
	if strings.HasPrefix(toolName, prefix) {
		return toolName
	}
	return prefix + toolName
}

// grokCLIFlattenTool normalizes a single tool for upstream Grok CLI. It returns
// the normalized JSON and true, or nil/false when the tool should be dropped.
func grokCLIFlattenTool(tool gjson.Result, namespaceName string) ([]byte, bool) {
	raw := []byte(tool.Raw)
	toolType := strings.TrimSpace(tool.Get("type").String())

	// Unwrap function-wrapped declarations regardless of top-level type.
	if fn := tool.Get("function"); fn.Exists() && fn.IsObject() {
		if fnName := strings.TrimSpace(fn.Get("name").String()); fnName != "" {
			raw, _ = sjson.SetBytes(raw, "type", "function")
			raw, _ = sjson.SetBytes(raw, "name", fnName)
			if params := fn.Get("parameters"); params.Exists() {
				raw, _ = sjson.SetRawBytes(raw, "parameters", []byte(params.Raw))
			}
			raw, _ = sjson.DeleteBytes(raw, "function")
			toolType = "function"
		}
	}

	switch toolType {
	case "custom":
		raw, _ = sjson.SetBytes(raw, "type", "function")
		toolType = "function"
	case "function":
		// keep
	case "":
		return nil, false
	}

	if toolType == "function" {
		name := strings.TrimSpace(gjson.GetBytes(raw, "name").String())
		if name == "" {
			return nil, false
		}
		if namespaceName != "" {
			qualifiedName := grokCLIQualifyNamespaceToolName(namespaceName, name)
			raw, _ = sjson.SetBytes(raw, "name", qualifiedName)
		}
	}

	// Drop any nested namespace wrapper recursively.
	if toolType == "namespace" {
		return nil, false
	}

	return raw, true
}
