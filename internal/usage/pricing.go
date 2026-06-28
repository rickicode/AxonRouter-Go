package usage

import "strings"

// Pricing holds cost per 1K tokens for a model.
type Pricing struct {
	InputPer1K   float64
	OutputPer1K  float64
	ReasonPer1K  float64
	ImagePerUnit float64
	AudioPerMin  float64
}

// pricingTable maps model prefixes/names to pricing.
// ponytail: hardcoded table, upgrade to DB later.
var pricingTable = map[string]Pricing{
	"gpt-4o":            {InputPer1K: 0.0025, OutputPer1K: 0.01},
	"gpt-4o-mini":       {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gpt-4-turbo":       {InputPer1K: 0.01, OutputPer1K: 0.03},
	"gpt-4":             {InputPer1K: 0.03, OutputPer1K: 0.06},
	"gpt-3.5-turbo":     {InputPer1K: 0.0005, OutputPer1K: 0.0015},
	"o3":                {InputPer1K: 0.01, OutputPer1K: 0.04},
	"o3-mini":           {InputPer1K: 0.0011, OutputPer1K: 0.0044},
	"o4-mini":           {InputPer1K: 0.0011, OutputPer1K: 0.0044},
	"claude-opus-4":     {InputPer1K: 0.015, OutputPer1K: 0.075},
	"claude-sonnet-4":   {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-3.5-sonnet": {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-3.5-haiku":  {InputPer1K: 0.0008, OutputPer1K: 0.004},
	"gemini-2.5-pro":    {InputPer1K: 0.00125, OutputPer1K: 0.01},
	"gemini-2.5-flash":  {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"deepseek-chat":     {InputPer1K: 0.00014, OutputPer1K: 0.00028},
	"deepseek-reasoner": {InputPer1K: 0.00055, OutputPer1K: 0.00219},
	"mimo-v2-pro":       {InputPer1K: 0.001, OutputPer1K: 0.002},
	"mimo-v2":           {InputPer1K: 0.0005, OutputPer1K: 0.001},
	"mimo-v2-flash":     {InputPer1K: 0.0001, OutputPer1K: 0.0002},
	"mimo-v2-omni":      {InputPer1K: 0.001, OutputPer1K: 0.002},
	"llama-3.3-70b":     {InputPer1K: 0.00059, OutputPer1K: 0.00079},
	"llama-3.1-405b":    {InputPer1K: 0.003, OutputPer1K: 0.003},
	"kimi-k2":           {InputPer1K: 0.0006, OutputPer1K: 0.0024},
}

// EstimateCost calculates the cost in USD for a request.
func EstimateCost(modelID string, inputTokens, outputTokens, reasoningTokens int64) float64 {
	p := lookupPricing(modelID)
	cost := float64(inputTokens) / 1000.0 * p.InputPer1K
	cost += float64(outputTokens) / 1000.0 * p.OutputPer1K
	cost += float64(reasoningTokens) / 1000.0 * p.ReasonPer1K
	return cost
}

// lookupPricing finds pricing for a model by prefix matching.
func lookupPricing(modelID string) Pricing {
	// Strip provider prefix
	_, model := splitModel(modelID)

	if p, ok := pricingTable[model]; ok {
		return p
	}
	// Fuzzy match: check if model contains any key
	for key, p := range pricingTable {
		if strings.Contains(model, key) {
			return p
		}
	}
	// Default: cheap
	return Pricing{InputPer1K: 0.001, OutputPer1K: 0.002}
}

func splitModel(s string) (string, string) {
	for i, c := range s {
		if c == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "", s
}
