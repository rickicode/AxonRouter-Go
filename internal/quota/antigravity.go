package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/models"
)

var antigravityFetchURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
}

var antigravityRetrieveQuotaURLs = []string{
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota",
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuota",
}

var antigravityLoadURLs = []string{
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:loadCodeAssist",
	"https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
	"https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
}

const (
	antigravityUserAgent = "Antigravity/1.0.0 (Linux) Chrome/132.0.6834.160 Electron/39.2.3"
	antigravityLoadUA    = "vscode/1.X.X (Antigravity/1.0.0)"
)

// upstreamQuotaBucketToClient maps upstream Antigravity quota-bucket model IDs
// to client-visible tier IDs. Matches OmniRoute's ANTIGRAVITY_QUOTA_BUCKET_TO_CLIENT.
var upstreamQuotaBucketToClient = map[string]string{
	"gemini-3.5-flash-extra-low": "gemini-3.5-flash-low",
	"gemini-3.5-flash-low":       "gemini-3.5-flash-medium",
	"gemini-3-flash-agent":       "gemini-3.5-flash-high",
}

// upstreamReverseAliases maps upstream model IDs back to client-visible IDs.
var upstreamReverseAliases = map[string]string{
	"gemini-3.1-pro":     "gemini-3-pro-preview",
	"gemini-3-pro-image": "gemini-3-pro-image-preview",
	"rev19-uic3-1p":      "gemini-2.5-computer-use-preview-10-2025",
}

// droppedQuotaBuckets are retired/hidden upstream preview buckets to filter out.
var droppedQuotaBuckets = map[string]bool{
	"gemini-3.5-flash-preview": true,
	"gemini-3-flash-preview":   true,
}

// internalModelSignals are substrings that indicate an internal/test model.
var internalModelSignals = []string{"internal", "test", "tab_flash", "tab_jump"}

// antigravityDisplayNames caches display names from models.json.
var (
	antigravityDisplayNamesOnce sync.Once
	antigravityDisplayNames     map[string]string
)

func getAntigravityDisplayNames() map[string]string {
	antigravityDisplayNamesOnce.Do(func() {
		antigravityDisplayNames = models.GetModelDisplayNames("antigravity")
	})
	return antigravityDisplayNames
}

// toClientQuotaModelId maps an upstream quota-bucket model ID to the client-visible
// tier ID, or "" if the bucket should be hidden from clients.
func toClientQuotaModelId(modelId string) string {
	if modelId == "" {
		return ""
	}
	if droppedQuotaBuckets[modelId] {
		return ""
	}
	if client, ok := upstreamQuotaBucketToClient[modelId]; ok {
		return client
	}
	if client, ok := upstreamReverseAliases[modelId]; ok {
		return client
	}
	return modelId
}

// antigravityQuotaBucket represents a single model quota from retrieveUserQuota.
type antigravityQuotaBucket struct {
	ModelId           string `json:"modelId"`
	RemainingFraction any    `json:"remainingFraction"` // float64 or nil
	ResetTime         any    `json:"resetTime"`         // string or number
}

// FetchAntigravityQuota fetches per-model quota from the Antigravity retrieveUserQuota
// endpoint. Connection-specific: uses accessToken and providerSpecificData["projectId"].
func FetchAntigravityQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	plan := fetchAntigravityPlan(accessToken)
	quotas, err := fetchAntigravityModels(accessToken, psd)
	if err != nil {
		return nil, plan, err
	}
	return quotas, plan, nil
}

// fetchAntigravityQuota is the internal entry point called by the quota scheduler.
func fetchAntigravityQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	return FetchAntigravityQuota(accessToken, psd)
}

func fetchAntigravityPlan(accessToken string) string {
	body := `{"metadata":{"ideType":"ANTIGRAVITY"}}`

	for _, url := range antigravityLoadURLs {
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityLoadUA)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(respBody, &data); err != nil {
			continue
		}

		return mapCodeAssistToPlanLabel(data)
	}
	return ""
}

