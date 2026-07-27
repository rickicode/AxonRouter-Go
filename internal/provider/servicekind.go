package provider

// Service-kind constants identify capabilities offered by a provider type.
const (
	ServiceKindLLM                = "llm"
	ServiceKindEmbedding          = "embedding"
	ServiceKindImage              = "image"
	ServiceKindImageToText        = "imageToText"
	ServiceKindTTS                = "tts"
	ServiceKindSTT                = "stt"
	ServiceKindWebSearch          = "webSearch"
	ServiceKindWebFetch           = "webFetch"
	ServiceKindVideo              = "video"
	ServiceKindMusic            = "music"
	// ServiceKindClassification marks text-classification models. We reuse the
	// existing "llm" executor path for Cloudflare classification because the
	// CloudflareExecutor detects the task from the model/body and routes to the
	// native /ai/run endpoint while still producing an OpenAI-compatible shape.
	ServiceKindClassification = "classification"
	// ServiceKindRerank marks rerank models. Like classification, rerank is handled
	// inside the CloudflareExecutor and exposed through the chat-style route.
	ServiceKindRerank = "rerank"
	// ServiceKindImageClassification marks image-classification and object-detection
	// models. It is handled inside the CloudflareExecutor and exposed through the
	// chat-style route as well.
	ServiceKindImageClassification = "imageClassification"
)

// HasServiceKind reports whether kinds contains the requested service kind.
func HasServiceKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// DefaultServiceKinds returns the service kinds assumed when none are specified.
func DefaultServiceKinds() []string {
	return []string{ServiceKindLLM}
}
