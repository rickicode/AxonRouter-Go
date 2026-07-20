// Package kiro exposes the static Kiro model catalog and synthetic variant
// generator used by discovery and routing.
package kiro

// Capabilities flags a model variant as thinking / agentic / vision / reasoning / search.
type Capabilities struct {
	Thinking  bool
	Agentic   bool
	Vision    bool
	Reasoning bool
	Search    bool
}

// BaseModel is a verified upstream Kiro model without synthetic suffixes.
type BaseModel struct {
	ID              string
	DisplayName     string
	OwnedBy         string
	ContextLength   int
	MaxOutputTokens int
}

// Model is a concrete Kiro model entry, possibly a synthetic variant.
type Model struct {
	BaseModel
	Capabilities    Capabilities
	UpstreamModelID string
	VariantSuffix   string
	Description     string
	RateMultiplier  float64
	Strip           []string
}

// baseCapabilities returns the upstream capability flags for a base model ID.
// These are independent from synthetic -thinking / -agentic variants.
func baseCapabilities(id string) Capabilities {
	switch id {
	case "claude-sonnet-4.5", "claude-sonnet-4", "claude-haiku-4.5":
		return Capabilities{Vision: true}
	}
	return Capabilities{}
}

// baseStrip returns content types a base model cannot consume.
func baseStrip(id string) []string {
	switch id {
	case "deepseek-3.2", "qwen3-coder-next":
		return []string{"image", "audio"}
	}
	return nil
}

// baseRateMultiplier returns the cost multiplier used by 9router for GPT 5.6 models.
func baseRateMultiplier(id string) float64 {
	return 1.0
}

// baseDescription returns a short description for known experimental models.
func baseDescription(id string) string {
	return ""
}

// BaseModels lists the verified upstream Kiro models.
// These IDs match Kiro's real upstream catalog exactly (fabricated IDs are
// rejected with "Invalid model"). Synthetic -thinking and -agentic variants are
// generated from this list at runtime.
var BaseModels = []BaseModel{
	{
		ID:              "auto",
		DisplayName:     "Kiro Auto",
		OwnedBy:         "amazon",
		ContextLength:   200000,
		MaxOutputTokens: 64000,
	},
	{
		ID:              "claude-sonnet-4.5",
		DisplayName:     "Claude Sonnet 4.5",
		OwnedBy:         "amazon",
		ContextLength:   200000,
		MaxOutputTokens: 64000,
	},
	{
		ID:              "claude-sonnet-4",
		DisplayName:     "Claude Sonnet 4",
		OwnedBy:         "amazon",
		ContextLength:   200000,
		MaxOutputTokens: 64000,
	},
	{
		ID:              "claude-haiku-4.5",
		DisplayName:     "Claude Haiku 4.5",
		OwnedBy:         "amazon",
		ContextLength:   200000,
		MaxOutputTokens: 64000,
	},
	{
		ID:              "deepseek-3.2",
		DisplayName:     "DeepSeek V3.2",
		OwnedBy:         "deepseek",
		ContextLength:   128000,
		MaxOutputTokens: 8192,
	},
	{
		ID:              "minimax-m2.5",
		DisplayName:     "MiniMax M2.5",
		OwnedBy:         "minimax",
		ContextLength:   200000,
		MaxOutputTokens: 8192,
	},
	{
		ID:              "minimax-m2.1",
		DisplayName:     "MiniMax M2.1",
		OwnedBy:         "minimax",
		ContextLength:   200000,
		MaxOutputTokens: 8192,
	},
	{
		ID:              "glm-5",
		DisplayName:     "GLM 5",
		OwnedBy:         "zhipu",
		ContextLength:   128000,
		MaxOutputTokens: 8192,
	},
	{
		ID:              "qwen3-coder-next",
		DisplayName:     "Qwen3 Coder Next",
		OwnedBy:         "alibaba",
		ContextLength:   131072,
		MaxOutputTokens: 32768,
	},
}

// AllModels returns the base models plus every synthetic variant.
func AllModels() []Model {
	return ExpandVariants(BaseModels)
}

// ExpandVariants builds base, -thinking, -agentic and -thinking-agentic
// variants for each supplied base model. Unknown synthetic suffixes are stripped
// from the base ID before expansion so that callers can pass already-suffixed
// upstream IDs safely.
func ExpandVariants(bases []BaseModel) []Model {
	out := make([]Model, 0, len(bases)*4)
	for _, b := range bases {
		baseID := StripSyntheticSuffix(b.ID)
		base := b
		base.ID = baseID
		base.DisplayName = stripVariantDisplayName(b.DisplayName)
		caps := baseCapabilities(baseID)
		strip := baseStrip(baseID)
		desc := baseDescription(baseID)
		rate := baseRateMultiplier(baseID)
		out = append(out, Model{
			BaseModel:       base,
			Capabilities:    caps,
			UpstreamModelID: baseID,
			VariantSuffix:   "",
			Description:     desc,
			RateMultiplier:  rate,
			Strip:           strip,
		})
		out = append(out, Model{
			BaseModel:       variantBase(base, "thinking"),
			Capabilities:    withThinking(caps),
			UpstreamModelID: baseID,
			VariantSuffix:   "thinking",
			Description:     desc,
			RateMultiplier:  rate,
			Strip:           strip,
		})
		out = append(out, Model{
			BaseModel:       variantBase(base, "agentic"),
			Capabilities:    withAgentic(caps),
			UpstreamModelID: baseID,
			VariantSuffix:   "agentic",
			Description:     desc,
			RateMultiplier:  rate,
			Strip:           strip,
		})
		out = append(out, Model{
			BaseModel:       variantBase(base, "thinking-agentic"),
			Capabilities:    withThinking(withAgentic(caps)),
			UpstreamModelID: baseID,
			VariantSuffix:   "thinking-agentic",
			Description:     desc,
			RateMultiplier:  rate,
			Strip:           strip,
		})
	}
	return out
}

func withThinking(c Capabilities) Capabilities {
	c.Thinking = true
	return c
}

func withAgentic(c Capabilities) Capabilities {
	c.Agentic = true
	return c
}

// StripSyntheticSuffix removes -thinking, -agentic and -thinking-agentic
// suffixes from a model ID.
func StripSyntheticSuffix(id string) string {
	if len(id) > 17 && id[len(id)-17:] == "-thinking-agentic" {
		return id[:len(id)-17]
	}
	if len(id) > 8 && id[len(id)-8:] == "-agentic" {
		return id[:len(id)-8]
	}
	if len(id) > 9 && id[len(id)-9:] == "-thinking" {
		return id[:len(id)-9]
	}
	return id
}

func variantBase(base BaseModel, suffix string) BaseModel {
	base.ID = base.ID + "-" + suffix
	base.DisplayName = base.DisplayName + " (" + variantLabel(suffix) + ")"
	return base
}

func variantLabel(suffix string) string {
	switch suffix {
	case "thinking":
		return "Thinking"
	case "agentic":
		return "Agentic"
	case "thinking-agentic":
		return "Thinking + Agentic"
	default:
		return suffix
	}
}

func stripVariantDisplayName(name string) string {
	for _, suffix := range []string{" (Thinking + Agentic)", " (Thinking)", " (Agentic)"} {
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}