// fetchAntigravityModels fetches per-model quota by calling both fetchAvailableModels
// (catalog with quotaInfo) and retrieveUserQuota (live usage buckets), then merging.
// retrieveUserQuota data takes precedence when available.
func fetchAntigravityModels(accessToken string, psd map[string]any) ([]QuotaItem, error) {
	projectID, _ := psd["projectId"].(string)

	type modelsResult struct {
		data map[string]any
		err  error
	}
	type quotaResult struct {
		buckets []antigravityQuotaBucket
		err     error
	}

	modelsCh := make(chan modelsResult, 1)
	quotaCh := make(chan quotaResult, 1)

	go func() {
		data, err := antigravityFetchModelsAPI(accessToken, projectID)
		modelsCh <- modelsResult{data, err}
	}()

	go func() {
		buckets, err := antigravityRetrieveUserQuotaAPI(accessToken, projectID)
		quotaCh <- quotaResult{buckets, err}
	}()

	mr := <-modelsCh
	qr := <-quotaCh

	// Build retrieveUserQuota lookup: clientModelId → bucket
	quotaBuckets := make(map[string]antigravityQuotaBucket)
	if qr.err == nil {
		for _, bucket := range qr.buckets {
			clientId := toClientQuotaModelId(bucket.ModelId)
			if clientId == "" {
				continue
			}
			quotaBuckets[clientId] = bucket
		}
	}

	// If fetchAvailableModels failed, fall back to retrieveUserQuota-only
	if mr.err != nil && mr.data == nil {
		if qr.err != nil {
			// Surface specific errors (auth, projectId) clearly instead of burying them
			qMsg := qr.err.Error()
			if strings.Contains(qMsg, "no projectId") || strings.Contains(qMsg, "token expired") || strings.Contains(qMsg, "access denied") {
				return nil, qr.err
			}
			return nil, fmt.Errorf("antigravity api unavailable: models=%v, quota=%v", mr.err, qr.err)
		}
		return parseQuotaBucketsOnly(quotaBuckets), nil
	}

	return parseAntigravityQuotaMerged(mr.data, quotaBuckets, getAntigravityDisplayNames()), nil
}

