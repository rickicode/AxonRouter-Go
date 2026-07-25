package executor

import (
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// disableThinkingIfToolChoiceForced removes thinking/output_config.effort when
// the client forces a specific tool choice. Anthropic's API rejects thinking
// when tool_choice.type is "any" or "tool".
// See: https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations
func disableThinkingIfToolChoiceForced(body []byte) []byte {
	toolChoiceType := gjson.GetBytes(body, "tool_choice.type").String()
	if toolChoiceType == "any" || toolChoiceType == "tool" {
		logging.Logger.Warn("claude: disabling thinking because tool_choice forces a specific tool", "tool_choice.type", toolChoiceType)
		body, _ = sjson.DeleteBytes(body, "thinking")
		body, _ = sjson.DeleteBytes(body, "output_config.effort")
		if oc := gjson.GetBytes(body, "output_config"); oc.Exists() && oc.IsObject() && len(oc.Map()) == 0 {
			body, _ = sjson.DeleteBytes(body, "output_config")
		}
	}
	return body
}

// normalizeClaudeSamplingForUpstream removes temperature/top_p and, when
// extended thinking is enabled, also strips top_k/top_p so Anthropic does not
// reject the request.
func normalizeClaudeSamplingForUpstream(body []byte) []byte {
	body, _ = sjson.DeleteBytes(body, "temperature")
	body, _ = sjson.DeleteBytes(body, "top_p")

	thinkingType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	switch thinkingType {
	case "enabled", "adaptive", "auto":
		body, _ = sjson.DeleteBytes(body, "top_p")
		body, _ = sjson.DeleteBytes(body, "top_k")
	}
	return body
}

// ensureClaudeThinkingDisplay defaults thinking.display to "summarized" when
// extended thinking is enabled and the client has not supplied a display value.
// This keeps thinking text visible to clients instead of returning signature-only
// blocks from backends that enable redacted thinking.
func ensureClaudeThinkingDisplay(body []byte) []byte {
	thinkingType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	switch thinkingType {
	case "enabled", "adaptive", "auto":
	default:
		return body
	}
	if display := strings.TrimSpace(gjson.GetBytes(body, "thinking.display").String()); display != "" {
		return body
	}
	out, err := sjson.SetBytes(body, "thinking.display", "summarized")
	if err != nil {
		return body
	}
	return out
}
