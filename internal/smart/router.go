package smart

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// Router selects a concrete model for a virtual model request.
type Router struct {
	registry  *Registry
	telemetry *TelemetryStore
	store     *connstate.Store
	elig      *connstate.EligibilityManager
}

// NewRouter builds the smart router.
func NewRouter(registry *Registry, telemetry *TelemetryStore, store *connstate.Store, elig *connstate.EligibilityManager) *Router {
	return &Router{
		registry:  registry,
		telemetry: telemetry,
		store:     store,
		elig:      elig,
	}
}

// ResolveResult is the chosen concrete model plus routing metadata.
type ResolveResult struct {
	ConnectionID string   `json:"connection_id"`
	Provider     string   `json:"provider"`
	ModelID      string   `json:"model_id"`
	Display      string   `json:"display_model_id"`
	Score        float64  `json:"score"`
	Reason       string   `json:"reason"`
	Capabilities []string `json:"capabilities"`
}

// Resolve picks the best concrete provider/model for the virtual model.
func (r *Router) Resolve(virtualModelID string, body []byte) (*ResolveResult, error) {
	if !IsVirtualModel(virtualModelID) {
		return nil, fmt.Errorf("not a virtual model: %s", virtualModelID)
	}
	vm, ok := r.registry.Get(VirtualModelID(virtualModelID))
	if !ok {
		return nil, fmt.Errorf("virtual model configuration not found: %s", virtualModelID)
	}
	if !vm.Enabled {
		return nil, fmt.Errorf("virtual model disabled: %s", virtualModelID)
	}
	features := ExtractFeatures(body)
	required := ExtractCapabilityFeatures(body)
	tel := r.telemetry.All()

	candidates := vm.Candidates
	if len(candidates) == 0 {
		candidates = r.defaultCandidates()
	}

	scored := r.scoreCandidates(candidates, vm, features, required, tel)
	if len(scored) == 0 {
		return nil, fmt.Errorf("no eligible candidate for %s", virtualModelID)
	}

	best := scored[0]
	return &ResolveResult{
		ConnectionID: best.connID,
		Provider:     best.provider,
		ModelID:      best.modelID,
		Display:      best.display,
		Score:        best.score,
		Reason:       best.reason,
		Capabilities: best.capabilities,
	}, nil
}

// score holds an evaluated candidate.
type score struct {
	connID       string
	provider     string
	modelID      string
	display      string
	capabilityOk bool
	score        float64
	reason       string
	capabilities []string
}

func (r *Router) scoreCandidates(candidates []string, vm *VirtualModel, features Features, required models.ModelCapabilities, tel map[string]*ModelTelemetry) []score {
	var out []score
	for _, cand := range candidates {
		s := r.evaluate(cand, vm, features, required, tel)
		if s == nil {
			continue
		}
		out = append(out, *s)
	}
	if len(out) == 0 {
		return nil
	}

	// Sort: eligible capability matches first, then higher score.
	sort.Slice(out, func(i, j int) bool {
		if out[i].capabilityOk != out[j].capabilityOk {
			return out[i].capabilityOk
		}
		return out[i].score > out[j].score
	})
	return out
}

