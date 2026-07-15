package executor

import "os"

// OpenRouterExecutor is a thin wrapper around OpenAIExecutor that ensures
// OpenRouter attribution headers are present on every request. It also lets
// operators override the default HTTP-Referer and X-Title via connection
// provider_specific_data or environment variables.
type OpenRouterExecutor struct {
	*OpenAIExecutor
}

// NewOpenRouterExecutor creates an executor for the OpenRouter endpoint.
func NewOpenRouterExecutor(base *BaseExecutor) *OpenRouterExecutor {
	return &OpenRouterExecutor{OpenAIExecutor: NewOpenAIExecutor(base)}
}

// openRouterHeaders sets the HTTP-Referer and X-Title attribution headers
// that OpenRouter uses for app identification and rate-limit tracking.
// Preference order: provider_specific_data > env var > sensible default.
func openRouterHeaders(headers map[string]string, provider string, psd map[string]string) {
	if provider != "openrouter" {
		return
	}

	referer := ""
	title := ""
	if psd != nil {
		referer = psd["http_referer"]
		title = psd["x_title"]
	}
	if referer == "" {
		referer = os.Getenv("OPENROUTER_HTTP_REFERER")
	}
	if title == "" {
		title = os.Getenv("OPENROUTER_X_TITLE")
	}
	if referer == "" {
		referer = "https://endpoint-proxy.local"
	}
	if title == "" {
		title = "Endpoint Proxy"
	}

	headers["HTTP-Referer"] = referer
	headers["X-Title"] = title
}
