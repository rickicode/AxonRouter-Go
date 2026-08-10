package quota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultJobTokenExchangeURL = "https://openapi.qoder.sh/api/v1/jobToken/exchange"
	defaultUserStatusURL       = "https://openapi.qoder.sh/api/v3/user/status"
	defaultJobTokenTTL         = 23 * time.Hour
	minJobTokenTTL             = time.Minute
)

// qoderJobTokenCache is a short-lived, in-memory cache keyed by sha256(PAT).
// Job tokens are not persisted to the database.
var qoderJobTokenCache sync.Map // key: string (hex sha256) -> *qoderJobTokenCacheEntry

type qoderJobTokenCacheEntry struct {
	jobToken  string
	expiresAt time.Time
}

// getQoderJobTokenExchangeURL returns the configured PAT->job-token exchange URL.
// Defaults to the public Qoder endpoint but can be overridden via
// QODER_JOB_TOKEN_EXCHANGE_URL for tests or private deployments.
func getQoderJobTokenExchangeURL() string {
	if v := strings.TrimSpace(os.Getenv("QODER_JOB_TOKEN_EXCHANGE_URL")); v != "" {
		return v
	}
	return defaultJobTokenExchangeURL
}

// getQoderUserStatusURL returns the configured /user/status URL.
// Defaults to the public Qoder endpoint but can be overridden via
// QODER_USER_STATUS_URL for tests or private deployments.
func getQoderUserStatusURL() string {
	if v := strings.TrimSpace(os.Getenv("QODER_USER_STATUS_URL")); v != "" {
		return v
	}
	return defaultUserStatusURL
}

func qoderTokenCacheKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// resolveQoderJobToken resolves a token for use with Qoder HTTP APIs.
//   - Empty tokens return an error.
//   - Tokens already starting with "jt-" are returned unchanged.
//   - Tokens starting with "pt-" are exchanged for a short-lived job token.
//     Exchange failures gracefully fall back to the original token.
//   - Any other token is returned unchanged.
func resolveQoderJobToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("qoder: empty token")
	}
	if strings.HasPrefix(token, "jt-") {
		return token, nil
	}
	if !strings.HasPrefix(token, "pt-") {
		return token, nil
	}

	now := time.Now()
	key := qoderTokenCacheKey(token)
	if cached, ok := qoderJobTokenCache.Load(key); ok {
		if entry, ok := cached.(*qoderJobTokenCacheEntry); ok && entry.expiresAt.After(now) {
			return entry.jobToken, nil
		}
	}

	jobToken, ttl, _, err := exchangeQoderJobToken(token)
	if err != nil || jobToken == "" {
		// Graceful fallback: if the exchange fails for any reason, return the
		// original PAT so callers can try the downstream API directly.
		return token, nil
	}

	if ttl < minJobTokenTTL {
		ttl = minJobTokenTTL
	}
	qoderJobTokenCache.Store(key, &qoderJobTokenCacheEntry{
		jobToken:  jobToken,
		expiresAt: now.Add(ttl),
	})

	return jobToken, nil
}

func exchangeQoderJobToken(pat string) (jobToken string, ttl time.Duration, statusCode int, err error) {
	body, err := json.Marshal(map[string]string{"personal_token": pat})
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal exchange request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, getQoderJobTokenExchangeURL(), bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("qoder job token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", 0, resp.StatusCode, fmt.Errorf("qoder job token exchange %d: %s", resp.StatusCode, string(respBody))
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", 0, http.StatusOK, fmt.Errorf("parse exchange response: %w", err)
	}

	jobToken = findQoderJobToken(data)
	if jobToken == "" {
		return "", 0, http.StatusOK, fmt.Errorf("qoder job token response missing jt-* token")
	}

	ttl = defaultJobTokenTTL
	if exp := findQoderExpiresIn(data); exp > 0 {
		ttl = time.Duration(exp) * time.Second
	}
	if ttl < minJobTokenTTL {
		ttl = minJobTokenTTL
	}

	return jobToken, ttl, http.StatusOK, nil
}

