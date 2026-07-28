package claude

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/headroom"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/signature"
	"github.com/rickicode/AxonRouter-Go/internal/thinking"
	"github.com/rickicode/AxonRouter-Go/internal/translator/common"
	"github.com/tidwall/gjson"
)

// Claude tool cloaking suffix (matches OmniRoute anti-ban _cc suffix).
const claudeToolSuffix = "_cc"

func cloakClaudeToolName(name string) string {
	if name == "" {
		return name
	}
	if len(name) > 3 && name[len(name)-3:] == claudeToolSuffix {
		return name
	}
	return name + claudeToolSuffix
}

// UncloakClaudeToolName removes the _cc suffix from a tool name.
func UncloakClaudeToolName(name string) string {
	if len(name) > 3 && name[len(name)-3:] == claudeToolSuffix {
		return name[:len(name)-3]
	}
	return name
}

// convertOpenAIRequestToClaude converts an OpenAI Chat Completions request to Anthropic Messages format.
func convertOpenAIRequestToClaude(modelName string, body []byte, stream bool) []byte {
	body = common.CompressToolBlocks(body, headroom.GlobalToolCompressor(), headroom.DefaultToolThreshold)
	root := gjson.ParseBytes(body)

	out := make(map[string]interface{})
	out["model"] = modelName

	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out["max_tokens"] = maxTokens.Int()
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out["temperature"] = temp.Float()
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out["top_p"] = topP.Float()
	}
	out["stream"] = stream

	// Stop sequences
	if stop := root.Get("stop"); stop.Exists() && stop.IsArray() {
		var stops []string
		stop.ForEach(func(_, v gjson.Result) bool {
			stops = append(stops, v.String())
			return true
		})
		if len(stops) > 0 {
			out["stop_sequences"] = stops
		}
	}

	// Convert OpenAI reasoning_effort to Claude thinking config.
	if re := root.Get("reasoning_effort"); re.Exists() && re.Type == gjson.String {
		effort := strings.ToLower(strings.TrimSpace(re.String()))
		if effort != "" {
			applyReasoningEffort(out, modelName, effort)
		}
	}

	// System message extraction with cache_control preservation.
	var systemBlocks []map[string]interface{}
	if sys := root.Get("messages"); sys.Exists() && sys.IsArray() {
		sys.ForEach(func(_, msg gjson.Result) bool {
			if msg.Get("role").String() != "system" {
				return true
			}
			start := len(systemBlocks)
			if c := msg.Get("content"); c.Exists() {
				if c.Type == gjson.String && c.String() != "" {
					block := map[string]interface{}{
						"type": "text",
						"text": c.String(),
					}
					common.CopyCacheControlToMap(block, msg)
					systemBlocks = append(systemBlocks, block)
				} else if c.IsArray() {
					c.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "text" {
							block := map[string]interface{}{
								"type": "text",
								"text": part.Get("text").String(),
							}
							common.CopyCacheControlToMap(block, part)
							systemBlocks = append(systemBlocks, block)
						}
						return true
					})
					// Message-level cache_control applies to the last block from this message.
					if len(systemBlocks) > start {
						last := systemBlocks[len(systemBlocks)-1]
						if _, ok := last["cache_control"]; !ok {
							common.CopyCacheControlToMap(last, msg)
						}
					}
				}
			}
			return true
		})
	}
	if len(systemBlocks) > 0 {
		out["system"] = systemBlocks
	}

	// Messages: filter out system, convert content blocks
	var messages []map[string]interface{}
	if msgs := root.Get("messages"); msgs.Exists() && msgs.IsArray() {
		msgs.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			if role == "system" {
				return true // skip, already extracted
			}

			claudeMsg := map[string]interface{}{
				"role": role,
			}

			if content := msg.Get("content"); content.Exists() {
				if content.Type == gjson.String {
					claudeMsg["content"] = content.String()
				} else if content.IsArray() {
					var parts []map[string]interface{}
					content.ForEach(func(_, part gjson.Result) bool {
						pType := part.Get("type").String()
						switch pType {
						case "text":
							block := map[string]interface{}{
								"type": "text",
								"text": part.Get("text").String(),
							}
							common.CopyCacheControlToMap(block, part)
							parts = append(parts, block)
						case "image_url":
							if url := part.Get("image_url.url"); url.Exists() {
								block := map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type": "url",
										"url":  url.String(),
									},
								}
								common.CopyCacheControlToMap(block, part)
								parts = append(parts, block)
							}
						case "tool_use":
							toolUse := map[string]interface{}{
								"type":  "tool_use",
								"id":    part.Get("id").String(),
								"name":  part.Get("name").String(),
								"input": json.RawMessage(part.Get("input").Raw),
							}
							common.CopyCacheControlToMap(toolUse, part)
							parts = append(parts, toolUse)
						case "tool_result":
							toolResult := map[string]interface{}{
								"type":        "tool_result",
								"tool_use_id": part.Get("tool_use_id").String(),
							}
							if c := part.Get("content"); c.Exists() {
								toolResult["content"] = c.String()
							}
							common.CopyCacheControlToMap(toolResult, part)
							parts = append(parts, toolResult)
						}
						return true
					})
					if len(parts) > 0 {
						claudeMsg["content"] = parts
					}
				}
			}

			// Tool calls (assistant message)
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				var contentParts []map[string]interface{}
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					contentParts = append(contentParts, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.Get("id").String(),
						"name":  cloakClaudeToolName(tc.Get("function.name").String()),
						"input": json.RawMessage(tc.Get("function.arguments").String()),
					})
					return true
				})
				if existing, ok := claudeMsg["content"].([]map[string]interface{}); ok {
					claudeMsg["content"] = append(existing, contentParts...)
				} else if s, ok := claudeMsg["content"].(string); ok && s != "" {
					claudeMsg["content"] = append([]map[string]interface{}{
						{"type": "text", "text": s},
					}, contentParts...)
				} else {
					claudeMsg["content"] = contentParts
				}
			}

			// Preserve message-level cache_control on the last content block.
			common.AttachMessageCacheControlToMap(claudeMsg, msg)

			messages = append(messages, claudeMsg)
			return true
		})
	}

	out["messages"] = messages

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var claudeTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			claudeTool := map[string]interface{}{
				"name": cloakClaudeToolName(tool.Get("function.name").String()),
			}
			if desc := tool.Get("function.description"); desc.Exists() {
				claudeTool["description"] = desc.String()
			}
			if params := tool.Get("function.parameters"); params.Exists() {
				claudeTool["input_schema"] = json.RawMessage(params.Raw)
			}
			if copied := common.CopyCacheControlToMap(claudeTool, tool); !copied {
				common.CopyCacheControlToMap(claudeTool, tool.Get("function"))
			}
			claudeTools = append(claudeTools, claudeTool)
			return true
		})
		if len(claudeTools) > 0 {
			out["tools"] = claudeTools
		}
	}

	// Tool choice mapping from OpenAI format to Claude Code format.
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		switch toolChoice.Type {
		case gjson.String:
			switch toolChoice.String() {
			case "none":
				// Leave unset; Claude will not use tools by default.
			case "auto":
				out["tool_choice"] = map[string]interface{}{"type": "auto"}
			case "required":
				out["tool_choice"] = map[string]interface{}{"type": "any"}
			}
		case gjson.JSON:
			if toolChoice.Get("type").String() == "function" {
				functionName := toolChoice.Get("function.name").String()
				if functionName != "" {
					out["tool_choice"] = map[string]interface{}{
						"type": "tool",
						"name": cloakClaudeToolName(functionName),
					}
				}
			}
		}
	}

	result, _ := json.Marshal(out)
	result, _ = signature.SanitizeClaudeMessagesForClaudeUpstream(result, modelName)
	return result
}

