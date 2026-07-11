package compression

// CompressionMode defines the available compression levels.
type CompressionMode string

const (
	ModeOff        CompressionMode = "off"
	ModeLite       CompressionMode = "lite"
	ModeStandard   CompressionMode = "standard"
	ModeAggressive CompressionMode = "aggressive"
	ModeUltra      CompressionMode = "ultra"
)

// EngineStats reports token savings for a compression run.
type EngineStats struct {
	OriginalTokens   int      `json:"original_tokens"`
	CompressedTokens int      `json:"compressed_tokens"`
	SavingsPercent   float64  `json:"savings_percent"`
	TechniquesUsed   []string `json:"techniques_used"`
}

// EngineConfig is an untyped bag of engine-specific parameters.
type EngineConfig map[string]interface{}

// Engine is the interface implemented by every compression engine.
type Engine interface {
	ID() string
	Apply(body []byte, config EngineConfig) ([]byte, EngineStats, error)
}
