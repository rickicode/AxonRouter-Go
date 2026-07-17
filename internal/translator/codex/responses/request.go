package responses

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIRequestToCodex converts an OpenAI Chat Completions request JSON
// into an OpenAI Responses API request JSON compatible with the Codex upstream.
// It follows the behavior of CLIProxyAPI's chat-completions→Responses translator:
// forced stream/store/reasoning defaults, message→input conversion, tool name
// shortening, response_format mapping, and stripping of unsupported fields.
var codexHostedToolTypes = map[string]struct{}{
	"image_generation": {}, "web_search": {}, "web_search_preview": {},
	"file_search": {}, "computer": {}, "computer_use_preview": {},
	"code_interpreter": {}, "mcp": {}, "local_shell": {}, "tool_search": {},
}

func ConvertOpenAIRequestToCodex(modelName string, inputRawJSON []byte, stream bool) []byte {
	rawJSON := inputRawJSON

	// Start with empty JSON object.
	out := []byte(`{}`)

	// Stream must be set as requested (Codex requires true for streaming).
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Map reasoning effort with a default of "medium".
	reasoningEffort := "medium"
	if v := gjson.GetBytes(rawJSON, "reasoning_effort"); v.Exists() {
		reasoningEffort = normalizeCodexReasoningEffort(v.String())
	}
	out, _ = sjson.SetBytes(out, "reasoning.effort", reasoningEffort)
	out, _ = sjson.SetBytes(out, "parallel_tool_calls", true)
	out, _ = sjson.SetBytes(out, "reasoning.summary", "auto")
	if reasoningEffort != "" && reasoningEffort != "none" {
		out, _ = sjson.SetBytes(out, "include", []string{"reasoning.encrypted_content"})
	}

	// Preserve Responses-API fields that Codex supports if they are already present.
	if v := gjson.GetBytes(rawJSON, "instructions"); v.Exists() && v.Type == gjson.String && v.String() != "" {
		out, _ = sjson.SetBytes(out, "instructions", v.String())
	}
	if v := gjson.GetBytes(rawJSON, "prompt_cache_key"); v.Exists() {
		out, _ = sjson.SetBytes(out, "prompt_cache_key", v.Value())
	}
	if v := gjson.GetBytes(rawJSON, "client_metadata"); v.Exists() {
		out, _ = sjson.SetRawBytes(out, "client_metadata", []byte(v.Raw))
	}

	// Model
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Build tool name shortening map from original tools (if any).
	originalToolNameMap := map[string]string{}
	{
		tools := gjson.GetBytes(rawJSON, "tools")
		if tools.IsArray() && len(tools.Array()) > 0 {
			var names []string
			arr := tools.Array()
			for i := 0; i < len(arr); i++ {
				t := arr[i]
				if t.Get("type").String() == "function" {
					fn := t.Get("function")
					if fn.Exists() {
						if v := fn.Get("name"); v.Exists() {
							names = append(names, v.String())
						}
					}
				}
			}
			if len(names) > 0 {
				originalToolNameMap = buildShortNameMap(names)
			}
		}
	}

	// Build input from messages, handling all message types including tool calls.
	out, _ = sjson.SetRawBytes(out, "input", []byte(`[]`))
	if messages := gjson.GetBytes(rawJSON, "messages"); messages.IsArray() {
		arr := messages.Array()
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()
			switch role {
			case "tool":
				// Handle tool response messages as top-level function_call_output objects.
				toolCallID := m.Get("tool_call_id").String()
				content := m.Get("content")
				funcOutput := []byte(`{}`)
				funcOutput, _ = sjson.SetBytes(funcOutput, "type", "function_call_output")
				funcOutput, _ = sjson.SetBytes(funcOutput, "call_id", toolCallID)
				funcOutput = setToolCallOutputContent(funcOutput, content)
				out, _ = sjson.SetRawBytes(out, "input.-1", funcOutput)
			default:
				// Handle regular messages.
				msg := []byte(`{}`)
				msg, _ = sjson.SetBytes(msg, "type", "message")
				if role == "system" {
					msg, _ = sjson.SetBytes(msg, "role", "developer")
				} else {
					msg, _ = sjson.SetBytes(msg, "role", role)
				}
				msg, _ = sjson.SetRawBytes(msg, "content", []byte(`[]`))

				c := m.Get("content")
				if c.Exists() && c.Type == gjson.String && c.String() != "" {
					partType := "input_text"
					if role == "assistant" {
						partType = "output_text"
					}
					part := []byte(`{}`)
					part, _ = sjson.SetBytes(part, "type", partType)
					part, _ = sjson.SetBytes(part, "text", c.String())
					msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
				} else if c.Exists() && c.IsArray() {
					items := c.Array()
					for j := 0; j < len(items); j++ {
						it := items[j]
						t := it.Get("type").String()
						switch t {
						case "text":
							partType := "input_text"
							if role == "assistant" {
								partType = "output_text"
							}
							part := []byte(`{}`)
							part, _ = sjson.SetBytes(part, "type", partType)
							part, _ = sjson.SetBytes(part, "text", it.Get("text").String())
							msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
						case "image_url":
							if role == "user" {
								part := []byte(`{}`)
								part, _ = sjson.SetBytes(part, "type", "input_image")
								if u := it.Get("image_url.url"); u.Exists() {
									part, _ = sjson.SetBytes(part, "image_url", u.String())
								}
								msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
							}
						case "file":
							if role == "user" {
								fileData := it.Get("file.file_data").String()
								filename := it.Get("file.filename").String()
								if fileData != "" {
									part := []byte(`{}`)
									part, _ = sjson.SetBytes(part, "type", "input_file")
									part, _ = sjson.SetBytes(part, "file_data", fileData)
									if filename != "" {
										part, _ = sjson.SetBytes(part, "filename", filename)
									}
									msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
								}
							}
						case "input_audio":
							if role == "user" {
								audioData := it.Get("input_audio.data").String()
								audioFormat := it.Get("input_audio.format").String()
								if audioData != "" {
									part := []byte(`{}`)
									part, _ = sjson.SetBytes(part, "type", "input_audio")
									part, _ = sjson.SetBytes(part, "data", audioData)
									if audioFormat != "" {
										part, _ = sjson.SetBytes(part, "format", audioFormat)
									}
									msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
								}
							}
						}
					}
				}
				// Don't emit empty assistant messages when only tool_calls are present.
				if role != "assistant" || len(gjson.GetBytes(msg, "content").Array()) > 0 {
					out, _ = sjson.SetRawBytes(out, "input.-1", msg)
				}
				// Handle tool calls for assistant messages as separate top-level objects.
				if role == "assistant" {
					toolCalls := m.Get("tool_calls")
					if toolCalls.Exists() && toolCalls.IsArray() {
						toolCallsArr := toolCalls.Array()
						for j := 0; j < len(toolCallsArr); j++ {
							tc := toolCallsArr[j]
							if tc.Get("type").String() == "function" {
								funcCall := []byte(`{}`)
								funcCall, _ = sjson.SetBytes(funcCall, "type", "function_call")
								funcCall, _ = sjson.SetBytes(funcCall, "call_id", tc.Get("id").String())
								{
									name := tc.Get("function.name").String()
									if short, ok := originalToolNameMap[name]; ok {
										name = short
									} else {
										name = shortenNameIfNeeded(name)
									}
									funcCall, _ = sjson.SetBytes(funcCall, "name", name)
								}
								funcCall, _ = sjson.SetBytes(funcCall, "arguments", tc.Get("function.arguments").String())
								out, _ = sjson.SetRawBytes(out, "input.-1", funcCall)
							}
						}
					}
				}
			}
		}
	}

	// Map response_format and text settings to Responses API text.format.
	rf := gjson.GetBytes(rawJSON, "response_format")
	text := gjson.GetBytes(rawJSON, "text")
	if rf.Exists() {
		if !gjson.GetBytes(out, "text").Exists() {
			out, _ = sjson.SetRawBytes(out, "text", []byte(`{}`))
		}
		rft := rf.Get("type").String()
		switch rft {
		case "text":
			out, _ = sjson.SetBytes(out, "text.format.type", "text")
		case "json_schema":
			js := rf.Get("json_schema")
			if js.Exists() {
				out, _ = sjson.SetBytes(out, "text.format.type", "json_schema")
				if v := js.Get("name"); v.Exists() {
					out, _ = sjson.SetBytes(out, "text.format.name", v.Value())
				}
				if v := js.Get("strict"); v.Exists() {
					out, _ = sjson.SetBytes(out, "text.format.strict", v.Value())
				}
				if v := js.Get("schema"); v.Exists() {
					out, _ = sjson.SetRawBytes(out, "text.format.schema", []byte(v.Raw))
				}
			}
		}
		if text.Exists() {
			if v := text.Get("verbosity"); v.Exists() {
				out, _ = sjson.SetBytes(out, "text.verbosity", v.Value())
			}
		}
	} else if text.Exists() {
		if v := text.Get("verbosity"); v.Exists() {
			if !gjson.GetBytes(out, "text").Exists() {
				out, _ = sjson.SetRawBytes(out, "text", []byte(`{}`))
			}
			out, _ = sjson.SetBytes(out, "text.verbosity", v.Value())
		}
	}

	// Map tools (flatten function fields).
	tools := gjson.GetBytes(rawJSON, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		out, _ = sjson.SetRawBytes(out, "tools", []byte(`[]`))
		arr := tools.Array()
		for i := 0; i < len(arr); i++ {
			t := arr[i]
			toolType := t.Get("type").String()
	// Pass through hosted tools Codex executes server-side, plus custom passthrough tools.
		if toolType != "" && toolType != "function" && t.IsObject() {
			_, hosted := codexHostedToolTypes[toolType]
			if hosted || toolType == "custom" {
				out, _ = sjson.SetRawBytes(out, "tools.-1", []byte(t.Raw))
			}
			continue
		}
			if toolType == "function" {
				item := []byte(`{}`)
				item, _ = sjson.SetBytes(item, "type", "function")
				fn := t.Get("function")
				if fn.Exists() {
					if v := fn.Get("name"); v.Exists() {
						name := v.String()
						if short, ok := originalToolNameMap[name]; ok {
							name = short
						} else {
							name = shortenNameIfNeeded(name)
						}
						item, _ = sjson.SetBytes(item, "name", name)
					}
					if v := fn.Get("description"); v.Exists() {
						item, _ = sjson.SetBytes(item, "description", v.Value())
					}
					if v := fn.Get("parameters"); v.Exists() {
						item, _ = sjson.SetRawBytes(item, "parameters", []byte(v.Raw))
					}
					if v := fn.Get("strict"); v.Exists() {
						item, _ = sjson.SetBytes(item, "strict", v.Value())
					}
				}
				out, _ = sjson.SetRawBytes(out, "tools.-1", item)
			}
		}
	}

	// Map tool_choice when present.
	if tc := gjson.GetBytes(rawJSON, "tool_choice"); tc.Exists() {
		switch {
		case tc.Type == gjson.String:
			out, _ = sjson.SetBytes(out, "tool_choice", tc.String())
		case tc.IsObject():
			tcType := tc.Get("type").String()
			if tcType == "function" {
				name := tc.Get("function.name").String()
				if name != "" {
					if short, ok := originalToolNameMap[name]; ok {
						name = short
					} else {
						name = shortenNameIfNeeded(name)
					}
				}
				choice := []byte(`{}`)
				choice, _ = sjson.SetBytes(choice, "type", "function")
				if name != "" {
					choice, _ = sjson.SetBytes(choice, "name", name)
				}
				out, _ = sjson.SetRawBytes(out, "tool_choice", choice)
			} else if tcType != "" {
				// Built-in tool choices (e.g. {"type":"web_search"}) are already Responses-compatible.
				out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(tc.Raw))
			}
		}
	}

	// Codex rejects several Chat Completions fields.
	out, _ = sjson.DeleteBytes(out, "max_output_tokens")
	out, _ = sjson.DeleteBytes(out, "max_completion_tokens")
	out, _ = sjson.DeleteBytes(out, "max_tokens")
	out, _ = sjson.DeleteBytes(out, "temperature")
	out, _ = sjson.DeleteBytes(out, "top_p")
	out, _ = sjson.DeleteBytes(out, "frequency_penalty")
	out, _ = sjson.DeleteBytes(out, "presence_penalty")
	out, _ = sjson.DeleteBytes(out, "logprobs")
	out, _ = sjson.DeleteBytes(out, "top_logprobs")
	out, _ = sjson.DeleteBytes(out, "n")
	out, _ = sjson.DeleteBytes(out, "seed")
	out, _ = sjson.DeleteBytes(out, "truncation")
	out, _ = sjson.DeleteBytes(out, "context_management")
	out, _ = sjson.DeleteBytes(out, "user")
	out, _ = sjson.DeleteBytes(out, "metadata")
	out, _ = sjson.DeleteBytes(out, "stream_options")
	out, _ = sjson.DeleteBytes(out, "safety_identifier")
	out, _ = sjson.DeleteBytes(out, "prompt_cache_retention")
	out, _ = sjson.DeleteBytes(out, "previous_response_id")

	// service_tier is restricted to "priority" by Codex. "fast" is mapped to "priority".
	if st := gjson.GetBytes(out, "service_tier"); st.Exists() {
		if st.String() == "fast" {
			out, _ = sjson.SetBytes(out, "service_tier", "priority")
		} else if st.String() != "priority" {
			out, _ = sjson.DeleteBytes(out, "service_tier")
		}
	}

	out, _ = sjson.SetBytes(out, "store", false)

	// Final allowlist — strip any unknown top-level field that could trigger
	// upstream "routing_unsupported" / type validation errors.
	allowed := map[string]struct{}{
		"model": {}, "input": {}, "instructions": {}, "tools": {},
		"tool_choice": {}, "stream": {}, "store": {}, "reasoning": {},
		"parallel_tool_calls": {}, "service_tier": {}, "include": {},
		"prompt_cache_key": {}, "client_metadata": {}, "text": {},
	}
	root := gjson.ParseBytes(out)
	if root.IsObject() {
		root.ForEach(func(key, _ gjson.Result) bool {
			if _, ok := allowed[key.String()]; !ok {
				out, _ = sjson.DeleteBytes(out, key.String())
			}
			return true
		})
	}

	return out
}

