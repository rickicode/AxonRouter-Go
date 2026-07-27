package smart

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/models"
	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
)

const (
	// SettingsKey is the settings row that stores the virtual model registry.
	SettingsKey = "smart_router_virtual_models"
	// DefaultConfigTTL controls how long the registry is cached in memory.
	DefaultConfigTTL = 10 * time.Second
)

var (
	// ErrNoCandidates is returned when a virtual model has no usable candidates.
	ErrNoCandidates = errors.New("no eligible candidates for virtual model")
	// ErrDisabled is returned when the requested virtual model is disabled.
	ErrDisabled = errors.New("virtual model is disabled")
	// ErrUnknownVirtualModel is returned for unsupported virtual model ids.
	ErrUnknownVirtualModel = errors.New("unknown virtual model")
)

// Router resolves smart virtual models to concrete provider/model ids.
type Router struct {
	db        *sql.DB
	store     *connstate.Store
	elig      *connstate.EligibilityManager
	configTTL time.Duration
	tel       *TelemetryCache

	mu       sync.RWMutex
	cached   *VirtualModelConfig
	cachedAt time.Time
}

// RouterOption configures a Router.
type RouterOption func(*Router)

// WithConfigTTL sets the registry cache TTL.
func WithConfigTTL(d time.Duration) RouterOption {
	return func(r *Router) {
		r.configTTL = d
	}
}

// NewRouter creates a smart router.
func NewRouter(database *sql.DB, store *connstate.Store, elig *connstate.EligibilityManager, opts ...RouterOption) *Router {
	r := &Router{
		db:        database,
		store:     store,
		elig:      elig,
		configTTL: DefaultConfigTTL,
		tel:       newTelemetryCache(database),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve picks a concrete provider/model-id for a smart virtual model.
// The allowed set comes from API-key allowed_models; an empty map means
// no restrictions. body is used to detect required capabilities and complexity.
func (r *Router) Resolve(ctx context.Context, virtualModelID string, body []byte, allowed map[string]struct{}) (string, error) {
	if !IsVirtualModel(virtualModelID) {
		return "", fmt.Errorf("%w: %s", ErrUnknownVirtualModel, virtualModelID)
	}

	cfg, err := r.loadConfig()
	if err != nil {
		return "", fmt.Errorf("load smart router config: %w", err)
	}

	entry, ok := cfg.find(virtualModelID)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrDisabled, virtualModelID)
	}
	if !entry.Enabled {
		return "", fmt.Errorf("%w: %s", ErrDisabled, virtualModelID)
	}
	if len(entry.Candidates) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNoCandidates, virtualModelID)
	}

	required := detectRequiredCapabilities(body)
	features := ExtractFeatures(body)

	candidates := r.prepareCandidates(entry.Candidates, allowed, required, features)
	if len(candidates) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNoCandidates, virtualModelID)
	}

	strategy := strategyForModel[VirtualModel(virtualModelID)]
	best := pickBest(candidates, strategy, features)
	if best == "" {
		return "", fmt.Errorf("%w: %s", ErrNoCandidates, virtualModelID)
	}
	return best, nil
}

// Refresh invalidates in-memory caches so the next Resolve reads fresh data.
func (r *Router) Refresh() {
	r.mu.Lock()
	r.cachedAt = time.Time{}
	r.mu.Unlock()
	r.tel.Refresh()
}

// loadConfig reads and caches the virtual model registry from settings.
func (r *Router) loadConfig() (*VirtualModelConfig, error) {
	r.mu.RLock()
	if r.cached != nil && time.Since(r.cachedAt) < r.configTTL {
		defer r.mu.RUnlock()
		return r.cached, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil && time.Since(r.cachedAt) < r.configTTL {
		return r.cached, nil
	}

	cfg := emptyConfig()
	if r.db != nil {
		var raw string
		row := r.db.QueryRowContext(context.Background(), `SELECT value FROM settings WHERE key = ?`, SettingsKey)
		if err := row.Scan(&raw); err == nil && raw != "" {
			if parsed := parseConfig(raw); parsed != nil {
				cfg = parsed
			}
		}
	}
	r.cached = cfg
	r.cachedAt = time.Now()
	return cfg, nil
}

func parseConfig(raw string) *VirtualModelConfig {
	var cfg VirtualModelConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil
	}
	out := emptyConfig()
	byID := make(map[string]VirtualModelEntry)
	for _, m := range cfg.Models {
		byID[m.ID] = m
	}
	for i, v := range out.Models {
		if m, ok := byID[v.ID]; ok {
			if m.Candidates == nil {
				m.Candidates = []string{}
			}
			out.Models[i] = m
		}
	}
	if len(out.Models) == 0 {
		return nil
	}
	return out
}

