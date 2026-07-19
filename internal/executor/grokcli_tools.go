package executor

import (
	"encoding/json"
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

// grokcliNormalizeTools post-processes every tool declaration after namespace
// flattening. It drops upstream pseudo-tools, rewrites custom declarations to
// function, strips web_search-specific unsupported fields, injects empty
// parameters when missing, and replaces fragile schemas with a permissive
// object shape.
func grokcliNormalizeTools(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}

	normalizeAtPath := func(path string) {
		tools := gjson.GetBytes(body, path)
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		out := []byte(`[]`)
		changed := false
		for _, tool := range tools.Array() {
			raw, ok := grokcliNormalizeSingleTool([]byte(tool.Raw))
			if !ok {
				changed = true
				continue
			}
			if string(raw) != tool.Raw {
				changed = true
			}
			out, _ = sjson.SetRawBytes(out, "-1", raw)
		}
		if changed {
			body, _ = sjson.SetRawBytes(body, path, out)
		}
	}

	normalizeAtPath("tools")
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for idx, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
				normalizeAtPath("input." + strconv.Itoa(idx) + ".tools")
			}
		}
	}
	return body
}

// grokcliNormalizeSingleTool normalizes one tool JSON and reports whether it
// should be kept.
func grokcliNormalizeSingleTool(raw []byte) ([]byte, bool) {
	toolType := strings.TrimSpace(gjson.GetBytes(raw, "type").String())

	switch toolType {
	case "tool_search", "image_generation":
		return nil, false
	case "custom":
		if strings.TrimSpace(gjson.GetBytes(raw, "name").String()) == "apply_patch" {
			return nil, false
		}
		raw, _ = sjson.SetBytes(raw, "type", "function")
		toolType = "function"
	case "web_search":
		if gjson.GetBytes(raw, "external_web_access").Exists() {
			raw, _ = sjson.DeleteBytes(raw, "external_web_access")
		}
	}

	if toolType == "function" {
		hasParams := gjson.GetBytes(raw, "parameters").Exists()
		if !hasParams {
			raw, _ = sjson.SetRawBytes(raw, "parameters", []byte(`{"type":"object"}`))
		} else if grokcliIsFragileParameters(raw) {
			raw, _ = sjson.SetRawBytes(raw, "parameters", []byte(`{"type":"object","additionalProperties":true}`))
			raw, _ = sjson.DeleteBytes(raw, "strict")
		}
	}

	return raw, true
}

// grokcliIsFragileParameters returns true when the tool parameters schema might
// confuse upstream Grok CLI, e.g. a non-object root type or a union containing
// non-object branches.
func grokcliIsFragileParameters(raw []byte) bool {
	params := gjson.GetBytes(raw, "parameters")
	if !params.Exists() || !params.IsObject() {
		return false
	}
	if t := strings.TrimSpace(params.Get("type").String()); t != "" && t != "object" {
		return true
	}
	unionNotObject := func(key string) bool {
		arr := params.Get(key)
		if !arr.Exists() || !arr.IsArray() {
			return false
		}
		for _, branch := range arr.Array() {
			if strings.TrimSpace(branch.Get("type").String()) != "object" {
				return true
			}
		}
		return false
	}
	if unionNotObject("anyOf") || unionNotObject("oneOf") {
		return true
	}
	return false
}

// grokcliCollectUpstreamToolNames collects the qualified names of function
// declarations that survive request normalization.
func grokcliCollectUpstreamToolNames(body []byte) map[string]bool {
	names := make(map[string]bool)
	if !gjson.ValidBytes(body) {
		return names
	}
	collectAt := func(path string) {
		tools := gjson.GetBytes(body, path)
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		for _, tool := range tools.Array() {
			if strings.TrimSpace(tool.Get("type").String()) != "function" {
				continue
			}
			name := strings.TrimSpace(tool.Get("name").String())
			if name != "" {
				names[name] = true
			}
		}
	}
	collectAt("tools")
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for idx, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
				collectAt("input." + strconv.Itoa(idx) + ".tools")
			}
		}
	}
	return names
}

// grokcliEnsureNativeXSearchTool appends the native x_search tool to the
// request if it is not already declared, and tells the caller whether it was
// added so downstream maps can be updated.
func grokcliEnsureNativeXSearchTool(body []byte) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}
	hasXSearch := func(path string) bool {
		tools := gjson.GetBytes(body, path)
		if !tools.Exists() || !tools.IsArray() {
			return false
		}
		for _, tool := range tools.Array() {
			if strings.TrimSpace(tool.Get("type").String()) == "x_search" {
				return true
			}
		}
		return false
	}
	if hasXSearch("tools") {
		return body, false
	}
	// Also consider additional_tools when deciding whether x_search is present.
	input := gjson.GetBytes(body, "input")
	if input.Exists() && input.IsArray() {
		for idx, item := range input.Array() {
			if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
				if hasXSearch("input." + strconv.Itoa(idx) + ".tools") {
					return body, false
				}
			}
		}
	}

	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		body, _ = sjson.SetRawBytes(body, "tools", []byte(`[{"type":"x_search"}]`))
	} else {
		body, _ = sjson.SetRawBytes(body, "tools.-1", []byte(`{"type":"x_search"}`))
	}

	// If the user explicitly restricted tool_choice to allowed_tools, keep
	// x_search eligible.
	if tc := gjson.GetBytes(body, "tool_choice"); tc.Exists() && tc.IsObject() {
		if strings.TrimSpace(tc.Get("type").String()) == "allowed_tools" {
			body, _ = sjson.SetRawBytes(body, "tool_choice.tools.-1", []byte(`{"type":"x_search"}`))
		}
	}

	return body, true
}

