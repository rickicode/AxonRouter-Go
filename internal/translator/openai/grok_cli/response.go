package grok_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// grokStreamState holds per-stream bookkeeping for Grok CLI Responses → OpenAI.
type grokStreamState struct {
	MessageID          string
	Model              string
	ToolIndex          int
	ToolNames          map[int]string
	ToolArgs           map[string]string // call_id -> accumulated arguments
	Filter             *grokCLIStreamFilter
	ClientDeclaredKeys map[string]bool
	NamespaceRefs      map[string]grokCLINamespaceToolRef
}

var dataTag = []byte("data:")

func getGrokState(param *any) *grokStreamState {
	if *param == nil {
		*param = &grokStreamState{
			ToolIndex: -1,
			ToolNames: make(map[int]string),
			ToolArgs:  make(map[string]string),
			Filter:    newGrokCLIStreamFilter(),
		}
	}
	return (*param).(*grokStreamState)
}

type grokCLINamespaceToolRef struct {
	namespace string
	name      string
}

func grokCLINamespaceToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("namespace").String())
}

func grokCLIQualifyNamespaceToolName(namespaceName, toolName string) string {
	namespaceName = strings.TrimSpace(namespaceName)
	toolName = strings.TrimSpace(toolName)
	if namespaceName == "" || toolName == "" || strings.HasPrefix(toolName, "mcp__") {
		return ""
	}
	prefix := namespaceName
	if !strings.HasPrefix(toolName, namespaceName+"__") {
		prefix = namespaceName + "__"
	}
	return prefix + toolName
}

func grokCLIEffectiveDeclaredToolType(toolType string) string {
	if strings.TrimSpace(toolType) == "custom" {
		return "function"
	}
	return strings.TrimSpace(toolType)
}

func grokCLIClientToolKey(toolType, namespace, name string) string {
	return grokCLIEffectiveDeclaredToolType(toolType) + "|" + namespace + "|" + name
}

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

var grokCLIInternalXSearchToolNames = map[string]bool{
	"x_user_search":     true,
	"x_semantic_search": true,
	"x_keyword_search":  true,
	"x_thread_fetch":    true,
}

func grokcliIsInternalXSearchToolName(name string) bool {
	return grokCLIInternalXSearchToolNames[strings.TrimSpace(name)]
}

func grokcliResponseCallDeclaredType(itemType string) string {
	switch strings.TrimSpace(itemType) {
	case "function_call":
		return "function"
	case "custom_tool_call":
		return "custom"
	}
	return ""
}

func grokcliIsInternalXSearchCall(item gjson.Result, clientDeclaredKeys map[string]bool) bool {
	itemType := strings.TrimSpace(item.Get("type").String())
	declaredType := grokcliResponseCallDeclaredType(itemType)
	if declaredType == "" {
		return false
	}
	name := strings.TrimSpace(item.Get("name").String())
	if !grokcliIsInternalXSearchToolName(name) {
		return false
	}
	if namespace := strings.TrimSpace(item.Get("namespace").String()); namespace != "" {
		return false
	}
	callID := strings.TrimSpace(item.Get("call_id").String())
	if strings.HasPrefix(callID, "xs_call") {
		return true
	}
	if _, declared := clientDeclaredKeys[grokCLIClientToolKey(declaredType, "", name)]; declared {
		return false
	}
	return true
}

func grokcliRestoreNamespaceToolCalls(data []byte, refs map[string]grokCLINamespaceToolRef) []byte {
	if len(refs) == 0 || len(data) == 0 || !gjson.ValidBytes(data) {
		return data
	}
	data = grokcliRestoreNamespaceToolCallAtPath(data, "item", refs)
	output := gjson.GetBytes(data, "response.output")
	if output.Exists() && output.IsArray() {
		for index := range output.Array() {
			data = grokcliRestoreNamespaceToolCallAtPath(data, fmt.Sprintf("response.output.%d", index), refs)
		}
	}
	return data
}

func grokcliRestoreNamespaceToolCallAtPath(data []byte, path string, refs map[string]grokCLINamespaceToolRef) []byte {
	typ := gjson.GetBytes(data, path+".type").String()
	if typ != "function_call" && typ != "custom_tool_call" {
		return data
	}
	qualifiedName := strings.TrimSpace(gjson.GetBytes(data, path+".name").String())
	ref, ok := refs[qualifiedName]
	if !ok {
		return data
	}
	updated, errSet := sjson.SetBytes(data, path+".name", ref.name)
	if errSet != nil {
		return data
	}
	updated, errSet = sjson.SetBytes(updated, path+".namespace", ref.namespace)
	if errSet != nil {
		return data
	}
	return updated
}

