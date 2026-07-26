package v1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertInteractionsToGemini converts an interactions-shaped request body into
// a native Gemini generateContent request body.
func convertInteractionsToGemini(modelName string, input []byte) []byte {
	root := gjson.ParseBytes(input)
	out := []byte(`{"model":"","contents":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	if sys := interactionsSystemInstruction(root); sys != "" {
		out, _ = sjson.SetBytes(out, "systemInstruction", sys)
	}
	if gc := root.Get("generation_config"); gc.Exists() {
		converted := convertJSONKeysToCamel([]byte(gc.Raw))
		out, _ = sjson.SetRawBytes(out, "generationConfig", converted)
	}
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		out, _ = sjson.SetRawBytes(out, "tools", []byte(tools.Raw))
	}

	contents := interactionsInputToContents(root.Get("input"))
	out, _ = sjson.SetRawBytes(out, "contents", contents)
	return out
}

// interactionsSystemInstruction extracts a string system instruction from an
// interactions request. It supports either a top-level string or an object
// with a "text" field.
func interactionsSystemInstruction(root gjson.Result) string {
	sys := root.Get("system_instruction")
	if !sys.Exists() {
		return ""
	}
	if sys.Type == gjson.String {
		return sys.String()
	}
	if text := sys.Get("text"); text.Exists() && text.Type == gjson.String {
		return text.String()
	}
	var parts []string
	sys.Get("content").ForEach(func(_, part gjson.Result) bool {
		if t := part.Get("text"); t.Exists() {
			parts = append(parts, t.String())
		}
		return true
	})
	return strings.Join(parts, "\n")
}

// interactionsInputToContents converts the interactions "input" field into
// Gemini contents. It accepts a string, an array of strings, or an array of
// step objects with a "content" array or string.
func interactionsInputToContents(input gjson.Result) []byte {
	switch {
	case input.Type == gjson.String:
		obj := map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"text": input.String()}},
		}
		b, _ := json.Marshal([]map[string]any{obj})
		return b
	case input.IsArray():
		var contents []map[string]any
		input.ForEach(func(_, item gjson.Result) bool {
			contents = append(contents, interactionsItemToContent(item))
			return true
		})
		b, _ := json.Marshal(contents)
		return b
	default:
		return []byte("[]")
	}
}

// interactionsItemToContent converts a single interactions input item to a
// Gemini content object. String items become user content; objects are inspected
// for role, type, and content.
func interactionsItemToContent(item gjson.Result) map[string]any {
	if item.Type == gjson.String {
		return map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"text": item.String()}},
		}
	}

	role := "user"
	if t := item.Get("type").String(); t == "model_output" || t == "assistant" {
		role = "model"
	}
	if r := item.Get("role").String(); r == "model" || r == "assistant" {
		role = "model"
	} else if r == "system" {
		role = "user"
	}

	var parts []map[string]any
	content := item.Get("content")
	if content.Type == gjson.String {
		parts = append(parts, map[string]any{"text": content.String()})
	} else if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text"); text.Exists() {
				parts = append(parts, map[string]any{"text": text.String()})
			} else if text := part.Get("content"); text.Exists() && text.Type == gjson.String {
				parts = append(parts, map[string]any{"text": text.String()})
			}
			return true
		})
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]any{"text": ""})
	}
	return map[string]any{"role": role, "parts": parts}
}

// convertJSONKeysToCamel recursively renames object keys from snake_case to
// camelCase. Values are preserved.
func convertJSONKeysToCamel(in []byte) []byte {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return in
	}
	out, err := json.Marshal(toCamelCaseKeys(v))
	if err != nil {
		return in
	}
	return out
}

// toCamelCaseKeys converts map keys to camelCase and recurses into values.
func toCamelCaseKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[snakeToCamel(k)] = toCamelCaseKeys(val)
		}
		return m
	case []any:
		arr := make([]any, len(x))
		for i, val := range x {
			arr[i] = toCamelCaseKeys(val)
		}
		return arr
	default:
		return v
	}
}

// snakeToCamel converts a snake_case identifier to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		out += strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return out
}

// convertGeminiResponseToInteractions turns a native Gemini generateContent
// response into a minimal interactions-shaped response.
func convertGeminiResponseToInteractions(modelName string, body []byte) map[string]any {
	root := gjson.ParseBytes(body)

	var steps []map[string]any
	root.Get("candidates").ForEach(func(_, cand gjson.Result) bool {
		var texts []string
		cand.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text"); t.Exists() {
				texts = append(texts, t.String())
			}
			return true
		})
		if len(texts) > 0 {
			content := []map[string]any{{"type": "text", "text": strings.Join(texts, "")}}
			steps = append(steps, map[string]any{"type": "model_output", "content": content})
		}
		return true
	})

	usage := map[string]any{}
	if u := root.Get("usageMetadata"); u.Exists() {
		if n := u.Get("promptTokenCount").Int(); n > 0 {
			usage["input_tokens"] = n
		}
		if n := u.Get("candidatesTokenCount").Int(); n > 0 || u.Get("candidatesTokenCount").Exists() {
			usage["output_tokens"] = n
		}
		if n := u.Get("totalTokenCount").Int(); n > 0 {
			usage["total_tokens"] = n
		}
		if n := u.Get("cachedContentTokenCount").Int(); n > 0 {
			usage["cached_tokens"] = n
		}
		if n := u.Get("thoughtsTokenCount").Int(); n > 0 {
			usage["reasoning_tokens"] = n
		}
	}

	resp := map[string]any{
		"object":   "interaction",
		"id":       fmt.Sprintf("interaction-%d", time.Now().UnixMilli()),
		"status":   "completed",
		"model":    "models/" + modelName,
		"steps":    steps,
		"usage":    usage,
		"response": map[string]any{"modelVersion": root.Get("modelVersion").String()},
	}
	return resp
}
