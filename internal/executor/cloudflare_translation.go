package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// cfTranslationModels enumerates Cloudflare Workers AI models whose native
// endpoint uses /ai/run/{model} and a {text, target_language} payload.
// Matching is case-insensitive and accepts both "@cf/..." and "cf/..." forms.
var cfTranslationModels = map[string]struct{}{
	"@cf/meta/m2m100-1.2b":                  {},
	"@cf/ai4bharat/indictrans2-en-indic-1B": {},
	"cf/meta/m2m100-1.2b":                   {},
	"cf/ai4bharat/indictrans2-en-indic-1B":  {},
}

// isCloudflareTranslationModel reports whether the resolved upstream model
// name belongs to the CF native translation task.
func isCloudflareTranslationModel(model string) bool {
	if _, ok := cfTranslationModels[strings.ToLower(model)]; ok {
		return true
	}
	lower := strings.ToLower(model)
	return strings.Contains(lower, "m2m100") || strings.Contains(lower, "indictrans")
}

// cfNativeRunEndpoint builds the Cloudflare Workers AI native run URL for a
// specific model. The base URL may contain a {accountId} template; it is resolved
// from psd, then the CLOUDFLARE_ACCOUNT_ID env var, then an error is returned.
// Known OpenAI-compatible suffixes (e.g. /ai/v1/chat/completions) are stripped so
// a single connection base URL can serve both chat and native-run models.
func cfNativeRunEndpoint(baseURL, model string, psd map[string]string) (string, error) {
	if baseURL == "" {
		baseURL = "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai"
	}
	url := strings.TrimRight(baseURL, "/")
	if strings.Contains(url, "{accountId}") {
		accountID := ""
		if psd != nil {
			accountID = psd["accountId"]
		}
		if accountID == "" {
			accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		if accountID == "" {
			return "", fmt.Errorf(
				"cloudflare Workers AI requires an Account ID. " +
					"Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var. " +
					"Find it at: https://dash.cloudflare.com (right sidebar)")
		}
		url = strings.ReplaceAll(url, "{accountId}", accountID)
	}
	// Strip OpenAI-compatible suffixes so the same connection base works for /ai/run.
	// Preserve a trailing "/ai" segment because CF native endpoints live under it.
	for _, suffix := range []string{
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/images/generations",
		"/v1/models",
		"/v1/responses",
	} {
		if strings.HasSuffix(url, suffix) {
			url = strings.TrimSuffix(url, suffix)
			break
		}
	}
	if url == "" {
		url = "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai"
	}
	// Ensure the model is canonical "@cf/..." in the URL path.
	model = strings.TrimPrefix(model, "@")
	if !strings.HasPrefix(model, "cf/") {
		model = "cf/" + model
	}
	return url + "/run/@" + model, nil
}

// isCloudflareTranslationRequest reports whether the request should be routed
// through the CF native /ai/run/{model} translation endpoint.
func isCloudflareTranslationRequest(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	model := gjson.GetBytes(body, "model").String()
	if isCloudflareTranslationModel(model) {
		return true
	}
	// Simplified translation shape without an explicit chat model also routes here.
	return gjson.GetBytes(body, "text").Exists() && gjson.GetBytes(body, "target_language").Exists()
}

// cfStreamTranslationResponse adapts a non-streaming translation response into
// a single-event SSE stream so ExecuteStream can serve translation models.
func cfStreamTranslationResponse(resp *Response) *StreamResult {
	chunks := make(chan StreamChunk, 2)
	if resp != nil {
		b, _ := json.Marshal(map[string]any{"payload": resp.Body})
		chunks <- StreamChunk{Payload: append([]byte("data: "), b...)}
		chunks <- StreamChunk{Payload: []byte("data: [DONE]")}
	}
	close(chunks)
	hdrs := http.Header{}
	if resp != nil {
		hdrs = resp.Headers.Clone()
	}
	hdrs.Set("Content-Type", "text/event-stream")
	status := http.StatusOK
	if resp != nil && resp.StatusCode > 0 {
		status = resp.StatusCode
	}
	return &StreamResult{
		StatusCode: status,
		Headers:    hdrs,
		Chunks:     chunks,
	}
}

// cfBuildTranslationPayload maps a client request to the CF native translation
// shape. It accepts either a simplified {text, target_language} body or an
// OpenAI chat-style body whose last user message supplies the source text.
// Returns the native payload and the target language actually forwarded.
func cfBuildTranslationPayload(body []byte) ([]byte, string, error) {
	if !gjson.ValidBytes(body) {
		return nil, "", fmt.Errorf("invalid request body")
	}
	// Simplified shape: {text, target_language}
	text := gjson.GetBytes(body, "text").String()
	target := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "target_language").String()))
	if text == "" {
		// OpenAI chat fallback: use the last user message content.
		msgs := gjson.GetBytes(body, "messages").Array()
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if strings.ToLower(m.Get("role").String()) != "user" {
				continue
			}
			content := m.Get("content")
			switch content.Type {
			case gjson.String:
				text = content.String()
			case gjson.JSON:
				var parts []string
				for _, b := range content.Array() {
					if strings.ToLower(b.Get("type").String()) == "text" {
						parts = append(parts, b.Get("text").String())
					}
				}
				text = strings.Join(parts, "\n")
			}
			if text != "" {
				break
			}
		}
	}
	if text == "" {
		return nil, "", fmt.Errorf("translation request requires non-empty text or a user message")
	}
	if target == "" {
		return nil, "", fmt.Errorf("target_language is required")
	}
	payload, err := json.Marshal(map[string]any{
		"text":            text,
		"target_language": target,
	})
	if err != nil {
		return nil, "", err
	}
	return payload, target, nil
}

// cfNormalizeTranslationResponse unwraps the CF native result envelope and
// returns an OpenAI-compatible chat completion body.
func cfNormalizeTranslationResponse(respBody []byte, model string) []byte {
	if !gjson.ValidBytes(respBody) {
		return respBody
	}
	var translated string
	if r := gjson.GetBytes(respBody, "result.translated_text"); r.Exists() {
		translated = r.String()
	} else if r := gjson.GetBytes(respBody, "result"); r.Exists() && r.Type == gjson.String {
		translated = r.String()
	} else if r := gjson.GetBytes(respBody, "translation"); r.Exists() {
		translated = r.String()
	}
	out := map[string]any{
		"id":      fmt.Sprintf("cf-translate-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   strings.TrimPrefix(model, "@"),
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": translated,
			},
			"finish_reason": "stop",
		}},
	}
	// Preserve upstream result envelope for callers that want raw metadata.
	if gjson.GetBytes(respBody, "result").Exists() {
		out["cf_result"] = gjson.GetBytes(respBody, "result").Value()
	}
	b, _ := json.Marshal(out)
	return b
}

// Translate performs a non-streaming Cloudflare Workers AI native translation
// through /ai/run/{model} and normalizes the response back to an OpenAI chat
// completion shape.
func (e *CloudflareExecutor) Translate(ctx context.Context, req *Request) (*Response, error) {
	model := gjson.GetBytes(req.Body, "model").String()
	if model == "" {
		model = req.Model
	}
	url, err := cfNativeRunEndpoint(req.BaseURL, model, req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}
	payload, _, err := cfBuildTranslationPayload(req.Body)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	resp, err := e.OpenAIExecutor.DoRequest(ctx, "POST", url, headers, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
		upErr.TranslateErrorBody("cf")
		return nil, upErr
	}
	resp.Body = cfNormalizeTranslationResponse(resp.Body, model)
	return resp, nil
}