func (c *VirtualModelConfig) find(id string) (VirtualModelEntry, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return VirtualModelEntry{}, false
}

// prepareCandidates filters and enriches candidate models for scoring.
func (r *Router) prepareCandidates(candidates []string, allowed map[string]struct{}, required modelCapabilities, features FeatureVector) []candidateScore {
	allTelemetry := r.tel.GetAll()
	out := make([]candidateScore, 0, len(candidates))

	for _, modelID := range candidates {
		provider, model, ok := splitProviderModel(modelID)
		if !ok {
			continue
		}
		if len(allowed) > 0 && !modelAllowed(modelID, provider, allowed) {
			continue
		}
		caps := models.GetCapabilities(modelID)
		if !hardCapabilitiesMatch(required, caps) {
			continue
		}
		if !providerAvailable(provider, model, r.store, r.elig) {
			continue
		}

		t := allTelemetry[modelID]
		if t.Requests == 0 {
			t = telemetryFromPricing(modelID)
		}

		out = append(out, candidateScore{
			modelID:       modelID,
			provider:      provider,
			model:         model,
			telemetry:     t,
			caps:          modelCapabilities{Vision: caps.Vision, PDF: caps.PDF, AudioInput: caps.AudioInput, VideoInput: caps.VideoInput, Tools: caps.Tools},
			hardSatisfied: true,
			softSatisfied: softCapabilitiesMatch(required, caps),
		})
	}

	return out
}

func splitProviderModel(modelID string) (provider, model string, ok bool) {
	s := strings.TrimSpace(modelID)
	s = strings.TrimPrefix(s, "@")
	idx := strings.Index(s, "/")
	if idx <= 0 || idx+1 >= len(s) {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

func modelAllowed(modelID, provider string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	if _, ok := allowed[modelID]; ok {
		return true
	}
	if _, ok := allowed[provider]; ok {
		return true
	}
	return false
}

func providerAvailable(provider, model string, store *connstate.Store, elig *connstate.EligibilityManager) bool {
	provider = provideralias.ResolveAlias(provider)
	if elig != nil {
		cs := elig.PickConnection(provider, model)
		if cs == nil || cs.ID == "" {
			return false
		}
		return true
	}
	if store == nil {
		return true
	}
	found := false
	store.RangeByConnID(func(_ string, cs *connstate.ConnectionState) bool {
		if cs.Prefix != provider {
			return true
		}
		if cs.GetStatus().IsRoutingTerminal() {
			return true
		}
		if cs.IsInCooldown() {
			return true
		}
		if cs.IsModelInCooldown(model) {
			return true
		}
		found = true
		return false
	})
	return found
}

func detectRequiredCapabilities(body []byte) modelCapabilities {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return modelCapabilities{}
	}
	var caps modelCapabilities

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		caps.Tools = true
	}

	if msgs, ok := req["messages"].([]any); ok {
		for _, m := range msgs {
			detectMsgCaps(m, &caps)
		}
	}

	if input, ok := req["input"].([]any); ok {
		for _, item := range input {
			detectMsgCaps(item, &caps)
		}
	}

	if contents, ok := req["contents"].([]any); ok {
		for _, item := range contents {
			detectGeminiContentCaps(item, &caps)
		}
	}
	if request, ok := req["request"].(map[string]any); ok {
		if contents, ok := request["contents"].([]any); ok {
			for _, item := range contents {
				detectGeminiContentCaps(item, &caps)
			}
		}
	}

	return caps
}

func detectMsgCaps(item any, caps *modelCapabilities) {
	msg, ok := item.(map[string]any)
	if !ok {
		return
	}
	if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
		caps.Tools = true
	}
	detectContentCaps(msg["content"], caps)
}

func detectContentCaps(content any, caps *modelCapabilities) {
	switch v := content.(type) {
	case string:
		// no-op
	case []any:
		for _, part := range v {
			detectPartCaps(part, caps)
		}
	case map[string]any:
		detectPartCaps(v, caps)
	}
}

func detectPartCaps(part any, caps *modelCapabilities) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "image_url", "image", "input_image":
		caps.Vision = true
	case "input_audio", "audio":
		caps.AudioInput = true
	case "video", "input_video":
		caps.VideoInput = true
	case "file", "document", "input_file":
		if isPDFMap(m) {
			caps.PDF = true
		}
	}
	if sub, ok := m["file"].(map[string]any); ok && isPDFMap(sub) {
		caps.PDF = true
	}
}

