package smart

// Strategy determines how candidates are ranked by the smart router.
type Strategy string

const (
	// StrategyAuto balances cost, quality, and latency.
	StrategyAuto Strategy = "auto"
	// StrategyFast minimizes latency and cost.
	StrategyFast Strategy = "fast"
	// StrategyQuality maximizes capability and success rate, even at higher cost.
	StrategyQuality Strategy = "quality"
)

// VirtualModel identifies a smart virtual model.
type VirtualModel string

const (
	VirtualAuto    VirtualModel = "smart/auto"
	VirtualFast    VirtualModel = "smart/auto-fast"
	VirtualQuality VirtualModel = "smart/auto-quality"
)

// KnownVirtualModels is the ordered list of supported virtual models.
var KnownVirtualModels = []VirtualModel{VirtualAuto, VirtualFast, VirtualQuality}

// strategyForModel maps a virtual model id to its selection strategy.
var strategyForModel = map[VirtualModel]Strategy{
	VirtualAuto:    StrategyAuto,
	VirtualFast:    StrategyFast,
	VirtualQuality: StrategyQuality,
}

// FeatureVector describes the request features used for routing decisions.
type FeatureVector struct {
	TotalTokens   int64  // Estimated total input tokens.
	MessageCount  int    // Number of conversational turns.
	HasImages     bool   // Request contains image input.
	HasAudio      bool   // Request contains audio input.
	HasVideo      bool   // Request contains video input.
	HasPDF        bool   // Request contains PDF input.
	ToolCount     int    // Number of tools declared at top level.
	ToolCallCount int    // Number of assistant tool_calls in the conversation.
	Reasoning     bool   // Request explicitly asks for reasoning.
	ReasoningEffort string // "low", "medium", or "high" when set.
	CodeHint      bool   // Content contains code fences with a language hint.
	CodeLanguage  string // First detected code-fence language, if any.
}

// Complexity returns a unitless complexity score. Higher means a more demanding request.
func (f FeatureVector) Complexity() float64 {
	score := float64(f.TotalTokens) + float64(f.MessageCount)*10.0
	if f.HasImages {
		score += 500
	}
	if f.HasAudio {
		score += 300
	}
	if f.HasVideo {
		score += 1000
	}
	if f.HasPDF {
		score += 400
	}
	score += float64(f.ToolCount) * 50.0
	score += float64(f.ToolCallCount) * 20.0
	if f.Reasoning {
		switch f.ReasoningEffort {
		case "high":
			score += 800
		case "medium":
			score += 400
		default:
			score += 200
		}
	}
	return score
}

// VirtualModelConfig is the persisted shape of the smart-router registry.
type VirtualModelConfig struct {
	Models []VirtualModelEntry `json:"models"`
}

// VirtualModelEntry is a single virtual model mapping.
type VirtualModelEntry struct {
	ID         string   `json:"id"`
	Enabled    bool     `json:"enabled"`
	Candidates []string `json:"candidates"`
}

// Telemetry aggregates recent performance data for a concrete model.
type Telemetry struct {
	Requests        int64
	AvgLatencyMs    float64
	SuccessRate     float64
	CostPer1KTokens float64
}

// CandidateScore holds intermediate scoring state for a candidate.
type candidateScore struct {
	modelID         string
	provider        string
	model           string
	telemetry       Telemetry
	caps            modelCapabilities
	hardSatisfied   bool
	softSatisfied   bool
	latencyScore    float64
	costScore       float64
	qualityScore    float64
	capScore        float64
	finalScore      float64
}

// modelCapabilities is a local copy of models.ModelCapabilities so this package
// can compare requirements without an extra import alias.
type modelCapabilities struct {
	Vision     bool
	PDF        bool
	AudioInput bool
	VideoInput bool
	Tools      bool
}

// IsVirtualModel reports whether modelID is one of the supported smart virtual models.
func IsVirtualModel(modelID string) bool {
	for _, v := range KnownVirtualModels {
		if string(v) == modelID {
			return true
		}
	}
	return false
}

// emptyConfig returns the default registry with all virtual models enabled and no candidates.
func emptyConfig() *VirtualModelConfig {
	cfg := &VirtualModelConfig{Models: make([]VirtualModelEntry, 0, len(KnownVirtualModels))}
	for _, v := range KnownVirtualModels {
		cfg.Models = append(cfg.Models, VirtualModelEntry{ID: string(v), Enabled: true, Candidates: []string{}})
	}
	return cfg
}