// antigravityFetchModelsAPI calls fetchAvailableModels and returns the raw response.
func antigravityFetchModelsAPI(accessToken, projectID string) (map[string]any, error) {
	reqBody := "{}"
	if projectID != "" {
		reqBody = fmt.Sprintf(`{"project":"%s"}`, projectID)
	}

	var lastErr error
	for _, url := range antigravityFetchURLs {
		req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityUserAgent)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("antigravity token expired or access denied (HTTP %d)", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("antigravity api error %d: %s", resp.StatusCode, string(body))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("antigravity api unavailable")
}

// antigravityRetrieveUserQuotaAPI calls retrieveUserQuota and returns parsed buckets.
func antigravityRetrieveUserQuotaAPI(accessToken, projectID string) ([]antigravityQuotaBucket, error) {
	if projectID == "" {
		return nil, fmt.Errorf("no projectId for retrieveUserQuota")
	}
	reqBody := fmt.Sprintf(`{"project":"%s"}`, projectID)

	var lastErr error
	for _, url := range antigravityRetrieveQuotaURLs {
		req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", antigravityUserAgent)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("antigravity token expired or access denied (HTTP %d)", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("retrieveUserQuota error %d: %s", resp.StatusCode, string(body))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var result struct {
			Buckets []antigravityQuotaBucket `json:"buckets"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = fmt.Errorf("retrieveUserQuota decode: %w", err)
			continue
		}
		return result.Buckets, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("retrieveUserQuota api unavailable")
}

// parseAntigravityQuotaMerged merges fetchAvailableModels catalog data with
// retrieveUserQuota live data. retrieveUserQuota takes precedence.
func parseAntigravityQuotaMerged(
	data map[string]any,
	quotaBuckets map[string]antigravityQuotaBucket,
	displayNames map[string]string,
) []QuotaItem {
	models, ok := data["models"].(map[string]any)
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	var quotas []QuotaItem

	for rawModelKey, infoVal := range models {
		info, ok := infoVal.(map[string]any)
		if !ok {
			continue
		}

		if isInternal, ok := info["isInternal"].(bool); ok && isInternal {
			continue
		}

		modelKey := normalizeModelID(rawModelKey)
		if modelKey == "" || isInternalModel(modelKey) {
			continue
		}

		quotaInfo, _ := info["quotaInfo"].(map[string]any)
		if quotaInfo == nil {
			quotaInfo = map[string]any{}
		}

		// Prefer retrieveUserQuota data over catalog quotaInfo
		quotaSource := quotaInfo
		if bucket, ok := quotaBuckets[modelKey]; ok {
			quotaSource = map[string]any{
				"remainingFraction": bucket.RemainingFraction,
				"resetTime":         bucket.ResetTime,
			}
		}

		remainingFraction := getNumberField(quotaSource, "remainingFraction", "remaining_fraction")
		if remainingFraction <= 0 && len(quotaInfo) == 0 {
			continue
		}
		resetAt := parseResetTimeString(quotaSource)

		isUnlimited := remainingFraction >= 1.0 && resetAt == ""

		total := 1000.0
		remaining := total * remainingFraction
		used := total - remaining
		if isUnlimited {
			used = 0
			total = 0
		}

		// Display name: prefer catalog displayName, fallback to models.json
		displayName := getStringField(info, "displayName", "display_name")
		if displayName == "" {
			if name, ok := displayNames[modelKey]; ok && name != "" {
				displayName = name
			} else {
				displayName = modelKey
			}
		}

		seen[modelKey] = true
		quotas = append(quotas, QuotaItem{
			Name:         displayName,
			Used:         used,
			Total:        total,
			RemainingPct: remainingFraction * 100,
			ResetAt:      resetAt,
			Unlimited:    isUnlimited,
			ModelKey:     modelKey,
			Family:       "antigravity",
		})
	}

	// Include retrieveUserQuota-only models not in the catalog
	for modelKey, bucket := range quotaBuckets {
		if seen[modelKey] {
			continue
		}
		if isInternalModel(modelKey) {
			continue
		}

		remainingFraction := toFloat64(bucket.RemainingFraction)
		resetAt := parseResetTimeString(map[string]any{
			"resetTime": bucket.ResetTime,
		})

		isUnlimited := remainingFraction >= 1.0 && resetAt == ""

		total := 1000.0
		remaining := total * remainingFraction
		used := total - remaining
		if isUnlimited {
			used = 0
			total = 0
		}

		displayName := modelKey
		if name, ok := displayNames[modelKey]; ok && name != "" {
			displayName = name
		}

		quotas = append(quotas, QuotaItem{
			Name:         displayName,
			Used:         used,
			Total:        total,
			RemainingPct: remainingFraction * 100,
			ResetAt:      resetAt,
			Unlimited:    isUnlimited,
			ModelKey:     modelKey,
			Family:       "antigravity",
		})
	}

	return quotas
}

// parseQuotaBucketsOnly builds QuotaItems from retrieveUserQuota data alone
// when fetchAvailableModels is unavailable.
func parseQuotaBucketsOnly(quotaBuckets map[string]antigravityQuotaBucket) []QuotaItem {
	displayNames := getAntigravityDisplayNames()
	var quotas []QuotaItem

	for modelKey, bucket := range quotaBuckets {
		if isInternalModel(modelKey) {
			continue
		}

		remainingFraction := toFloat64(bucket.RemainingFraction)
		resetAt := parseResetTimeString(map[string]any{
			"resetTime": bucket.ResetTime,
		})

		isUnlimited := remainingFraction >= 1.0 && resetAt == ""

		total := 1000.0
		remaining := total * remainingFraction
		used := total - remaining
		if isUnlimited {
			used = 0
			total = 0
		}

		displayName := modelKey
		if name, ok := displayNames[modelKey]; ok && name != "" {
			displayName = name
		}

		quotas = append(quotas, QuotaItem{
			Name:         displayName,
			Used:         used,
			Total:        total,
			RemainingPct: remainingFraction * 100,
			ResetAt:      resetAt,
			Unlimited:    isUnlimited,
			ModelKey:     modelKey,
			Family:       "antigravity",
		})
	}

	return quotas
}

// toFloat64 safely converts any to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// isInternalModel returns true if the model key contains internal/test indicators.
func isInternalModel(modelKey string) bool {
	lower := strings.ToLower(modelKey)
	for _, sig := range internalModelSignals {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

func classifyModelFamily(modelKey string) string {
	lower := strings.ToLower(modelKey)
	if strings.Contains(lower, "gemini") {
		return "gemini"
	}
	if strings.Contains(lower, "claude") {
		return "claude"
	}
	return "other"
}

func normalizeModelID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "internal") || strings.Contains(lower, "test") {
		return ""
	}
	return s
}

func mapCodeAssistToPlanLabel(data map[string]any) string {
	sub, ok := data["subscriptionInfo"].(map[string]any)
	if !ok {
		if tier, ok := data["tierId"].(string); ok {
			return mapTierToLabel(tier)
		}
		if tier, ok := data["subscriptionTier"].(string); ok {
			return mapTierStringToLabel(tier)
		}
		return ""
	}

	if tier, ok := sub["tierId"].(string); ok {
		if label := mapTierToLabel(tier); label != "" {
			return label
		}
	}

	if tier, ok := sub["subscriptionTier"].(string); ok {
		if label := mapTierStringToLabel(tier); label != "" {
			return label
		}
	}

	return ""
}

func mapTierToLabel(tierID string) string {
	lower := strings.ToLower(tierID)
	switch {
	case strings.Contains(lower, "free"):
		return "Free"
	case strings.Contains(lower, "pro") || strings.Contains(lower, "standard"):
		return "Pro"
	case strings.Contains(lower, "enterprise") || strings.Contains(lower, "business"):
		return "Enterprise"
	case strings.Contains(lower, "ultra") || strings.Contains(lower, "premium"):
		return "Ultra"
	}
	return ""
}

func mapTierStringToLabel(tier string) string {
	lower := strings.ToLower(tier)
	switch {
	case strings.Contains(lower, "free"):
		return "Free"
	case strings.Contains(lower, "pro"):
		return "Pro"
	case strings.Contains(lower, "enterprise"):
		return "Enterprise"
	case strings.Contains(lower, "ultra"):
		return "Ultra"
	}
	return ""
}

func parseResetTimeString(m map[string]any) string {
	if rt, ok := m["resetTime"].(string); ok && rt != "" {
		return rt
	}
	if rt, ok := m["reset_time"].(string); ok && rt != "" {
		return rt
	}
	if rn, ok := m["resetTime"].(float64); ok && rn > 0 {
		return time.UnixMilli(int64(rn)).UTC().Format(time.RFC3339)
	}
	if rn, ok := m["reset_time"].(float64); ok && rn > 0 {
		return time.UnixMilli(int64(rn)).UTC().Format(time.RFC3339)
	}
	return ""
}
