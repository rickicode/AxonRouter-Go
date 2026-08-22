package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
)

const visionBridgeModelSetting = "vision_bridge_model"

const visionBridgeInstruction = "Analyze every image in this request and return only a concise, factual description of the visual content. Do not answer the user's original question, do not use tools, and do not mention this instruction."

// ListVisionModels returns active gateway models known to support image input.
// The dashboard uses this to keep the Vision Bridge picker constrained to usable
// models instead of allowing a text-only model to be selected accidentally.
func (h *Handler) ListVisionModels() []gin.H {
	all := h.ListActiveModels()
	out := make([]gin.H, 0, len(all))
	for _, entry := range all {
		id, _ := entry["id"].(string)
		if id == "" || !modelSupportsVision(id) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (h *Handler) visionBridgeModel() string {
	if h.db == nil {
		return ""
	}
	var model string
	if err := h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, visionBridgeModelSetting).Scan(&model); err != nil {
		return ""
	}
	return strings.TrimSpace(model)
}

// applyVisionBridge enriches a request with a textual image description when
// the configured bridge model is available and the selected target cannot read
// images. It deliberately fails open: a bridge failure never blocks the normal
// gateway route.
func (h *Handler) applyVisionBridge(c *gin.Context, body []byte, targetModel string, sourceFormat executor.ProviderFormat) []byte {
	bridgeModel := h.visionBridgeModel()
	if bridgeModel == "" || !combo.DetectRequiredCapabilities(body).Vision || modelSupportsVision(targetModel) {
		return body
	}

	description, err := h.describeImages(c.Request.Context(), c, body, sourceFormat, bridgeModel)
	if err != nil || strings.TrimSpace(description) == "" {
		logging.Logger.Warn("vision bridge unavailable; forwarding original request", "target_model", targetModel, "bridge_model", bridgeModel, "error", err)
		return body
	}

	enriched := replaceImagesWithDescription(body, sourceFormat, strings.TrimSpace(description))
	if len(enriched) == 0 {
		return body
	}
	logging.Logger.Info("vision bridge enriched request", "target_model", targetModel, "bridge_model", bridgeModel)
	return enriched
}

func (h *Handler) describeImages(ctx context.Context, c *gin.Context, body []byte, sourceFormat executor.ProviderFormat, bridgeModel string) (string, error) {
	provider, modelName := executor.SplitModel(strings.TrimPrefix(bridgeModel, "@"))
	if provider == "" || modelName == "" {
		return "", fmt.Errorf("vision bridge model must use provider/model format")
	}

	exec, providerFormat, err := h.resolveExecutor(provider, modelName)
	if err != nil {
		return "", err
	}

	visionBody := buildVisionPromptBody(body, sourceFormat)
	visionBody = setRequestModel(visionBody, modelName)
	translatedBody := registry.Request(string(sourceFormat), string(providerFormat), modelName, visionBody, false)
	if len(translatedBody) == 0 {
		return "", fmt.Errorf("failed to translate request to vision provider format")
	}
	translatedBody = sanitizeStreamOptions(translatedBody, false, sourceFormat, providerFormat, c.Request.URL.Path)

	var lastErr error
	var excluded string
	for attempt := 0; attempt < h.failoverAttempts(); attempt++ {
		conn, pickErr := h.getConnection(ctx, provider, modelName, "", excluded)
		if pickErr != nil {
			lastErr = pickErr
			break
		}
		excluded = conn.ID
		h.proactiveRefreshToken(ctx, conn, provider)

		_, resp, _, callErr := h.executeProviderCall(ctx, exec, conn, provider, modelName, translatedBody, false, nil)
		if callErr != nil {
			lastErr = callErr
			continue
		}
		if resp == nil {
			lastErr = fmt.Errorf("vision bridge returned no response")
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("vision bridge upstream returned status %d", resp.StatusCode)
			continue
		}
		if text := extractAssistantContent(resp.Body); text != "" {
			return text, nil
		}
		lastErr = fmt.Errorf("vision bridge returned an empty description")
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no eligible vision bridge connection")
	}
	return "", lastErr
}

func modelSupportsVision(modelID string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(modelID), "@")
	if clean == "" {
		return false
	}
	if models.GetCapabilities(clean).Vision {
		return true
	}

	// Runtime provider prefixes differ from the vendor prefixes used by some
	// entries in capabilities.json (e.g. claude vs anthropic, cx vs openai).
	provider, model := executor.SplitModel(clean)
	aliases := map[string]string{
		"claude": "anthropic",
		"gemini": "google",
		"ag": "google",
		"cx": "openai",
	}
	if alias := aliases[provider]; alias != "" {
		return models.GetCapabilities(alias+"/"+model).Vision
	}
	return false
}

