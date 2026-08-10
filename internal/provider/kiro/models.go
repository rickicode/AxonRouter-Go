package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	kiroRuntimeSDKVersion = "1.0.0"
	kiroAgentOS           = "windows"
	kiroAgentOSVersion    = "10.0.26200"
	kiroNodeVersion       = "22.21.1"
	kiroIDEVersion        = "0.10.32"
	kiroDefaultRegion     = "us-east-1"

	awsQHostTemplate        = "q.%s.amazonaws.com"
	awsCodeWhispererDevHost = "codewhisperer.us-east-1.amazonaws.com"
)

var (
	liveModelCacheTTL      = 5 * time.Minute
	liveModelCacheMu       sync.Mutex
	liveModelCache         = map[string]liveModelCacheEntry{}
	liveCatalogHTTPClient  = &http.Client{Timeout: 15 * time.Second}
	liveModelsEndpointBase = "" // test override; when set, used instead of AWS URLs
)

type liveModelCacheEntry struct {
	expiresAt time.Time
	models    []Model
}

// LiveModelResult is the result of a live Kiro model catalog fetch.
type LiveModelResult struct {
	Models []Model `json:"models"`
	Source string  `json:"source"` // "api" or "fallback"
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

func regionFromKiroProfileArn(profileArn string) string {
	if profileArn == "" {
		return ""
	}
	prefix := "arn:aws:codewhisperer:"
	if !strings.HasPrefix(profileArn, prefix) {
		return ""
	}
	rest := profileArn[len(prefix):]
	idx := strings.Index(rest, ":")
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}

// isValidAWSRegion validates the AWS region shape:
// two lowercase letters, hyphen, location, hyphen, digit(s).
func isValidAWSRegion(region string) bool {
	if region == "" {
		return false
	}
	parts := strings.Split(region, "-")
	if len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 2 {
		return false
	}
	for _, r := range parts[0] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	if parts[1] == "" {
		return false
	}
	for _, r := range parts[1] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	if parts[2] == "" {
		return false
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func resolveKiroRuntimeRegion(psd map[string]string) string {
	fromArn := regionFromKiroProfileArn(psd["profileArn"])
	if fromArn != "" && isValidAWSRegion(fromArn) {
		return fromArn
	}
	stored := normalizeRegion(psd["region"])
	if stored != "" && isValidAWSRegion(stored) {
		return stored
	}
	return kiroDefaultRegion
}

func buildKiroModelsEndpoints(region string) []string {
	if liveModelsEndpointBase != "" {
		return []string{liveModelsEndpointBase + "/ListAvailableModels"}
	}
	normalized := normalizeRegion(region)
	if normalized == "" {
		normalized = kiroDefaultRegion
	}
	urls := []string{fmt.Sprintf("https://"+awsQHostTemplate+"/ListAvailableModels", normalized)}
	if normalized != kiroDefaultRegion {
		urls = append(urls, fmt.Sprintf("https://"+awsQHostTemplate+"/ListAvailableModels", kiroDefaultRegion))
	}
	return urls
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func toNonEmptyString(v any) string {
	s := stringFromAny(v)
	if s == "" {
		return ""
	}
	return s
}

func cacheKey(accessToken string, psd map[string]any) string {
	seed := toNonEmptyString(psd["profileArn"])
	if seed == "" {
		seed = toNonEmptyString(psd["clientId"])
	}
	if seed == "" {
		seed = accessToken
	}
	if seed == "" {
		seed = "anonymous"
	}
	h := sha256.Sum256([]byte("kiro:" + seed))
	return hex.EncodeToString(h[:])
}

func buildKiroFingerprintHeaders(psd map[string]any, accessToken string) map[string]string {
	seed := toNonEmptyString(psd["clientId"])
	if seed == "" {
		seed = toNonEmptyString(psd["profileArn"])
	}
	if seed == "" {
		seed = accessToken
	}
	if seed == "" {
		seed = "kiro-anonymous"
	}
	h := sha256.Sum256([]byte(seed))
	machineID := hex.EncodeToString(h[:])
	userAgent := fmt.Sprintf(
		"aws-sdk-js/%s ua/2.1 os/%s#%s lang/js md/nodejs#%s api/codewhispererruntime#%s m/N,E KiroIDE-%s-%s",
		kiroRuntimeSDKVersion, kiroAgentOS, kiroAgentOSVersion, kiroNodeVersion, kiroRuntimeSDKVersion, kiroIDEVersion, machineID,
	)
	return map[string]string{
		"User-Agent":                  userAgent,
		"x-amz-user-agent":            fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s-%s", kiroRuntimeSDKVersion, kiroIDEVersion, machineID),
		"x-amzn-kiro-agent-mode":      "vibe",
		"x-amzn-codewhisperer-optout": "true",
		"amz-sdk-request":             "attempt=1; max=1",
		"amz-sdk-invocation-id":       genUUID(),
		"Accept":                      "application/json",
	}
}

func tryFetchModels(url, accessToken string, psd map[string]any) ([]Model, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range buildKiroFingerprintHeaders(psd, accessToken) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := liveCatalogHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	models := expandLiveModels(data)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models in response")
	}
	return models, nil
}

func expandLiveModels(data map[string]any) []Model {
	var rawItems []any
	if m, ok := data["models"].([]any); ok {
		rawItems = m
	} else if m, ok := data["models"].([]map[string]any); ok {
		rawItems = make([]any, len(m))
		for i, v := range m {
			rawItems[i] = v
		}
	} else if m, ok := data["availableModels"].([]any); ok {
		rawItems = m
	}

	seen := make(map[string]struct{})
	var expanded []Model
	for _, v := range rawItems {
		var item map[string]any
		switch x := v.(type) {
		case map[string]any:
			item = x
		default:
			continue
		}
		upstreamID := toNonEmptyString(item["modelId"])
		if upstreamID == "" {
			upstreamID = toNonEmptyString(item["id"])
		}
		if upstreamID == "" {
			continue
		}

		displayName := formatDisplayName(item["modelName"], upstreamID, item["rateMultiplier"])
		tokenLimits, _ := item["tokenLimits"].(map[string]any)
		contextLength := 200000
		if maxIn, ok := tokenLimits["maxInputTokens"].(float64); ok && maxIn > 0 {
			contextLength = int(maxIn)
		}
		rateMultiplier := 1.0
		if rm, ok := item["rateMultiplier"].(float64); ok && rm > 0 {
			rateMultiplier = rm
		}

		// Preserve manually-curated metadata for known upstream IDs when available.
		caps := baseCapabilities(upstreamID)
		strip := baseStrip(upstreamID)
		desc := baseDescription(upstreamID)
		if desc == "" {
			desc = toNonEmptyString(item["description"])
		}

		variants := buildLiveVariants(upstreamID, displayName)
		for _, variant := range variants {
			if _, ok := seen[variant.ID]; ok {
				continue
			}
			seen[variant.ID] = struct{}{}
			variant.ContextLength = contextLength
			variant.RateMultiplier = rateMultiplier
			variant.UpstreamModelID = upstreamID
			variant.OwnedBy = "kiro"
			variant.Description = desc
			// Merge base capabilities into variant capabilities. Live variants already
			// set Thinking/Agentic; we overlay Vision/Reasoning/Search from curation.
			variant.Capabilities = mergeCapabilities(variant.Capabilities, caps)
			variant.Strip = strip
			expanded = append(expanded, variant)
		}
	}
	return expanded
}

// mergeCapabilities copies base-level Vision/Reasoning/Search flags into the
// variant capability set while preserving synthetic Thinking/Agentic flags.
func mergeCapabilities(variant Capabilities, base Capabilities) Capabilities {
	variant.Vision = variant.Vision || base.Vision
	variant.Reasoning = variant.Reasoning || base.Reasoning
	variant.Search = variant.Search || base.Search
	return variant
}

func stripSyntheticSuffixes(id string) string {
	out := id
	if strings.HasSuffix(out, "-thinking-agentic") {
		out = strings.TrimSuffix(out, "-thinking-agentic")
	}
	if strings.HasSuffix(out, "-agentic") {
		out = strings.TrimSuffix(out, "-agentic")
	}
	if strings.HasSuffix(out, "-thinking") {
		out = strings.TrimSuffix(out, "-thinking")
	}
	return out
}

func formatDisplayName(modelName any, modelID string, rateMultiplier any) string {
	base := toNonEmptyString(modelName)
	if base == "" {
		base = modelID
	}
	rate := 1.0
	if f, ok := rateMultiplier.(float64); ok {
		rate = f
	}
	if rate <= 0 || rate == 1.0 {
		return fmt.Sprintf("Kiro %s", base)
	}
	return fmt.Sprintf("Kiro %s (%.1fx credit)", base, rate)
}

func buildLiveVariants(upstream, displayName string) []Model {
	safe := stripSyntheticSuffixes(upstream)
	isAuto := safe == "auto" || safe == "auto-kiro"
	base := Model{
		BaseModel: BaseModel{
			ID:          safe,
			DisplayName: displayName,
		},
		Capabilities:  Capabilities{},
		VariantSuffix: "",
	}
	variants := []Model{base}
	variants = append(variants, Model{
		BaseModel: BaseModel{
			ID:          safe + "-thinking",
			DisplayName: displayName + " (Thinking)",
		},
		Capabilities:  Capabilities{Thinking: true},
		VariantSuffix: "thinking",
	})
	if !isAuto {
		variants = append(variants, Model{
			BaseModel: BaseModel{
				ID:          safe + "-agentic",
				DisplayName: displayName + " (Agentic)",
			},
			Capabilities:  Capabilities{Agentic: true},
			VariantSuffix: "agentic",
		}, Model{
			BaseModel: BaseModel{
				ID:          safe + "-thinking-agentic",
				DisplayName: displayName + " (Thinking + Agentic)",
			},
			Capabilities:  Capabilities{Thinking: true, Agentic: true},
			VariantSuffix: "thinking-agentic",
		})
	}
	return variants
}

func toFallbackResult() *LiveModelResult {
	return &LiveModelResult{
		Models: ExpandVariants(BaseModels),
		Source: "fallback",
	}
}

func psdToStringMap(psd map[string]any) map[string]string {
	out := make(map[string]string, len(psd))
	for k, v := range psd {
		out[k] = toNonEmptyString(v)
	}
	return out
}

// FetchLiveModels discovers the Kiro model catalog live via ListAvailableModels,
// falling back to the static catalog when no token is available or every attempt
// fails. Results are cached for 5 minutes keyed by token/profileArn/clientId.
func FetchLiveModels(accessToken string, psd map[string]any) (*LiveModelResult, error) {
	if psd == nil {
		psd = map[string]any{}
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return toFallbackResult(), nil
	}

	key := cacheKey(token, psd)
	liveModelCacheMu.Lock()
	if cached, ok := liveModelCache[key]; ok && time.Now().Before(cached.expiresAt) {
		liveModelCacheMu.Unlock()
		cp := make([]Model, len(cached.models))
		copy(cp, cached.models)
		return &LiveModelResult{Models: cp, Source: "api"}, nil
	}
	liveModelCacheMu.Unlock()

	region := resolveKiroRuntimeRegion(psdToStringMap(psd))
	endpoints := buildKiroModelsEndpoints(region)
	profileArn := toNonEmptyString(psd["profileArn"])

	// Pass 1: origin-only (works for Builder ID / social / IdC).
	for _, base := range endpoints {
		url := base + "?origin=AI_EDITOR"
		if models, err := tryFetchModels(url, token, psd); err == nil {
			return cacheAndReturn(key, models), nil
		}
	}

	// Pass 2: retry with profileArn on the primary regional endpoint.
	if profileArn != "" {
		url := fmt.Sprintf("%s?origin=AI_EDITOR&profileArn=%s", endpoints[0], urlEncode(profileArn))
		if models, err := tryFetchModels(url, token, psd); err == nil {
			return cacheAndReturn(key, models), nil
		}
	}

	return toFallbackResult(), nil
}

func cacheAndReturn(key string, models []Model) *LiveModelResult {
	liveModelCacheMu.Lock()
	liveModelCache[key] = liveModelCacheEntry{
		expiresAt: time.Now().Add(liveModelCacheTTL),
		models:    models,
	}
	liveModelCacheMu.Unlock()
	cp := make([]Model, len(models))
	copy(cp, models)
	return &LiveModelResult{Models: cp, Source: "api"}
}

// ClearLiveModelCache removes all cached live catalog entries. Primarily used by tests.
func ClearLiveModelCache() {
	liveModelCacheMu.Lock()
	liveModelCache = map[string]liveModelCacheEntry{}
	liveModelCacheMu.Unlock()
}

func urlEncode(s string) string {
	return strings.ReplaceAll(s, "&", "%26")
}