// applyReasoningEffort maps an OpenAI reasoning_effort value onto Claude's
// thinking config. Adaptive thinking (output_config.effort) is used when the
// model advertises thinking Levels in the catalog; otherwise the legacy
// budget_tokens form is used. Matches CLIProxyAPI's chat-completions→Claude
// reasoning mapping.
func applyReasoningEffort(out map[string]interface{}, modelName, effort string) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return
	}

	levels := models.GetModelThinkingLevels(modelName)
	supportsAdaptive := len(levels) > 0
	supportsMax := supportsAdaptive && sliceContains(levels, "max")

	if supportsAdaptive {
		switch effort {
		case "none":
			out["thinking"] = map[string]interface{}{"type": "disabled"}
		case "auto":
			out["thinking"] = map[string]interface{}{"type": "adaptive"}
		default:
			mapped, ok := mapToClaudeEffort(effort, supportsMax)
			if !ok {
				return
			}
			out["thinking"] = map[string]interface{}{"type": "adaptive"}
			out["output_config"] = map[string]interface{}{"effort": mapped}
		}
		return
	}

	// Legacy/manual thinking via budget_tokens.
	budget, ok := thinking.BudgetFromString(effort)
	if !ok {
		return
	}
	budget = thinking.ClampBudget("claude", budget)
	switch {
	case budget == thinking.BudgetDisabled:
		out["thinking"] = map[string]interface{}{"type": "disabled"}
	case budget == thinking.BudgetAuto:
		out["thinking"] = map[string]interface{}{"type": "enabled"}
	case budget > 0:
		out["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": budget,
		}
	}
}

// mapToClaudeEffort maps a generic reasoning_effort level to a Claude adaptive
// thinking effort value. It mirrors CLIProxyAPI's internal/thinking.convert.go.
func mapToClaudeEffort(level string, supportsMax bool) (string, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "":
		return "", false
	case "minimal":
		return "low", true
	case "low", "medium", "high":
		return level, true
	case "xhigh", "max":
		if supportsMax {
			return "max", true
		}
		return "high", true
	case "auto":
		return "high", true
	default:
		return "", false
	}
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(strings.TrimSpace(s), needle) {
			return true
		}
	}
	return false
}
