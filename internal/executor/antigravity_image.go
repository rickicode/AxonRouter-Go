package executor

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// antigravityImageSuffixes are the aspect-ratio suffixes the 9Router fork strips
// from image-generation model names. Keeping them explicit means unknown trailing
// tokens do not accidentally change upstream routing.
var antigravityImageSuffixes = []string{
	"-widescreen",
	"-portrait",
	"-square",
	"-landscape",
}

// antigravityImageModelPattern recognises image-generation models that carry an
// aspect suffix not in the fixed list above. It captures the upstream model base
// and an optional trailing ratio token.
var antigravityImageModelPattern = regexp.MustCompile(`(?i)^(.*?-image)(?:-([a-z0-9]+))?$`)

// isAntigravityImageModel reports whether the model id should be routed to the
// Antigravity image generation endpoint.
func isAntigravityImageModel(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "-image")
}

// stripAntigravityImageSuffix removes a known aspect-ratio suffix from a model
// name and returns the cleaned upstream model id plus the parsed aspect ratio.
// The search is case-insensitive for the suffix but preserves the case of the
// remaining model id.
func stripAntigravityImageSuffix(modelID string) (upstreamModelID, aspectRatio string) {
	lower := strings.ToLower(modelID)
	for _, suffix := range antigravityImageSuffixes {
		if strings.HasSuffix(lower, suffix) {
			aspectRatio = strings.TrimPrefix(suffix, "-")
			if len(suffix) < len(modelID) {
				upstreamModelID = modelID[:len(modelID)-len(suffix)]
			}
			return upstreamModelID, aspectRatio
		}
	}
	// Unknown trailing token after "-image" is treated as an aspect ratio hint.
	if matches := antigravityImageModelPattern.FindStringSubmatch(modelID); len(matches) == 3 && matches[2] != "" {
		return matches[1], matches[2]
	}
	return modelID, ""
}

// buildAntigravityImageEnvelope transforms a translator-issued Antigravity chat
// envelope into a generateImage-style envelope for image-generation models.
// The input must already contain the outer "request" object. It:
//   - sets requestType to "image_gen"
//   - removes chat-only fields (tools, toolConfig, safetySettings)
//   - flattens generationConfig.imageConfig into request.parameters
//   - collapses contents to a single user turn with the prompt
//   - removes the aspect-ratio suffix from the outer model field
func buildAntigravityImageEnvelope(envelope []byte, upstreamModelID string) ([]byte, error) {
	prompt := ""
	if r := gjson.GetBytes(envelope, "request.contents.0.parts.0.text"); r.Exists() && r.Type == gjson.String {
		prompt = r.String()
	}

	parameters := map[string]any{}
	imgCfg := gjson.GetBytes(envelope, "request.generationConfig.imageConfig")
	if imgCfg.Exists() && imgCfg.IsObject() {
		imgCfg.ForEach(func(key, value gjson.Result) bool {
			parameters[key.String()] = value.Value()
			return true
		})
	}
	if n := gjson.GetBytes(envelope, "request.generationConfig.candidateCount"); n.Exists() && n.Type == gjson.Number {
		parameters["numberOfImages"] = n.Int()
	}

	out := envelope
	if model := gjson.GetBytes(envelope, "model"); model.Exists() && model.String() != "" {
		out, _ = sjson.SetBytes(out, "model", upstreamModelID)
	}
	out, _ = sjson.SetBytes(out, "requestType", "image_gen")
	out, _ = sjson.DeleteBytes(out, "request.generationConfig")
	out, _ = sjson.DeleteBytes(out, "request.toolConfig")
	out, _ = sjson.DeleteBytes(out, "request.tools")
	out, _ = sjson.DeleteBytes(out, "request.safetySettings")

	if prompt != "" {
		contents := []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": prompt}}},
		}
		b, _ := json.Marshal(contents)
		out, _ = sjson.SetRawBytes(out, "request.contents", b)
	}
	if len(parameters) > 0 {
		b, _ := json.Marshal(parameters)
		out, _ = sjson.SetRawBytes(out, "request.parameters", b)
	}
	return out, nil
}

// mapAntigravityImageResponse converts a raw upstream generateImage response
// into the standard Antigravity "response" envelope so the existing translators
// can consume it unchanged.
func mapAntigravityImageResponse(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	// Already wrapped by upstream.
	if gjson.GetBytes(body, "response").Exists() {
		return body
	}

	predictions := gjson.GetBytes(body, "predictions")
	if !predictions.Exists() || !predictions.IsArray() {
		// Some image-gen errors come back as a bare error envelope; let the
		// normal error path handle them.
		return body
	}

	parts := make([]map[string]any, 0)
	predictions.ForEach(func(_, pred gjson.Result) bool {
		if data := pred.Get("bytesBase64Encoded").String(); data != "" {
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": "image/png",
					"data":     data,
				},
			})
			return true
		}
		if inlineData := pred.Get("content.parts.0.inlineData"); inlineData.Exists() {
			var m map[string]any
			if err := json.Unmarshal([]byte(inlineData.Raw), &m); err == nil {
				parts = append(parts, map[string]any{"inlineData": m})
			} else if data := inlineData.Get("data").String(); data != "" {
				parts = append(parts, map[string]any{
					"inlineData": map[string]any{
						"mimeType": inlineData.Get("mimeType").String(),
						"data":     data,
					},
				})
			}
		}
		return true
	})

	if len(parts) == 0 {
		return body
	}

	candidate := map[string]any{
		"content": map[string]any{
			"role":  "model",
			"parts": parts,
		},
	}
	response := map[string]any{
		"candidates": []any{candidate},
	}
	for _, key := range []string{"modelVersion", "responseId", "usageMetadata"} {
		if v := gjson.GetBytes(body, key); v.Exists() {
			response[key] = v.Value()
		}
	}

	wrapped := map[string]any{
		"response": response,
	}
	b, _ := json.Marshal(wrapped)
	return b
}

// antigravityImageURL returns the upstream Antigravity image generation
// endpoint. If no base URL is provided it uses the prod endpoint, mirroring
// antigravityNonStreamURL but pointed at generateImage.
func antigravityImageURL(base string) string {
	if base == "" {
		return "https://cloudcode-pa.googleapis.com/v1internal:generateImage"
	}
	if strings.Contains(base, "/v1internal:generateImage") {
		return base
	}
	if strings.Contains(base, "/v1internal:generateContent") {
		return strings.Replace(base, "/v1internal:generateContent", "/v1internal:generateImage", 1)
	}
	if strings.Contains(base, "/v1internal:streamGenerateContent") {
		return strings.Replace(base, "/v1internal:streamGenerateContent", "/v1internal:generateImage", 1)
	}
	// Any other base path gets the standard image path appended.
	base = strings.TrimSuffix(base, "/")
	return base + "/v1internal:generateImage"
}