func findQoderJobToken(data map[string]any) string {
	nested, _ := data["data"].(map[string]any)
	candidates := []string{
		getStringField(data, "job_token"),
		getStringField(data, "jobToken"),
		getStringField(data, "jt"),
		getStringField(data, "token"),
		getStringField(nested, "job_token"),
		getStringField(nested, "jobToken"),
		getStringField(nested, "jt"),
		getStringField(nested, "token"),
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.HasPrefix(c, "jt-") {
			return c
		}
	}
	return ""
}

func findQoderExpiresIn(data map[string]any) int64 {
	nested, _ := data["data"].(map[string]any)
	for _, m := range []map[string]any{data, nested} {
		if v, ok := getNumberFieldOK(m, "expires_in", "expiresIn"); ok && v > 0 {
			return int64(v)
		}
	}
	return 0
}

// fetchQoderQuota fetches Qoder account quota from /api/v3/user/status.
// Token resolution priority:
//  1. accessToken argument
//  2. psd["qoder_pat"]
//  3. psd["api_key"]
func fetchQoderQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	token := strings.TrimSpace(accessToken)
	if token == "" && psd != nil {
		if s := stringAny(psd["qoder_pat"]); s != "" {
			token = s
		} else if s := stringAny(psd["api_key"]); s != "" {
			token = s
		}
	}
	if token == "" {
		return nil, "", fmt.Errorf("qoder: no access token")
	}

	resolvedToken, err := resolveQoderJobToken(token)
	if err != nil {
		return nil, "", err
	}

	req, err := http.NewRequest(http.MethodGet, getQoderUserStatusURL(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create user status request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+resolvedToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("qoder user status api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read qoder user status: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", fmt.Errorf("qoder token expired or access denied (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("qoder user status api %d: %s", resp.StatusCode, string(respBody))
	}

	var status map[string]any
	if err := json.Unmarshal(respBody, &status); err != nil {
		return nil, "", fmt.Errorf("parse qoder user status: %w", err)
	}

	return parseQoderUserStatus(status)
}

func parseQoderUserStatus(status map[string]any) ([]QuotaItem, string, error) {
	userType := strings.ToLower(stringAny(status["userType"]))
	plan := stringAny(status["plan"])
	userTag := stringAny(status["userTag"])
	planLabel := prettifyQoderPlan(plan, userTag)

	isExceeded := false
	if b, ok := status["isQuotaExceeded"].(bool); ok {
		isExceeded = b
	}

	quota := getNumberField(status, "quota")
	resetAt := parseQoderResetTime(status["nextResetAt"])

	isPooled := userType == "teams" || userType == "enterprise" || quota <= 0

	var items []QuotaItem
	switch {
	case isExceeded:
		items = append(items, QuotaItem{
			Name:         "Quota",
			Used:         quota,
			Total:        quota,
			RemainingPct: 0,
			ResetAt:      resetAt,
			Unlimited:    false,
		})
	case isPooled:
		items = append(items, QuotaItem{
			Name:         "Plan",
			Used:         0,
			Total:        0,
			RemainingPct: 100,
			ResetAt:      resetAt,
			Unlimited:    true,
		})
	default:
		items = append(items, QuotaItem{
			Name:         "Requests",
			Used:         0,
			Total:        quota,
			RemainingPct: 100,
			ResetAt:      resetAt,
			Unlimited:    false,
		})
	}

	return items, planLabel, nil
}

func parseQoderResetTime(v any) string {
	if v == nil {
		return ""
	}

	parseUnix := func(n int64) string {
		if n <= 0 {
			return ""
		}
		// Upstream may send seconds or milliseconds. Heuristic: timestamps
		// below 1e12 are in seconds; larger values are already milliseconds.
		if n < 1e12 {
			n *= 1000
		}
		return time.UnixMilli(n).UTC().Format(time.RFC3339)
	}

	switch n := v.(type) {
	case float64:
		return parseUnix(int64(n))
	case int:
		return parseUnix(int64(n))
	case int64:
		return parseUnix(n)
	case json.Number:
		s := n.String()
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return parseUnix(i)
		}
		return parseQoderISOTime(s)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return ""
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return parseUnix(i)
		}
		return parseQoderISOTime(s)
	default:
		return ""
	}
}