// grokcliNormalizeToolChoice prunes tool_choice references to tools that were
// dropped during normalization and qualifies names that carry a namespace.
func grokcliNormalizeToolChoice(body []byte, names map[string]bool) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	tc := gjson.GetBytes(body, "tool_choice")
	if !tc.Exists() {
		return body
	}

	deleteChoice := func() []byte {
		b, _ := sjson.DeleteBytes(body, "tool_choice")
		b, _ = sjson.DeleteBytes(b, "parallel_tool_calls")
		return b
	}

	if tc.Type == gjson.String {
		s := strings.ToLower(strings.TrimSpace(tc.String()))
		if s == "auto" || s == "none" || s == "required" || s == "any" {
			return body
		}
		return deleteChoice()
	}
	if !tc.IsObject() {
		return deleteChoice()
	}

	ctype := strings.TrimSpace(tc.Get("type").String())
	switch ctype {
	case "none", "auto", "required", "any":
		return body
	case "allowed_tools":
		toolsArr := tc.Get("tools")
		if !toolsArr.Exists() || !toolsArr.IsArray() {
			return deleteChoice()
		}
		out := []byte(`[]`)
		kept := false
		for _, entry := range toolsArr.Array() {
			raw, ok := grokcliNormalizeToolChoiceEntry([]byte(entry.Raw), names)
			if !ok {
				continue
			}
			out, _ = sjson.SetRawBytes(out, "-1", raw)
			kept = true
		}
		if !kept {
			return deleteChoice()
		}
		body, _ = sjson.SetRawBytes(body, "tool_choice.tools", out)
		return body
	default:
		raw, ok := grokcliNormalizeToolChoiceEntry([]byte(tc.Raw), names)
		if !ok {
			return deleteChoice()
		}
		body, _ = sjson.SetRawBytes(body, "tool_choice", raw)
		return body
	}
}

// grokcliNormalizeToolChoiceEntry normalizes a single tool_choice entry and
// reports whether it should survive.
func grokcliNormalizeToolChoiceEntry(raw []byte, names map[string]bool) ([]byte, bool) {
	entry := gjson.ParseBytes(raw)
	entryType := strings.TrimSpace(entry.Get("type").String())
	if entryType == "tool" {
		entryType = "function"
		raw, _ = sjson.SetBytes(raw, "type", "function")
	}
	// Native x_search is always eligible.
	if entryType == "x_search" {
		return raw, true
	}
	if entryType != "function" {
		return nil, false
	}

	ns := strings.TrimSpace(entry.Get("namespace").String())
	name := ""
	if fn := entry.Get("function"); fn.Exists() && fn.IsObject() {
		name = strings.TrimSpace(fn.Get("name").String())
	}
	if name == "" {
		name = strings.TrimSpace(entry.Get("name").String())
	}
	qualified := grokCLIQualifyNamespaceToolName(ns, name)
	if qualified == "" || !names[qualified] {
		return nil, false
	}

	raw, _ = sjson.SetBytes(raw, "type", "function")
	raw, _ = sjson.SetBytes(raw, "name", qualified)
	raw, _ = sjson.DeleteBytes(raw, "namespace")
	raw, _ = sjson.DeleteBytes(raw, "function")
	return raw, true
}

// grokcliNormalizeInputItems converts legacy/custom input item types into the
// canonical function_call/function_call_output shapes and qualifies names that
// carry an explicit namespace.
func grokcliNormalizeInputItems(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	var out []any
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Type == gjson.String {
			out = append(out, item.String())
			return true
		}
		if !item.IsObject() {
			return true
		}
		m := make(map[string]any)
		item.ForEach(func(k, v gjson.Result) bool {
			if v.Type == gjson.String {
				m[k.String()] = v.String()
			} else {
				var val any
				_ = json.Unmarshal([]byte(v.Raw), &val)
				m[k.String()] = val
			}
			return true
		})

		typ, _ := m["type"].(string)
		switch typ {
		case "custom_tool_call", "tool_use":
			m["type"] = "function_call"
		case "custom_tool_call_output", "tool_result":
			m["type"] = "function_call_output"
		}

		if _, hasArgs := m["arguments"]; !hasArgs {
			if inp := m["input"]; inp != nil && (typ == "custom_tool_call" || typ == "tool_use") {
				b, _ := json.Marshal(inp)
				m["arguments"] = string(b)
			}
			delete(m, "input")
		}

		if name, ok := m["name"].(string); ok {
			if ns, ok := m["namespace"].(string); ok && ns != "" {
				m["name"] = grokCLIQualifyNamespaceToolName(ns, name)
				delete(m, "namespace")
			}
		}

		out = append(out, m)
		return true
	})

	raw, err := json.Marshal(out)
	if err != nil {
		return body, err
	}
	return sjson.SetRawBytes(body, "input", raw)
}