type grokCLIStreamFilter struct {
	clientDeclaredKeys   map[string]bool
	droppedOutputIndexes map[int64]struct{}
	droppedItemIDs       map[string]struct{}
}

func newGrokCLIStreamFilter() *grokCLIStreamFilter {
	return &grokCLIStreamFilter{
		clientDeclaredKeys:   map[string]bool{},
		droppedOutputIndexes: make(map[int64]struct{}),
		droppedItemIDs:       make(map[string]struct{}),
	}
}

func (f *grokCLIStreamFilter) recordDroppedItem(eventData []byte, item gjson.Result) {
	if outputIndex := gjson.GetBytes(eventData, "output_index"); outputIndex.Exists() {
		f.droppedOutputIndexes[outputIndex.Int()] = struct{}{}
	}
	for _, path := range []string{"id", "call_id"} {
		if id := strings.TrimSpace(item.Get(path).String()); id != "" {
			f.droppedItemIDs[id] = struct{}{}
		}
	}
}

func (f *grokCLIStreamFilter) referencesDroppedItem(eventData []byte) bool {
	if outputIndex := gjson.GetBytes(eventData, "output_index"); outputIndex.Exists() {
		if _, dropped := f.droppedOutputIndexes[outputIndex.Int()]; dropped {
			return true
		}
	}
	for _, path := range []string{"item_id", "call_id"} {
		id := strings.TrimSpace(gjson.GetBytes(eventData, path).String())
		if _, dropped := f.droppedItemIDs[id]; id != "" && dropped {
			return true
		}
	}
	return false
}

func (f *grokCLIStreamFilter) compactOutputIndex(eventData []byte) []byte {
	outputIndex := gjson.GetBytes(eventData, "output_index")
	if !outputIndex.Exists() {
		return eventData
	}
	original := outputIndex.Int()
	removedBefore := int64(0)
	for dropped := range f.droppedOutputIndexes {
		if dropped < original {
			removedBefore++
		}
	}
	if removedBefore == 0 {
		return eventData
	}
	updated, errSet := sjson.SetBytes(eventData, "output_index", original-removedBefore)
	if errSet != nil {
		return eventData
	}
	return updated
}

func (f *grokCLIStreamFilter) filterCompletedOutput(eventData []byte) []byte {
	output := gjson.GetBytes(eventData, "response.output")
	if !output.IsArray() {
		return eventData
	}
	items := make([]json.RawMessage, 0, len(output.Array()))
	changed := false
	for _, item := range output.Array() {
		if grokcliIsInternalXSearchCall(item, f.clientDeclaredKeys) {
			f.recordDroppedItem(eventData, item)
			changed = true
			continue
		}
		items = append(items, json.RawMessage(item.Raw))
	}
	if !changed {
		return eventData
	}
	rawOutput, errMarshal := json.Marshal(items)
	if errMarshal != nil {
		return eventData
	}
	updated, errSet := sjson.SetRawBytes(eventData, "response.output", rawOutput)
	if errSet != nil {
		return eventData
	}
	return updated
}

func (f *grokCLIStreamFilter) apply(eventData []byte) []byte {
	if f == nil || len(eventData) == 0 || !gjson.ValidBytes(eventData) {
		return eventData
	}
	if item := gjson.GetBytes(eventData, "item"); grokcliIsInternalXSearchCall(item, f.clientDeclaredKeys) {
		f.recordDroppedItem(eventData, item)
		return nil
	}
	eventData = f.filterCompletedOutput(eventData)
	if f.referencesDroppedItem(eventData) {
		return nil
	}
	return f.compactOutputIndex(eventData)
}