func parseQoderISOTime(s string) string {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Unix() <= 0 {
				return ""
			}
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func prettifyQoderPlan(plan, userTag string) string {
	if tag := strings.TrimSpace(userTag); tag != "" {
		return tag
	}
	stripped := strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(plan), "PLAN_TIER_", ""))
	if stripped == "" {
		return "Qoder"
	}
	return qoderToTitleCase(stripped)
}

func qoderToTitleCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '_' })
	for i, w := range words {
		runes := []rune(w)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToTitle(runes[0])
		for j := 1; j < len(runes); j++ {
			runes[j] = unicode.ToLower(runes[j])
		}
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func clearQoderJobTokenCache() {
	qoderJobTokenCache.Range(func(key, value any) bool {
		qoderJobTokenCache.Delete(key)
		return true
	})
}

const defaultAPITokenValidationURL = "https://dashscope.aliyuncs.com/compatible-mode/v1/models"

func getQoderAPITokenValidationURL() string {
	if v := strings.TrimSpace(os.Getenv("QODER_MODELS_VALIDATION_URL")); v != "" {
		return v
	}
	return defaultAPITokenValidationURL
}

// ResolveQoderJobToken exchanges a Qoder Personal Access Token (pt-*) for a
// short-lived job token (jt-*), using the in-memory cache when possible.
// It returns the job token, the HTTP status code from the exchange endpoint,
// and an error. Unlike the internal resolveQoderJobToken, it does not silently
// fall back so callers can distinguish auth failures from provider errors.
func ResolveQoderJobToken(pat string) (string, int, error) {
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return "", 0, fmt.Errorf("qoder: empty token")
	}
	if strings.HasPrefix(pat, "jt-") {
		return pat, http.StatusOK, nil
	}
	if !strings.HasPrefix(pat, "pt-") {
		return pat, http.StatusOK, nil
	}

	key := qoderTokenCacheKey(pat)
	now := time.Now()
	if cached, ok := qoderJobTokenCache.Load(key); ok {
		if entry, ok := cached.(*qoderJobTokenCacheEntry); ok && entry.expiresAt.After(now) {
			return entry.jobToken, http.StatusOK, nil
		}
		qoderJobTokenCache.Delete(key)
	}

	jobToken, ttl, status, err := exchangeQoderJobToken(pat)
	if err != nil || jobToken == "" {
		if status == 0 {
			status = http.StatusBadGateway
		}
		return "", status, err
	}
	if ttl < minJobTokenTTL {
		ttl = minJobTokenTTL
	}
	qoderJobTokenCache.Store(key, &qoderJobTokenCacheEntry{
		jobToken:  jobToken,
		expiresAt: now.Add(ttl),
	})
	return jobToken, status, nil
}

// CheckQoderAPIToken performs a lightweight GET to the DashScope
// OpenAI-compatible models endpoint to validate a non-PAT Qoder token.
// The returned error message intentionally omits the token.
func CheckQoderAPIToken(ctx context.Context, token string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, fmt.Errorf("qoder: empty token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getQoderAPITokenValidationURL(), nil)
	if err != nil {
		return 0, fmt.Errorf("create qoder validation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("qoder token validation request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return resp.StatusCode, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, fmt.Errorf("qoder API token rejected (HTTP %d)", resp.StatusCode)
	}
	return resp.StatusCode, fmt.Errorf("qoder API token validation failed (HTTP %d)", resp.StatusCode)
}

// ClearQoderJobTokenCache removes all cached Qoder job tokens. It is exposed
// primarily for tests and administrative resets.
func ClearQoderJobTokenCache() {
	clearQoderJobTokenCache()
}
