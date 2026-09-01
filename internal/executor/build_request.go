package executor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BuildUpstreamRequest mirrors how an executor would construct its upstream
// HTTP request for a translated body — WITHOUT actually sending it. It is used
// by the translator debugger (step 3) to preview the exact URL, headers and
// final body that the gateway would send to the provider, and by tests to pin
// down per-format request building.
//
// The preview intentionally shares the same endpoint/header/transform helpers
// used by the real Execute/ExecuteStream paths so the preview never drifts from
// production behavior. Format-specific transforms that need per-executor state
// (Claude tool-map reconciliation, Codex job-token sideband, Copilot tokens,
// Gemini system-instruction folding, etc.) are applied in the actual executors
// and are out of scope for the preview; it captures the deterministic core.
func BuildUpstreamRequest(req *Request) (*BuiltUpstreamRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	provider := req.Provider
	model := req.Model
	body := req.Body
	stream := req.Stream
	psd := req.ProviderSpecificData
	baseURL := req.BaseURL

	format := ""
	if _, f, ok := GetRegistry().Get(provider); ok {
		format = string(f)
	}

	var url string
	var err error

	// Apply the provider-neutral streaming mutations that executors apply before
	// building the URL/headers so the preview body matches the real request body.
	if stream {
		body = JSONSet(body, "stream", true)
		body = iflowTransformRequest(body, provider, true)
	} else {
		body = JSONSet(body, "stream", false)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, provider, psd)
	codebuddyHeaders(headers, provider)
	iflowHeaders(headers, provider, psd)

	switch format {
	case string(FormatOpenAI):
		url, err = openAIEndpoint(baseURL, "chat/completions", psd)
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	case string(FormatClaude):
		url = baseURL
		if url == "" {
			url = "https://api.anthropic.com/v1/messages"
		}
		headers["anthropic-version"] = "2023-06-01"
		headers["x-api-key"] = req.APIKey
		if req.AccessToken != "" {
			headers["Authorization"] = "Bearer " + req.AccessToken
		}
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	case string(FormatGemini):
		url = geminiEndpoint(baseURL, model, "generateContent")
		headers["x-goog-api-key"] = req.APIKey
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	case string(FormatOpenAIResponses), string(FormatAntigravity):
		url, err = openAIEndpoint(baseURL, "responses", psd)
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	case string(FormatKiro):
		url, err = openAIEndpoint(baseURL, "chat/completions", psd)
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	case string(FormatGrokCLI):
		url = baseURL
		if url == "" {
			url = "https://api.x.ai/v1/chat/completions"
		}
		if req.AccessToken != "" {
			headers["Authorization"] = "Bearer " + req.AccessToken
		} else if req.APIKey != "" {
			headers["Authorization"] = "Bearer " + req.APIKey
		}
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	default:
		// Unknown/not-registered format: fall back to OpenAI-compatible chat.
		url, err = openAIEndpoint(baseURL, "chat/completions", psd)
		if stream {
			headers["Accept"] = "text/event-stream"
			headers["Cache-Control"] = "no-cache"
		}
	}
	if err != nil {
		return nil, err
	}

	if url != "" {
		if err := validateURL(url); err != nil {
			return nil, fmt.Errorf("blocked URL: %w", err)
		}
	}

	return &BuiltUpstreamRequest{
		Provider: provider,
		Model:    model,
		Format:   format,
		URL:      url,
		Headers:  headers,
		Body:     body,
		BuiltAt:  time.Now().UTC(),
	}, nil
}

// BuiltUpstreamRequest is the previewed upstream request for one debugger step.
type BuiltUpstreamRequest struct {
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Format   string            `json:"format"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Body     []byte            `json:"body"`
	BuiltAt  time.Time         `json:"built_at"`
}

// HeaderJSON returns the headers as a JSON object (sorted keys for stable diffs).
func (b *BuiltUpstreamRequest) HeaderJSON() []byte {
	out, _ := json.Marshal(b.Headers)
	return out
}

// TrimmedBaseURL is a helper for debugger display: it hides nothing but keeps
// the URL readable by stripping a trailing endpoint when the base URL already
// includes one (openAIEndpoint strips it internally anyway).
func TrimmedBaseURL(baseURL string) string {
	u := strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/embeddings", "/models"} {
		u = strings.TrimSuffix(u, suffix)
	}
	return u
}
