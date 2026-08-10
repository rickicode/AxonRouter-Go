package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/tidwall/gjson"
)

// CloudflareSTTExecutor handles Cloudflare Workers AI speech-to-text by
// translating an OpenAI-style /v1/audio/transcriptions request to the native
// /ai/run/{model} endpoint.
type CloudflareSTTExecutor struct {
	*BaseExecutor
}

// NewCloudflareSTTExecutor creates a new Cloudflare STT executor.
func NewCloudflareSTTExecutor(base *BaseExecutor) *CloudflareSTTExecutor {
	return &CloudflareSTTExecutor{BaseExecutor: base}
}

// Execute translates the multipart STT request and returns an OpenAI-compatible
// transcription JSON.
func (e *CloudflareSTTExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url, err := cloudflareRunURL(req.BaseURL, req.Model, req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body, contentType, err := buildCloudflareSTTPayload(req.Body, req.Headers["Content-Type"])
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": contentType,
		"Accept":       "application/json",
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
	resp.Body = normalizeCloudflareSTTResponse(resp.Body)
	resp.Headers.Set("Content-Type", "application/json")
	return resp, nil
}

// ExecuteStream is not supported for STT.
func (e *CloudflareSTTExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("STT does not support streaming")
}

// buildCloudflareSTTPayload extracts the uploaded audio file and any language
// hint from an OpenAI /v1/audio/transcriptions multipart body and repackages
// it for CF's /ai/run/{model}. Cloudflare native STT accepts a multipart body
// with the audio under any key; we forward the file plus optional language.
func buildCloudflareSTTPayload(body []byte, contentType string) ([]byte, string, error) {
	if !strings.Contains(contentType, "multipart/") {
		return nil, "", fmt.Errorf("STT request must be multipart/form-data")
	}
	_, params, err := parseMediaType(contentType)
	if err != nil {
		return nil, "", fmt.Errorf("parse content type: %w", err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	if reader == nil {
		return nil, "", fmt.Errorf("invalid multipart body")
	}

	var audioData []byte
	var filename string
	language := ""

	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		name := part.FormName()
		data, err := readAll(part)
		if err != nil {
			part.Close()
			continue
		}
		part.Close()
		switch name {
		case "file":
			audioData = data
			filename = part.FileName()
		case "language":
			language = string(bytes.TrimSpace(data))
		}
	}
	if filename == "" {
		filename = "audio.mp3"
	}

	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	// Whisper uses "audio" as the canonical file key; keep it explicit.
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, "", err
	}
	if language != "" {
		_ = writer.WriteField("language", language)
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

// normalizeCloudflareSTTResponse unwraps CF's {"result":{"text":"...",...}}
// envelope into an OpenAI-compatible {"text":"..."} response. If the response
// is already shaped correctly (has top-level "text"), it is returned as-is.
func normalizeCloudflareSTTResponse(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	if gjson.GetBytes(body, "text").Exists() {
		return body
	}
	res := gjson.GetBytes(body, "result")
	if !res.Exists() {
		return body
	}
	if res.Type == gjson.String {
		out, _ := json.Marshal(map[string]any{"text": res.String()})
		return out
	}
	if res.IsObject() {
		text := res.Get("text").String()
		if text != "" {
			out, _ := json.Marshal(map[string]any{"text": text})
			return out
		}
		// Sometimes the nested object is a Deepgram result with "results.channels.0.alternatives.0.transcript".
		transcript := res.Get("results.0.channels.0.alternatives.0.transcript").String()
		if transcript != "" {
			out, _ := json.Marshal(map[string]any{"text": transcript})
			return out
		}
	}
	return body
}

func parseMediaType(s string) (string, map[string]string, error) {
	// Simple best-effort parser; stdlib mime.ParseMediaType fails on some boundaries.
	p := strings.SplitN(s, ";", 2)
	media := strings.TrimSpace(strings.ToLower(p[0]))
	params := make(map[string]string)
	if len(p) > 1 {
		for _, kv := range strings.Split(p[1], ";") {
			kv = strings.TrimSpace(kv)
			if idx := strings.Index(kv, "="); idx > 0 {
				key := strings.TrimSpace(strings.ToLower(kv[:idx]))
				val := strings.Trim(strings.TrimSpace(kv[idx+1:]), "\"")
				params[key] = val
			}
		}
	}
	return media, params, nil
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}


// BuildCloudflareSTTMultipart builds a multipart form for Cloudflare native STT
// endpoints. It places the audio file under the canonical "audio" field and
// forwards any language hint.
func BuildCloudflareSTTMultipart(audioData []byte, filename, model, language string) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, "", err
	}
	if language != "" {
		_ = writer.WriteField("language", language)
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}
