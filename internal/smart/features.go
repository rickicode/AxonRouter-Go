package smart

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

var codeFenceRe = regexp.MustCompile("```\\s*([a-zA-Z0-9_+-]+)")

// ExtractFeatures builds a feature vector from a JSON request body.
func ExtractFeatures(body []byte) FeatureVector {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return FeatureVector{}
	}

	fv := FeatureVector{
		TotalTokens: usage.EstimateTokensFromRequest(body),
	}

	if msgs, ok := req["messages"].([]any); ok {
		fv.MessageCount = len(msgs)
		for _, m := range msgs {
			detectMessageFeatures(m, &fv)
		}
	}

	if input, ok := req["input"].([]any); ok {
		for _, item := range input {
			detectMessageFeatures(item, &fv)
		}
	} else if inputStr, ok := req["input"].(string); ok && inputStr != "" && fv.MessageCount == 0 {
		fv.MessageCount = 1
	}

	if contents, ok := req["contents"].([]any); ok {
		for _, item := range contents {
			detectGeminiContent(item, &fv)
		}
	}

	if request, ok := req["request"].(map[string]any); ok {
		if contents, ok := request["contents"].([]any); ok {
			for _, item := range contents {
				detectGeminiContent(item, &fv)
			}
		}
	}

	if tools, ok := req["tools"].([]any); ok {
		fv.ToolCount = len(tools)
	}

	if reasoning, ok := req["reasoning"].(map[string]any); ok {
		fv.Reasoning = true
		if effort, ok := reasoning["effort"].(string); ok {
			fv.ReasoningEffort = strings.ToLower(strings.TrimSpace(effort))
		}
	} else if effort, ok := req["reasoning_effort"].(string); ok {
		fv.Reasoning = true
		fv.ReasoningEffort = strings.ToLower(strings.TrimSpace(effort))
	}

	// Detect code language hints without counting messages multiple times.
	scanForCodeHint(req, &fv)

	return fv
}

func detectMessageFeatures(item any, fv *FeatureVector) {
	msg, ok := item.(map[string]any)
	if !ok {
		return
	}
	content := msg["content"]
	detectContentFeatures(content, fv)

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		fv.ToolCallCount += len(toolCalls)
	}
}

func detectContentFeatures(content any, fv *FeatureVector) {
	switch v := content.(type) {
	case string:
		fv.scanTextFeatures(v)
	case []any:
		for _, part := range v {
			detectPartFeatures(part, fv)
		}
	case map[string]any:
		detectPartFeatures(v, fv)
	}
}

func detectPartFeatures(part any, fv *FeatureVector) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "image_url", "image", "input_image":
		fv.HasImages = true
	case "input_audio", "audio":
		fv.HasAudio = true
	case "video", "input_video":
		fv.HasVideo = true
	case "file", "document", "input_file":
		if isPDFMap(m) {
			fv.HasPDF = true
		}
	}
	if sub, ok := m["file"].(map[string]any); ok && isPDFMap(sub) {
		fv.HasPDF = true
	}
	if text, ok := m["text"].(string); ok && text != "" {
		fv.scanTextFeatures(text)
	}
}

func detectGeminiContent(item any, fv *FeatureVector) {
	content, ok := item.(map[string]any)
	if !ok {
		return
	}
	if parts, ok := content["parts"].([]any); ok {
		for _, part := range parts {
			detectGeminiPart(part, fv)
		}
	}
}

func detectGeminiPart(part any, fv *FeatureVector) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}
	if inlineData, ok := m["inlineData"].(map[string]any); ok {
		detectMimeCapabilities(inlineData, fv)
	}
	if fileData, ok := m["fileData"].(map[string]any); ok {
		detectMimeCapabilities(fileData, fv)
	}
	if text, ok := m["text"].(string); ok {
		fv.scanTextFeatures(text)
	}
}

func detectMimeCapabilities(m map[string]any, fv *FeatureVector) {
	mime, _ := m["mimeType"].(string)
	if mime == "" {
		mime, _ = m["mime_type"].(string)
	}
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		fv.HasImages = true
	case strings.HasPrefix(mime, "audio/"):
		fv.HasAudio = true
	case strings.HasPrefix(mime, "video/"):
		fv.HasVideo = true
	case strings.Contains(mime, "pdf"):
		fv.HasPDF = true
	}
}

func isPDFMap(m map[string]any) bool {
	if mime, ok := m["mime_type"].(string); ok && strings.Contains(strings.ToLower(mime), "pdf") {
		return true
	}
	if mime, ok := m["mimeType"].(string); ok && strings.Contains(strings.ToLower(mime), "pdf") {
		return true
	}
	if fname, ok := m["file_name"].(string); ok && strings.HasSuffix(strings.ToLower(fname), ".pdf") {
		return true
	}
	if fname, ok := m["filename"].(string); ok && strings.HasSuffix(strings.ToLower(fname), ".pdf") {
		return true
	}
	return false
}

func (fv *FeatureVector) scanTextFeatures(text string) {
	matches := codeFenceRe.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			fv.CodeHint = true
			if fv.CodeLanguage == "" {
				fv.CodeLanguage = strings.ToLower(m[1])
			}
		}
	}
}

func scanForCodeHint(req map[string]any, fv *FeatureVector) {
	if fv.CodeHint {
		return
	}
	for _, msgs := range []any{req["messages"], req["input"], req["contents"]} {
		arr, ok := msgs.([]any)
		if !ok {
			continue
		}
		for _, item := range arr {
			msg, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if content, ok := msg["content"].(string); ok {
				fv.scanTextFeatures(content)
			}
		}
	}
}
