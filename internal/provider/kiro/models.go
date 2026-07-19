package kiro

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

func genUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

const (
	kiroRuntimeSDKVersion = "1.0.0"
	kiroAgentOS           = "windows"
	kiroAgentOSVersion    = "10.0.26200"
	kiroNodeVersion       = "22.21.1"
	kiroIDEVersion        = "0.10.32"
	kiroDefaultRegion     = "us-east-1"
)

var (
	awsRegionPattern   = regexp.MustCompile(`^[a-z]{2}-[a-z]+-\d{1,2}$`)
	kiroProfileARNRe   = regexp.MustCompile(`^arn:aws:codewhisperer:([a-z0-9-]+):`)
	kiroProfileRegions = map[string]struct{}{"us-east-1": {}, "eu-central-1": {}}

	liveModelCacheTTL       = 5 * time.Minute
	liveModelCacheMu        sync.Mutex
	liveModelCache          = map[string]liveModelCacheEntry{}
	liveCatalogHTTPClient   = &http.Client{Timeout: 15 * time.Second}
	liveModelsEndpointBase  = "" // test override; when set, used instead of AWS URLs
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
	matches := kiroProfileARNRe.FindStringSubmatch(profileArn)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func resolveKiroRuntimeRegion(psd map[string]string) string {
	fromArn := regionFromKiroProfileArn(psd["profileArn"])
	if fromArn != "" && awsRegionPattern.MatchString(fromArn) {
		return fromArn
	}
	stored := normalizeRegion(psd["region"])
	if stored != "" {
		if _, ok := kiroProfileRegions[stored]; ok {
			return stored
		}
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
	urls := []string{fmt.Sprintf("https://q.%s.amazonaws.com/ListAvailableModels", normalized)}
	if normalized != kiroDefaultRegion {
		urls = append(urls, fmt.Sprintf("https://q.%s.amazonaws.com/ListAvailableModels", kiroDefaultRegion))
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
		"User-Agent":                 userAgent,
		"x-amz-user-agent":           fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s-%s", kiroRuntimeSDKVersion, kiroIDEVersion, machineID),
		"x-amzn-kiro-agent-mode":     "vibe",
		"x-amzn-codewhisperer-optout": "true",
		"amz-sdk-request":            "attempt=1; max=1",
		"amz-sdk-invocation-id":      genUUID(),
		"Accept":                     "application/json",
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
			variant.Description = toNonEmptyString(item["description"])
			expanded = append(expanded, variant)
		}
	}
	return expanded
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
		Capabilities: Capabilities{},
		VariantSuffix:  "",
	}
	variants := []Model{base}
	variants = append(variants, Model{
		BaseModel: BaseModel{
			ID:          safe + "-thinking",
			DisplayName: displayName + " (Thinking)",
		},
		Capabilities: Capabilities{Thinking: true},
		VariantSuffix:  "thinking",
	})
	if !isAuto {
		variants = append(variants, Model{
			BaseModel: BaseModel{
				ID:          safe + "-agentic",
				DisplayName: displayName + " (Agentic)",
			},
			Capabilities: Capabilities{Agentic: true},
			VariantSuffix:  "agentic",
		}, Model{
			BaseModel: BaseModel{
				ID:          safe + "-thinking-agentic",
				DisplayName: displayName + " (Thinking + Agentic)",
			},
			Capabilities: Capabilities{Thinking: true, Agentic: true},
			VariantSuffix:  "thinking-agentic",
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