func setToolCallOutputContent(funcOutput []byte, content gjson.Result) []byte {
	switch {
	case content.Type == gjson.String:
		funcOutput, _ = sjson.SetBytes(funcOutput, "output", content.String())
	case content.IsArray():
		output := []byte(`[]`)
		for _, item := range content.Array() {
			output = appendToolOutputContentPart(output, item)
		}
		funcOutput, _ = sjson.SetRawBytes(funcOutput, "output", output)
	default:
		fallbackOutput := content.Raw
		if fallbackOutput == "" {
			fallbackOutput = content.String()
		}
		funcOutput, _ = sjson.SetBytes(funcOutput, "output", fallbackOutput)
	}
	return funcOutput
}

func appendToolOutputContentPart(output []byte, item gjson.Result) []byte {
	switch item.Get("type").String() {
	case "text":
		part := []byte(`{}`)
		part, _ = sjson.SetBytes(part, "type", "input_text")
		part, _ = sjson.SetBytes(part, "text", item.Get("text").String())
		output, _ = sjson.SetRawBytes(output, "-1", part)
	case "image_url":
		imageURL := item.Get("image_url.url").String()
		fileID := item.Get("image_url.file_id").String()
		if imageURL == "" && fileID == "" {
			return appendToolOutputFallbackPart(output, item)
		}
		part := []byte(`{}`)
		part, _ = sjson.SetBytes(part, "type", "input_image")
		if imageURL != "" {
			part, _ = sjson.SetBytes(part, "image_url", imageURL)
		}
		if fileID != "" {
			part, _ = sjson.SetBytes(part, "file_id", fileID)
		}
		if detail := item.Get("image_url.detail").String(); detail != "" {
			part, _ = sjson.SetBytes(part, "detail", detail)
		}
		output, _ = sjson.SetRawBytes(output, "-1", part)
	case "file":
		fileID := item.Get("file.file_id").String()
		fileData := item.Get("file.file_data").String()
		fileURL := item.Get("file.file_url").String()
		if fileID == "" && fileData == "" && fileURL == "" {
			return appendToolOutputFallbackPart(output, item)
		}
		part := []byte(`{}`)
		part, _ = sjson.SetBytes(part, "type", "input_file")
		if fileID != "" {
			part, _ = sjson.SetBytes(part, "file_id", fileID)
		}
		if fileData != "" {
			part, _ = sjson.SetBytes(part, "file_data", fileData)
		}
		if fileURL != "" {
			part, _ = sjson.SetBytes(part, "file_url", fileURL)
		}
		if filename := item.Get("file.filename").String(); filename != "" {
			part, _ = sjson.SetBytes(part, "filename", filename)
		}
		output, _ = sjson.SetRawBytes(output, "-1", part)
	default:
		output = appendToolOutputFallbackPart(output, item)
	}
	return output
}

