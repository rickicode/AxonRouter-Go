package providers

// TranslateAntigravity converts an Antigravity (Google-backed) error response
// into an OpenAI-compatible error JSON. The Antigravity backend uses the same
// Google API error envelope as Gemini, so we reuse the Gemini translator.
func TranslateAntigravity(statusCode int, body []byte) []byte {
	return TranslateGemini(statusCode, body)
}
