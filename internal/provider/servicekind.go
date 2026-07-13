package provider

// Service-kind constants identify capabilities offered by a provider type.
const (
	ServiceKindLLM         = "llm"
	ServiceKindEmbedding   = "embedding"
	ServiceKindImage       = "image"
	ServiceKindImageToText = "imageToText"
	ServiceKindTTS         = "tts"
	ServiceKindSTT         = "stt"
	ServiceKindWebSearch   = "webSearch"
	ServiceKindWebFetch    = "webFetch"
	ServiceKindVideo       = "video"
	ServiceKindMusic       = "music"
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