func appendToolOutputFallbackPart(output []byte, item gjson.Result) []byte {
	text := item.Raw
	if text == "" {
		text = item.String()
	}
	part := []byte(`{}`)
	part, _ = sjson.SetBytes(part, "type", "input_text")
	part, _ = sjson.SetBytes(part, "text", text)
	output, _ = sjson.SetRawBytes(output, "-1", part)
	return output
}

// shortenNameIfNeeded applies the simple shortening rule for a single name.
// If the name length exceeds 64, it will try to preserve the "mcp__" prefix and last segment.
func normalizeCodexReasoningEffort(value string) string {
	if value == "max" {
		return "xhigh"
	}
	return value
}

func shortenNameIfNeeded(name string) string {
	const limit = 64
	if len(name) <= limit {
		return name
	}
	if strings.HasPrefix(name, "mcp__") {
		idx := strings.LastIndex(name, "__")
		if idx > 0 {
			candidate := "mcp__" + name[idx+2:]
			if len(candidate) > limit {
				return candidate[:limit]
			}
			return candidate
		}
	}
	return name[:limit]
}

// buildShortNameMap generates unique short names (<=64) for the given list of names.
func buildShortNameMap(names []string) map[string]string {
	const limit = 64
	used := map[string]struct{}{}
	m := map[string]string{}
	baseCandidate := func(n string) string {
		if len(n) <= limit {
			return n
		}
		if strings.HasPrefix(n, "mcp__") {
			idx := strings.LastIndex(n, "__")
			if idx > 0 {
				cand := "mcp__" + n[idx+2:]
				if len(cand) > limit {
					return cand[:limit]
				}
				return cand
			}
		}
		return n[:limit]
	}
	makeUnique := func(cand string) string {
		if _, ok := used[cand]; !ok {
			return cand
		}
		base := cand
		for i := 1; ; i++ {
			suffix := "_" + strconv.Itoa(i)
			allowed := limit - len(suffix)
			if allowed < 0 {
				allowed = 0
			}
			tmp := base
			if len(tmp) > allowed {
				tmp = tmp[:allowed]
			}
			tmp = tmp + suffix
			if _, ok := used[tmp]; !ok {
				return tmp
			}
		}
	}
	for _, n := range names {
		cand := baseCandidate(n)
		uniq := makeUnique(cand)
		used[uniq] = struct{}{}
		m[n] = uniq
	}
	return m
}