func detectGeminiContentCaps(item any, caps *modelCapabilities) {
	content, ok := item.(map[string]any)
	if !ok {
		return
	}
	if parts, ok := content["parts"].([]any); ok {
		for _, part := range parts {
			detectGeminiPartCaps(part, caps)
		}
	}
}

func detectGeminiPartCaps(part any, caps *modelCapabilities) {
	m, ok := part.(map[string]any)
	if !ok {
		return
	}
	if inlineData, ok := m["inlineData"].(map[string]any); ok {
		detectMimeCaps(inlineData, caps)
	}
	if fileData, ok := m["fileData"].(map[string]any); ok {
		detectMimeCaps(fileData, caps)
	}
}

func detectMimeCaps(m map[string]any, caps *modelCapabilities) {
	mime, _ := m["mimeType"].(string)
	if mime == "" {
		mime, _ = m["mime_type"].(string)
	}
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		caps.Vision = true
	case strings.HasPrefix(mime, "audio/"):
		caps.AudioInput = true
	case strings.HasPrefix(mime, "video/"):
		caps.VideoInput = true
	case strings.Contains(mime, "pdf"):
		caps.PDF = true
	}
}

func hardCapabilitiesMatch(required modelCapabilities, caps models.ModelCapabilities) bool {
	if required.Vision && !caps.Vision {
		return false
	}
	if required.PDF && !caps.PDF {
		return false
	}
	if required.AudioInput && !caps.AudioInput {
		return false
	}
	if required.VideoInput && !caps.VideoInput {
		return false
	}
	return true
}

func softCapabilitiesMatch(required modelCapabilities, caps models.ModelCapabilities) bool {
	if required.Tools && !caps.Tools {
		return false
	}
	return true
}

func pickBest(candidates []candidateScore, strategy Strategy, features FeatureVector) string {
	if len(candidates) == 0 {
		return ""
	}

	minLatency, maxLatency := math.Inf(1), math.Inf(-1)
	minCost, maxCost := math.Inf(1), math.Inf(-1)
	for _, c := range candidates {
		if c.telemetry.AvgLatencyMs < minLatency {
			minLatency = c.telemetry.AvgLatencyMs
		}
		if c.telemetry.AvgLatencyMs > maxLatency {
			maxLatency = c.telemetry.AvgLatencyMs
		}
		if c.telemetry.CostPer1KTokens < minCost {
			minCost = c.telemetry.CostPer1KTokens
		}
		if c.telemetry.CostPer1KTokens > maxCost {
			maxCost = c.telemetry.CostPer1KTokens
		}
	}

	latencyRange := maxLatency - minLatency
	costRange := maxCost - minCost

	for i := range candidates {
		c := &candidates[i]
		c.latencyScore = normalizeInvert(c.telemetry.AvgLatencyMs, minLatency, maxLatency, latencyRange)
		c.costScore = normalizeInvert(c.telemetry.CostPer1KTokens, minCost, maxCost, costRange)
		c.qualityScore = c.telemetry.SuccessRate
		if c.softSatisfied {
			c.capScore = 1.0
		} else if c.hardSatisfied {
			c.capScore = 0.6
		} else {
			c.capScore = 0.0
		}

		// Requests with complex content benefit from stronger capability matching.
		complexityBoost := 1.0
		if features.Complexity() > 1000 {
			complexityBoost = 1.2
		}

		switch strategy {
		case StrategyFast:
			c.finalScore = 0.45*c.latencyScore + 0.40*c.costScore + 0.10*c.qualityScore + 0.05*c.capScore*complexityBoost
		case StrategyQuality:
			c.finalScore = 0.15*c.latencyScore + 0.05*c.costScore + 0.40*c.qualityScore + 0.40*c.capScore*complexityBoost
		default: // StrategyAuto
			c.finalScore = 0.25*c.latencyScore + 0.25*c.costScore + 0.25*c.qualityScore + 0.25*c.capScore*complexityBoost
		}
	}

	// Sort by descending score, then by model id for deterministic tie-breaking.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].finalScore != candidates[j].finalScore {
			return candidates[i].finalScore > candidates[j].finalScore
		}
		return candidates[i].modelID < candidates[j].modelID
	})

	return candidates[0].modelID
}

func normalizeInvert(value, min, max, rng float64) float64 {
	if rng <= 0 || math.IsInf(rng, 0) || math.IsNaN(rng) {
		return 1.0
	}
	if value <= min {
		return 1.0
	}
	if value >= max {
		return 0.0
	}
	return (max - value) / rng
}