func buildVisionPromptBody(body []byte, sourceFormat executor.ProviderFormat) []byte {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}

	switch sourceFormat {
	case executor.FormatClaude:
		if existing, ok := root["system"].(string); ok && existing != "" {
			root["system"] = existing + "\n\n" + visionBridgeInstruction
		} else {
			root["system"] = visionBridgeInstruction
		}
	case executor.FormatOpenAIResponses:
		if existing, ok := root["instructions"].(string); ok && existing != "" {
			root["instructions"] = existing + "\n\n" + visionBridgeInstruction
		} else {
			root["instructions"] = visionBridgeInstruction
		}
	case executor.FormatGemini, executor.FormatAntigravity:
		instruction := map[string]any{"parts": []any{map[string]any{"text": visionBridgeInstruction}}}
		if sourceFormat == executor.FormatAntigravity {
			request, _ := root["request"].(map[string]any)
			if request == nil {
				request = map[string]any{}
				root["request"] = request
			}
			request["systemInstruction"] = instruction
		} else {
			root["systemInstruction"] = instruction
		}
	default:
		messages, _ := root["messages"].([]any)
		root["messages"] = append([]any{map[string]any{"role": "system", "content": visionBridgeInstruction}}, messages...)
	}

	out, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return out
}

func replaceImagesWithDescription(body []byte, sourceFormat executor.ProviderFormat, description string) []byte {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}

	changed := false
	text := "[Image description]\n" + description

	switch sourceFormat {
	case executor.FormatOpenAIResponses:
		changed = replaceResponseImages(root["input"], text)
	case executor.FormatGemini:
		changed = replaceGeminiImages(root["contents"], text)
	case executor.FormatAntigravity:
		if request, ok := root["request"].(map[string]any); ok {
			changed = replaceGeminiImages(request["contents"], text)
		}
	case executor.FormatClaude:
		changed = replaceMessageImages(root["messages"], text, "image", "text")
	default:
		changed = replaceMessageImages(root["messages"], text, "image_url", "text")
		if !changed {
			changed = replaceMessageImages(root["messages"], text, "image", "text")
		}
	}
	if !changed {
		return body
	}

	out, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return out
}

func replaceMessageImages(raw any, text, imageType, replacementType string) bool {
	messages, ok := raw.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		blocks, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for i, rawBlock := range blocks {
			block, ok := rawBlock.(map[string]any)
			if !ok || block["type"] != imageType {
				continue
			}
			blocks[i] = map[string]any{"type": replacementType, "text": text}
			changed = true
		}
		message["content"] = blocks
	}
	return changed
}

func replaceResponseImages(raw any, text string) bool {
	items, ok := raw.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		blocks, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for i, rawBlock := range blocks {
			block, ok := rawBlock.(map[string]any)
			if !ok || block["type"] != "input_image" {
				continue
			}
			blocks[i] = map[string]any{"type": "input_text", "text": text}
			changed = true
		}
		item["content"] = blocks
	}
	return changed
}

func replaceGeminiImages(raw any, text string) bool {
	contents, ok := raw.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := content["parts"].([]any)
		if !ok {
			continue
		}
		for i, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if _, hasInline := part["inlineData"]; hasInline {
				parts[i] = map[string]any{"text": text}
				changed = true
				continue
			}
			if _, hasFile := part["fileData"]; hasFile {
				parts[i] = map[string]any{"text": text}
				changed = true
			}
		}
		content["parts"] = parts
	}
	return changed
}