// convertGrokResponseToOpenAIStream converts Grok CLI Responses streaming events
// to OpenAI Chat Completions SSE chunks.
func convertGrokResponseToOpenAIStream(_ context.Context, _ string, originalReq, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getGrokState(param)
	if state.ClientDeclaredKeys == nil && len(originalReq) > 0 {
		state.ClientDeclaredKeys = collectGrokCLIClientDeclaredKeys(originalReq)
		state.NamespaceRefs = collectGrokCLINamespaceToolRefs(originalReq)
		state.Filter.clientDeclaredKeys = state.ClientDeclaredKeys
	}

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}

	raw = grokcliRestoreNamespaceToolCalls(raw, state.NamespaceRefs)
	raw = state.Filter.apply(raw)
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("type").String()

	if state.MessageID == "" {
		state.MessageID = root.Get("response.id").String()
	}
	if state.Model == "" {
		state.Model = root.Get("response.model").String()
	}

	switch eventType {
	case "response.output_text.delta":
		text := root.Get("delta").String()
		if text == "" {
			return nil
		}
		chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, &text, nil)
		return [][]byte{chunk}

	case "response.output_item.done":
		item := root.Get("item")
		itemType := item.Get("type").String()
		if itemType == "function_call" || itemType == "custom_tool_call" {
			state.ToolIndex++
			name := item.Get("name").String()
			if ns := item.Get("namespace").String(); ns != "" {
				name = ns + "." + name
			}
			state.ToolNames[state.ToolIndex] = name
			callID := item.Get("call_id").String()
			args := item.Get("arguments").String()
			if args == "" {
				if acc, ok := state.ToolArgs[callID]; ok {
					args = acc
					delete(state.ToolArgs, callID)
				}
			}
			tc := map[string]interface{}{
				"index": state.ToolIndex,
				"id":    callID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": args,
				},
			}
			chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, nil, []map[string]interface{}{tc})
			return [][]byte{chunk}
		}

	case "response.completed":
		finishReason := "stop"
		if state.ToolIndex >= 0 {
			finishReason = "tool_calls"
		}
		chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, nil, nil)
		chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", finishReason)
		return [][]byte{chunk}

	case "response.function_call_arguments.delta":
		callID := root.Get("item_id").String()
		if callID == "" {
			callID = root.Get("call_id").String()
		}
		if callID != "" {
			state.ToolArgs[callID] += root.Get("delta").String()
		}
		return nil
	}

	return nil
}

// convertGrokResponseToOpenAINonStream converts a complete Grok CLI Responses
// response body to OpenAI Chat Completions format.
func convertGrokResponseToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	// Grok CLI's non-stream executor returns the SSE event wrapper for
	// /v1/responses compatibility. If we see a top-level "response" object,
	// unwrap it before translating to OpenAI Chat Completions format.
	if r := root.Get("response"); r.Exists() && r.Type == gjson.JSON {
		root = r
	}

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["object"] = "chat.completion"
	out["model"] = root.Get("model").String()

	var textParts []string
	var toolCalls []map[string]interface{}
	toolIdx := 0

	if output := root.Get("output"); output.Exists() && output.IsArray() {
		output.ForEach(func(_, item gjson.Result) bool {
			iType := item.Get("type").String()
			switch iType {
			case "message":
				if content := item.Get("content"); content.Exists() && content.IsArray() {
					content.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "output_text" {
							textParts = append(textParts, part.Get("text").String())
						}
						return true
					})
				}
			case "function_call", "custom_tool_call":
				toolIdx++
				name := item.Get("name").String()
				if ns := item.Get("namespace").String(); ns != "" {
					name = ns + "." + name
				}
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   item.Get("call_id").String(),
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": item.Get("arguments").String(),
					},
				})
			}
			return true
		})
	}

	msg := map[string]interface{}{
		"role": "assistant",
	}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	out["choices"] = []map[string]interface{}{{
		"index":         0,
		"message":       msg,
		"finish_reason": finishReason,
	}}

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"prompt_tokens":     usage.Get("input_tokens").Int(),
			"completion_tokens": usage.Get("output_tokens").Int(),
			"total_tokens":      usage.Get("input_tokens").Int() + usage.Get("output_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func buildOpenAIChunkFromGrok(id, model string, content *string, toolCalls []map[string]interface{}) []byte {
	chunk := []byte(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{}}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", "chatcmpl-"+id)
	chunk, _ = sjson.SetBytes(chunk, "model", model)
	if content != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", *content)
	}
	if toolCalls != nil {
		b, _ := json.Marshal(toolCalls)
		chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls", b)
	}
	return []byte(fmt.Sprintf("data: %s\n\n", string(chunk)))
}
