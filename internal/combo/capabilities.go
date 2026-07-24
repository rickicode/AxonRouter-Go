package combo

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// hardCapabilities is the set of capabilities that are strictly required for a
// model to be usable for a given request. Soft capabilities are preferred but not
// blocking.
var hardCapabilities = map[string]bool{
	"vision":      true,
	"pdf":         true,
	"audio_input": true,
	"video_input": true,
}

// DetectRequiredCapabilities inspects a request body and returns the set of
// model capabilities required to handle it.
func DetectRequiredCapabilities(body []byte) models.ModelCapabilities {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return models.ModelCapabilities{}
	}
	var caps models.ModelCapabilities

	// Tools are a soft capability (preferred but not blocking).
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		caps.Tools = true
	}

	messages, _ := req["messages"].([]any)
	for _, m := range messages {
		detectMessageCapabilities(m, &caps)
	}

	// Responses API format may have `input` instead of `messages`.
	if input, ok := req["input"].([]any); ok {
		for _, item := range input {
			detectMessageCapabilities(item, &caps)
		}
	}

	// Gemini native format uses top-level `contents` with `parts`.
	if contents, ok := req["contents"].([]any); ok {
		detectGeminiContents(contents, &caps)
	}

	// Antigravity native format nests `contents` under `request`.
	if request, ok := req["request"].(map[string]any); ok {
		if contents, ok := request["contents"].([]any); ok {
			detectGeminiContents(contents, &caps)
		}
	}

	return caps
}

func detectGeminiContents(contents []any, caps *models.ModelCapabilities) {
	for _, item := range contents {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if parts, ok := content["parts"].([]any); ok {
			for _, part := range parts {
				detectGeminiPart(part, caps)
			}
		}
	}
}

func detectGeminiPart(part any, caps *models.ModelCapabilities) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}

	// Gemini parts use inlineData/fileData blobs with mimeType.
	if inlineData, ok := m["inlineData"].(map[string]any); ok {
		detectCapabilityByMimeType(inlineData, caps)
	}
	if fileData, ok := m["fileData"].(map[string]any); ok {
		detectCapabilityByMimeType(fileData, caps)
	}

	// Fallback to OpenAI-style typed parts when present.
	detectContentPart(part, caps)
}

func detectCapabilityByMimeType(m map[string]any, caps *models.ModelCapabilities) {
	mime, _ := m["mimeType"].(string)
	if mime == "" {
		mime, _ = m["mime_type"].(string)
	}
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		caps.Vision = true
	case strings.HasPrefix(mime, "audio/"):
		caps.AudioInput = true
	case strings.HasPrefix(mime, "video/"):
		caps.VideoInput = true
	case strings.Contains(mime, "pdf"):
		caps.PDF = true
	}
}

func detectMessageCapabilities(item any, caps *models.ModelCapabilities) {
	msg, ok := item.(map[string]any)
	if !ok {
		return
	}
	content := msg["content"]
	switch v := content.(type) {
	case string:
		// Text-only; no capability hints.
	case []any:
		for _, part := range v {
			detectContentPart(part, caps)
		}
	case map[string]any:
		detectContentPart(v, caps)
	}
}

func detectContentPart(part any, caps *models.ModelCapabilities) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "image_url", "image", "input_image":
		caps.Vision = true
	case "input_audio", "audio":
		caps.AudioInput = true
	case "video", "input_video":
		caps.VideoInput = true
	case "file", "document", "input_file":
		if isPDFContent(m) {
			caps.PDF = true
		}
	}

	// Responses API input_file/file may wrap data under a "file" sub-object.
	if sub, ok := m["file"].(map[string]any); ok {
		if isPDFContent(sub) {
			caps.PDF = true
		}
	}
}

func isPDFContent(m map[string]any) bool {
	if mime, ok := m["mime_type"].(string); ok && strings.Contains(strings.ToLower(mime), "pdf") {
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

// ReorderStepsByCapabilities returns a new slice of steps sorted so that models
// which satisfy all required capabilities are tried first. Steps that satisfy
// only hard capabilities come next, and the rest come last. The sort is stable
// so the original order is preserved within each tier; no steps are dropped.
func (h *Handler) ReorderStepsByCapabilities(steps []db.ComboStep, required models.ModelCapabilities) []db.ComboStep {
	if !hasAnyRequirement(required) {
		return steps
	}

	typed := make([]struct {
		step db.ComboStep
		tier int
	}, len(steps))
	for i, s := range steps {
		caps := models.GetCapabilities(s.ModelID)
		typed[i] = struct {
			step db.ComboStep
			tier int
		}{step: s, tier: capabilityTier(caps, required)}
	}

	out := make([]db.ComboStep, 0, len(steps))
	for t := 0; t <= 2; t++ {
		for _, item := range typed {
			if item.tier == t {
				out = append(out, item.step)
			}
		}
	}

	// Defensive: if some tier produced nothing, preserve remaining order exactly.
	if len(out) != len(steps) {
		logging.Logger.Warn("capability reorder produced unexpected length; using original order")
		return steps
	}

	return out
}

func hasAnyRequirement(required models.ModelCapabilities) bool {
	return required.Vision || required.PDF || required.AudioInput || required.VideoInput || required.Tools
}

// capabilityTier classifies a model against the required capabilities.
// Tier 0 = all hard + all soft; tier 1 = all hard only; tier 2 = everything else.
func capabilityTier(caps, required models.ModelCapabilities) int {
	hardOk := true
	if required.Vision && !caps.Vision {
		hardOk = false
	}
	if required.PDF && !caps.PDF {
		hardOk = false
	}
	if required.AudioInput && !caps.AudioInput {
		hardOk = false
	}
	if required.VideoInput && !caps.VideoInput {
		hardOk = false
	}
	if !hardOk {
		return 2
	}
	softOk := true
	if required.Tools && !caps.Tools {
		softOk = false
	}
	if softOk {
		return 0
	}
	return 1
}