func (r *Router) evaluate(cand string, vm *VirtualModel, features Features, required models.ModelCapabilities, tel map[string]*ModelTelemetry) *score {
	provider, modelName, ok := splitProviderModel(cand)
	if !ok {
		logging.Logger.Warn("smart router: invalid candidate model id", "candidate", cand)
		return nil
	}
	connID, eligible := r.findEligibleConnection(provider, modelName)
	if !eligible {
		return nil
	}
	caps := models.GetCapabilities(cand)
	capMatch := capabilityMatches(caps, required)

	baseScore := r.baseStrategyScore(vm.Strategy, features, caps, required)

	t := tel[cand]
	if t == nil {
		t = &ModelTelemetry{ModelID: cand, SuccessRate: 1.0, AvgLatencyMs: 500, CostPer1kTok: 0}
	}

	// Normalize telemetry into reward terms.
	latencyTerm := 1.0 / (1.0 + t.AvgLatencyMs/1000.0)
	successTerm := t.SuccessRate
	costTerm := 1.0 / (1.0 + t.CostPer1kTok*10.0)
	requestLoad := math.Min(float64(t.TotalReqs)/100.0, 0.15)

	var final float64
	switch vm.Strategy {
	case StrategyFast:
		final = baseScore*0.25 + latencyTerm*0.45 + successTerm*0.25 + costTerm*0.05
	case StrategyQuality:
		final = baseScore*0.55 + latencyTerm*0.05 + successTerm*0.25 + costTerm*0.05 + requestLoad*0.1
	case StrategyBalanced:
		fallthrough
	default:
		final = baseScore*0.35 + latencyTerm*0.25 + successTerm*0.25 + costTerm*0.15
	}

	// Capability mismatch heavily penalized.
	if !capMatch {
		final *= 0.25
	}

	// Small jitter to avoid pathological ties.
	final += rand.Float64() * 0.001

	reason := fmt.Sprintf("strategy=%s caps=%v latency=%.0fms success=%.2f cost/1k=%.4f",
		vm.Strategy, caps, t.AvgLatencyMs, t.SuccessRate, t.CostPer1kTok)

	return &score{
		connID:       connID,
		provider:     provider,
		modelID:      modelName,
		display:      cand,
		capabilityOk: capMatch,
		score:        final,
		reason:       reason,
		capabilities: capabilityList(caps),
	}
}

func (r *Router) baseStrategyScore(strategy Strategy, features Features, caps models.ModelCapabilities, required models.ModelCapabilities) float64 {
	quality := 0.0
	switch strategy {
	case StrategyFast:
		quality = 0.3
	case StrategyBalanced:
		quality = 0.5 + math.Min(features.Score, 0.3)
	case StrategyQuality:
		quality = 0.7 + math.Min(features.Score, 0.25)
	}

	// Prefer capable models according to request complexity and required features.
	if required.Vision && caps.Vision {
		quality += 0.15
	}
	if required.PDF && caps.PDF {
		quality += 0.1
	}
	if required.AudioInput && caps.AudioInput {
		quality += 0.15
	}
	if required.VideoInput && caps.VideoInput {
		quality += 0.15
	}
	if required.Tools && caps.Tools {
		quality += 0.1
	}
	return math.Min(quality, 1.0)
}

func (r *Router) findEligibleConnection(provider, modelID string) (string, bool) {
	if r.store == nil {
		return "", false
	}
	// First try eligibility manager for healthy connection.
	if r.elig != nil {
		if cs := r.elig.PickConnection(provider, modelID); cs != nil {
			if status := cs.GetStatus(); status.IsEligible() && !status.IsRoutingTerminal() {
				return cs.ID, true
			}
		}
	}
	// Fallback to connection store scan.
	var found string
	r.store.Range(func(connID string, cs *connstate.ConnectionState) bool {
		if cs.Prefix != provider {
			return true
		}
		if status := cs.GetStatus(); !status.IsEligible() || status.IsRoutingTerminal() {
			return true
		}
		if cs.IsInCooldown() {
			return true
		}
		found = cs.ID
		return false
	})
	return found, found != ""
}

// defaultCandidates is used when no explicit candidate list is configured.
func (r *Router) defaultCandidates() []string {
	return []string{
		"openai/gpt-4o",
		"openai/gpt-4o-mini",
		"anthropic/claude-sonnet-4-5-20251022",
		"anthropic/claude-opus-4-6-20251022",
		"google/gemini-2.5-flash",
		"google/gemini-2.5-pro",
	}
}

func capabilityMatches(caps, required models.ModelCapabilities) bool {
	return (!required.Vision || caps.Vision) &&
		(!required.PDF || caps.PDF) &&
		(!required.AudioInput || caps.AudioInput) &&
		(!required.VideoInput || caps.VideoInput) &&
		(!required.Tools || caps.Tools)
}

func capabilityList(caps models.ModelCapabilities) []string {
	var out []string
	if caps.Vision {
		out = append(out, "vision")
	}
	if caps.PDF {
		out = append(out, "pdf")
	}
	if caps.AudioInput {
		out = append(out, "audio_input")
	}
	if caps.VideoInput {
		out = append(out, "video_input")
	}
	if caps.Tools {
		out = append(out, "tools")
	}
	return out
}

// splitProviderModel splits "provider/model-id" into provider and raw model.
func splitProviderModel(model string) (provider, name string, ok bool) {
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx], model[idx+1:], true
	}
	return "", model, false
}
