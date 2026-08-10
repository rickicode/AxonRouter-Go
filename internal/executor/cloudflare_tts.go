package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
)

// CloudflareTTSExecutor handles Cloudflare Workers AI text-to-speech by
// translating an OpenAI-style /v1/audio/speech request to the native
// /ai/run/{model} endpoint.
type CloudflareTTSExecutor struct {
	*BaseExecutor
}

// NewCloudflareTTSExecutor creates a new Cloudflare TTS executor.
func NewCloudflareTTSExecutor(base *BaseExecutor) *CloudflareTTSExecutor {
	return &CloudflareTTSExecutor{BaseExecutor: base}
}

// Execute translates an OpenAI TTS request and returns audio bytes.
func (e *CloudflareTTSExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url, err := cloudflareRunURL(req.BaseURL, req.Model, req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}
	body, contentType, accept, err := buildCloudflareTTSPayload(req.Body, req.Model)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": contentType,
		"Accept":       accept,
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody("cf")
		return nil, upErr
	}
	cfResp := cloudflareNormalizeAudioResponse(resp)
	return cfResp, nil
}

// ExecuteStream is not supported for TTS.
func (e *CloudflareTTSExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("TTS does not support streaming")
}

// buildCloudflareTTSPayload converts a request body like
// {"input":"hello","voice":"alloy","response_format":"mp3"} into a CF native
// payload. MeloTTS uses {"prompt":"...","lang":"..."}; Aura-2 uses
// {"text":"...","speaker":"...",...}.
func buildCloudflareTTSPayload(body []byte, model string) (payload []byte, contentType, accept string, err error) {
	input := gjson.GetBytes(body, "input").String()
	voice := gjson.GetBytes(body, "voice").String()
	responseFormat := gjson.GetBytes(body, "response_format").String()
	if responseFormat == "" {
		responseFormat = "mp3"
	}

	fullModel := normalizeModelName(model, providercfg.CompatibilityFor("cf"))

	switch {
	case strings.HasSuffix(fullModel, "/melotts"):
		lang := voice
		if lang == "" || !isLanguageCode(lang) {
			lang = "en"
		}
		payload, err = json.Marshal(map[string]any{
			"prompt": input,
			"lang":   lang,
		})
		if err != nil {
			return nil, "", "", err
		}
		return payload, "application/json", "audio/mpeg", nil

	default:
		m := map[string]any{
			"text": input,
		}
		if voice != "" {
			m["speaker"] = voice
		}
		payload, err = json.Marshal(m)
		if err != nil {
			return nil, "", "", err
		}
		return payload, "application/json", cloudflareTTSAcceptHeader(responseFormat), nil
	}
}

func isLanguageCode(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func cloudflareTTSAcceptHeader(format string) string {
	switch format {
	case "mp3", "mpeg":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg", "oga":
		return "audio/ogg"
	case "webm":
		return "audio/webm"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/opus"
	case "pcm", "linear16":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}

// cloudflareRunURL builds the native /ai/run/{model} URL. It accounts for an
// accountId placeholder in BaseURL, otherwise falls back to the standard local form.
func cloudflareRunURL(baseURL, model string, psd map[string]string) (string, error) {
	accountID := psd["accountId"]
	if accountID == "" {
		accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	}

	if baseURL != "" {
		u := strings.TrimRight(baseURL, "/")
		for _, suffix := range []string{"/ai/v1/chat/completions", "/ai/v1/embeddings", "/ai/v1/images/generations", "/ai/v1/responses", "/ai/v1/models", "/chat/completions", "/embeddings", "/images/generations", "/responses", "/models"} {
			u = strings.TrimSuffix(u, suffix)
		}
		if strings.Contains(u, "{accountId}") {
			if accountID == "" {
				return "", fmt.Errorf("cloudflare Workers AI requires an Account ID")
			}
			u = strings.ReplaceAll(u, "{accountId}", accountID)
		}
		return u + "/ai/run/" + normalizeModelName(model, providercfg.CompatibilityFor("cf")), nil
	}

	if accountID == "" {
		return "", fmt.Errorf("cloudflare Workers AI requires an Account ID. Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var.")
	}
	modelName := normalizeModelName(model, providercfg.CompatibilityFor("cf"))
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", accountID, modelName), nil
}

// cloudflareNormalizeAudioResponse strips the JSON envelope that some CF audio
// endpoints wrap around their binary output, leaving the raw audio bytes intact.
func cloudflareNormalizeAudioResponse(resp *Response) *Response {
	if resp == nil || resp.StatusCode >= 400 {
		return resp
	}
	ct := strings.ToLower(resp.Headers.Get("Content-Type"))
	if strings.Contains(ct, "audio/") || strings.Contains(ct, "application/octet-stream") {
		return resp
	}
	// CF sometimes returns compact JSON like {"result":{"audio":"..."}} or {"result":"..."}.
	if !gjson.ValidBytes(resp.Body) {
		return resp
	}
	res := gjson.GetBytes(resp.Body, "result")
	if !res.Exists() {
		return resp
	}
	if res.Type == gjson.String {
		resp.Body = []byte(res.String())
		resp.Headers.Set("Content-Type", "audio/mpeg")
	} else if res.IsObject() {
		// Try a known audio/base64 or audio field first.
		if aud := res.Get("audio"); aud.Type == gjson.String {
			resp.Body = []byte(aud.String())
			 if mime := res.Get("mimeType").String(); mime != "" {
				resp.Headers.Set("Content-Type", mime)
			} else {
				resp.Headers.Set("Content-Type", "audio/mpeg")
			}
		}
	}
	return resp
}

