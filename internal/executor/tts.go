package executor

import (
	"context"
	"fmt"
)

// TTSExecutor handles text-to-speech requests across providers.
type TTSExecutor struct {
	*BaseExecutor
}

// NewTTSExecutor creates a new TTS executor.
func NewTTSExecutor(base *BaseExecutor) *TTSExecutor {
	return &TTSExecutor{BaseExecutor: base}
}

// Execute performs a TTS request. Returns audio bytes.
func (e *TTSExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := e.resolveURL(req)

	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "audio/mpeg",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

	resp, err := e.DoRequest(ctx, "POST", url, headers, req.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tts error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream is not supported for TTS (binary audio).
func (e *TTSExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("TTS does not support streaming")
}

func (e *TTSExecutor) resolveURL(req *Request) string {
	if req.BaseURL != "" {
		return req.BaseURL
	}
	switch req.Provider {
	case "elevenlabs":
		model := ExtractModel(req.Model)
		return fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", model)
	default:
		return "https://api.openai.com/v1/audio/speech"
	}
}
