package connstate

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
)

// ErrorCategory classifies the type of error for circuit breaker decisions.
type ErrorCategory string

const (
	ErrorNone          ErrorCategory = ""
	ErrorAuth          ErrorCategory = "auth"
	ErrorRateLimit     ErrorCategory = "rate_limit"
	ErrorQuota         ErrorCategory = "quota"
	ErrorBalanceEmpty  ErrorCategory = "balance_empty"
	ErrorServer        ErrorCategory = "server"
	ErrorTimeout       ErrorCategory = "timeout"
	ErrorNetwork       ErrorCategory = "network"
	ErrorModelNotFound ErrorCategory = "model_not_found"
	ErrorContentFilter ErrorCategory = "content_filter"
	ErrorUnknown       ErrorCategory = "unknown"
)

// ErrorDetection holds the result of error detection.
type ErrorDetection struct {
	Category       ErrorCategory
	Retryable      bool
	Message        string
	Status         Status
	CooldownUntil  *time.Time
	Scope          string // "connection" or "model"
	ModelID        string // model ID if scope is "model"
	DisabledReason string // short reason when Status == StatusDisabled (e.g. "auth_failed")
}

// Detector detects error types from HTTP responses and error messages.
type Detector struct{}

// NewDetector creates a new error detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Standard cooldown windows used by the detector.
const (
	quotaCooldown             = 30 * time.Minute
	authCooldown              = 30 * time.Minute
	balanceEmptyCooldown      = 30 * time.Minute
	serverErrorCooldown       = 1 * time.Minute
	modelNotFoundCooldown     = 12 * time.Hour
	freeUsageCooldownWindow   = 24 * time.Hour
	grokSpendingLimitCooldown = 24 * time.Hour

	rateLimitBaseCooldown = 1 * time.Second
	rateLimitMaxCooldown  = 30 * time.Minute
)

// DetectError classifies an error from HTTP status code, response body, and headers.
// modelID is passed through so callers can identify which model was rate-limited.
// headers is used to extract rate limit info and retry-after values.
func DetectError(ctx context.Context, statusCode int, body string, err error, providerPrefix string, modelID string, headers http.Header) ErrorDetection {
	d := NewDetector()
	var msg string
	if err != nil {
		msg = err.Error()
	} else {
		msg = body
	}

	// A plain "context canceled" that is NOT the inbound request context
	// means the server-side fetch was cancelled (proxy/relay teardown,
	// upstream closing mid-flight). Treat it as a transient timeout so it
	// is retryable and marks the connection degraded. The handler still
	// short-circuits client-side cancellations before calling this.
	if errors.Is(err, context.Canceled) {
		until := time.Now().Add(serverErrorCooldown)
		return ErrorDetection{
			Category:      ErrorTimeout,
			Retryable:     true,
			Message:       msg,
			Status:        StatusDegraded,
			CooldownUntil: &until,
			Scope:         "connection",
			ModelID:       modelID,
		}
	}

	var upErr *executor.UpstreamError
	if errors.As(err, &upErr) {
		statusCode = upErr.StatusCode
		msg = string(upErr.RawBody)
		if len(msg) == 0 {
			msg = string(upErr.Body)
		}
		if headers == nil {
			headers = upErr.Headers
		}
	}

	// Build a normalized lower-case body/text sample that includes both raw
	// message text and structured JSON code/type fields. This keeps message
	// matching and gjson extraction on equal footing.
	sample := d.errorSample(statusCode, msg, headers)
	cat := d.ClassifyFromMessage(sample)
	if statusCode > 0 {
		cat = d.ClassifyFromResponse(statusCode, sample)
	}

	det := ErrorDetection{
		Category:  cat,
		Retryable: d.IsRetryable(cat),
		Message:   msg,
		Scope:     "connection",
		ModelID:   modelID,
	}

	lower := strings.ToLower(msg)
	switch cat {
	case ErrorRateLimit:
		det.Status = StatusRateLimited
		// Free-tier exhaustion is an account-wide quota event, not a per-minute
		// rate limit. Reclassify before computing the cooldown. A provider Retry-After
		// hint still takes precedence over the default 24h window.
		if statusCode == http.StatusTooManyRequests && isFreeUsageExhausted(lower) {
			det.Category = ErrorQuota
			cat = ErrorQuota
			det.Status = StatusQuotaExhausted
			det.CooldownUntil = freeUsageCooldown(lower, headers)
			break
		}
		det.CooldownUntil = rateLimitCooldown(lower, headers)

	case ErrorAuth:
		det.Status = StatusDisabled
		det.DisabledReason = "auth_failed"
		until := time.Now().Add(authCooldown)
		det.CooldownUntil = &until

	case ErrorBalanceEmpty:
		// Grok CLI returns 402 with a "personal-team-blocked:spending-limit" code
		// when the account hits its spending cap. This is recoverable by the user
		// adding credits or upgrading, so treat it as a quota cooldown instead of
		// permanently disabling the connection. Other balance-empty cases mean the
		// account has no funds/payment method and require manual top-up before
		// reuse.
		if providerPrefix == "grok-cli" && strings.Contains(lower, "spending-limit") {
			det.Category = ErrorQuota
			cat = ErrorQuota
			det.Status = StatusQuotaExhausted
			until := time.Now().Add(grokSpendingLimitCooldown)
			det.CooldownUntil = &until
		} else {
			det.Status = StatusDisabled
			det.DisabledReason = "balance_empty"
			until := time.Now().Add(balanceEmptyCooldown)
			det.CooldownUntil = &until
		}

	case ErrorQuota:
		det.Status = StatusQuotaExhausted
		det.CooldownUntil = quotaCooldownFor(lower, headers)

	case ErrorModelNotFound:
		// Model-level lockout: the requested model is not available on this
		// connection, but other models may still work.
		until := time.Now().Add(modelNotFoundCooldown)
		det.CooldownUntil = &until
		det.Scope = "model"

	case ErrorServer, ErrorTimeout, ErrorNetwork:
		det.Status = StatusDegraded
		until := time.Now().Add(serverErrorCooldown)
		det.CooldownUntil = &until
	}

	// Per-model quota/rate-limit: only oc/ag mark at model scope; other providers
	// keep connection-wide cooldown/exhaustion as before. Free-tier exhaustion is
	// account-wide, so it is never scoped to a single model.
	if cat == ErrorRateLimit || cat == ErrorQuota {
		if det.CooldownUntil == nil {
			// Fallback: a short cooldown so the connection is not immediately reused.
			until := time.Now().Add(rateLimitBaseCooldown)
			det.CooldownUntil = &until
		}
		if HasPerModelQuota(providerPrefix) && det.ModelID != "" {
			det.Scope = "model"
		}
	}

	return det
}

// errorSample builds a single lower-case string that blends the raw response
// text with structured error fields extracted via gjson. This lets both
// substring matching and JSON path extraction drive the same classifier.
func (d *Detector) errorSample(statusCode int, msg string, headers http.Header) string {
	parts := []string{strings.ToLower(msg), http.StatusText(statusCode)}
	for _, path := range []string{
		"error.code",
		"error.type",
		"error.error.code",
		"error.error.type",
		"code",
		"type",
		"error.message",
		"message",
	} {
		if v := gjson.Get(msg, path).String(); v != "" {
			parts = append(parts, strings.ToLower(v))
		}
	}
	return strings.Join(parts, " ")
}

// isFreeUsageExhausted returns true when a 429 body unambiguously indicates a
// daily/free-tier quota has been hit rather than a throttle/rate-limit.
func isFreeUsageExhausted(lower string) bool {
	return strings.Contains(lower, "free-usage-exhausted") ||
		strings.Contains(lower, "included free usage") ||
		strings.Contains(lower, "freeusage")
}

// quotaCooldownFor returns the appropriate cooldown for a quota error.
// Explicit upstream quota codes use a fixed 30-minute cooldown; otherwise we
// fall back to the provider's reset horizon (next midnight UTC for daily quotas).
func quotaCooldownFor(lower string, headers http.Header) *time.Time {
	if isExplicitQuotaError(lower) {
		until := time.Now().Add(quotaCooldown)
		return &until
	}
	// Prefer a provider-supplied Retry-After or "resets in" text.
	if cd := exactCooldown(lower, headers, 0); cd != nil {
		return cd
	}
	until := nextMidnightUTC().Add(time.Minute)
	return &until
}

// freeUsageCooldown returns the cooldown for free-tier exhaustion. The default is
// 24 hours, but an explicit Retry-After header or "resets in" body hint wins.
func freeUsageCooldown(lower string, headers http.Header) *time.Time {
	if cd := exactCooldown(lower, headers, 0); cd != nil {
		return cd
	}
	until := time.Now().Add(freeUsageCooldownWindow)
	return &until
}

// isExplicitQuotaError matches upstream error text or codes that indicate a
// hard/org-level quota rather than a model-specific rate limit.
func isExplicitQuotaError(lower string) bool {
	return strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "insufficient quota") ||
		strings.Contains(lower, "billing_hard_limit_reached") ||
		strings.Contains(lower, "billing hard limit")
}

// ClassifyFromResponse classifies an error from HTTP status code and response body.
// Status codes that unambiguously map to a category are resolved first; ambiguous
// codes (402, 429) use body-pattern fallbacks so providers cannot force a false
// classification with a generic status code.
func (d *Detector) ClassifyFromResponse(statusCode int, body string) ErrorCategory {
	body = strings.ToLower(body)

	if cat, ok := StatusCodeCategories[statusCode]; ok {
		return cat
	}
	if IsServerErrorStatus(statusCode) {
		return ErrorServer
	}

	switch statusCode {
	case http.StatusTooManyRequests:
		// CF daily limit (4006) and similar providers return 429 with quota body.
		// Check body for quota patterns BEFORE defaulting to ErrorRateLimit.
		if containsAny(body, QuotaPatterns...) {
			return ErrorQuota
		}
		if containsAny(body, BalanceEmptyPatterns...) {
			return ErrorBalanceEmpty
		}
		return ErrorRateLimit
	case http.StatusPaymentRequired:
		// BalanceEmpty checked before Quota — balance-empty is a stricter
		// classification (manual action required).
		if containsAny(body, BalanceEmptyPatterns...) {
			return ErrorBalanceEmpty
		}
		return ErrorQuota
	case http.StatusNotFound:
		if strings.Contains(body, "model") {
			return ErrorModelNotFound
		}
		return ErrorServer
	}

	return d.ClassifyFromMessage(body)
}

// ClassifyFromMessage classifies an error from error message text.
func (d *Detector) ClassifyFromMessage(msg string) ErrorCategory {
	msg = strings.ToLower(msg)

	if containsAny(msg, RateLimitPatterns...) {
		return ErrorRateLimit
	}
	if containsAny(msg, BalanceEmptyPatterns...) {
		return ErrorBalanceEmpty
	}
	if containsAny(msg, QuotaPatterns...) {
		return ErrorQuota
	}
	if containsAny(msg, AuthPatterns...) {
		return ErrorAuth
	}
	if containsAny(msg, ModelNotFoundPatterns...) {
		return ErrorModelNotFound
	}
	if containsAny(msg, ContentFilterPatterns...) {
		return ErrorContentFilter
	}
	if containsAny(msg, TimeoutPatterns...) {
		return ErrorTimeout
	}
	if containsAny(msg, NetworkPatterns...) {
		return ErrorNetwork
	}
	if containsAny(msg, ServerErrorPatterns...) {
		return ErrorServer
	}

	return ErrorUnknown
}

// IsRetryable returns true if the error category is transient.
func (d *Detector) IsRetryable(cat ErrorCategory) bool {
	switch cat {
	case ErrorRateLimit, ErrorServer, ErrorTimeout, ErrorNetwork:
		return true
	default:
		return false
	}
}

// IsProviderFatal returns true if the error indicates the provider is fundamentally broken.
func (d *Detector) IsProviderFatal(cat ErrorCategory) bool {
	switch cat {
	case ErrorAuth, ErrorQuota, ErrorBalanceEmpty:
		return true
	default:
		return false
	}
}

func containsAny(msg string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// rateLimitBackoffState tracks consecutive rate-limit hits so failures can
// escalate exponentially without external state.
type rateLimitBackoffState struct {
	mu       sync.Mutex
	level    int
	lastSeen time.Time
}

var rateLimitBackoff sync.Map // key: provider + "\x00" + modelID

// rateLimitCooldown computes the cooldown for a rate-limit error.
// If the provider supplies Retry-After (header or JSON body), that value is
// respected (capped and jittered). Otherwise the cooldown escalates
// exponentially from a 1s base up to 30m.
func rateLimitCooldown(lower string, headers http.Header) *time.Time {
	retryAfter := 0
	if headers != nil {
		if v, ok := parseRetryAfterSeconds(headers.Get("Retry-After")); ok {
			retryAfter = v
		}
	}
	if retryAfter == 0 && lower != "" {
		retryAfter = parseRetryAfterFromBody(lower)
	}

	var cooldown time.Duration
	if retryAfter > 0 {
		cooldown = time.Duration(retryAfter) * time.Second
		if cooldown > rateLimitMaxCooldown {
			cooldown = rateLimitMaxCooldown
		}
		cooldown = jitteredCooldown(cooldown)
	} else {
		// First attempt from this provider/model starts at 1s and doubles.
		level := incrementRateLimitLevel("global", "")
		cooldown = exponentialCooldown(rateLimitBaseCooldown, level, rateLimitMaxCooldown)
		cooldown = jitteredCooldown(cooldown)
	}

	until := time.Now().Add(cooldown)
	return &until
}

// parseRetryAfterSeconds parses a Retry-After value. It accepts either a delay
// in seconds or an HTTP-date. Values in the past are clamped to zero.
func parseRetryAfterSeconds(v string) (int, bool) {
	if v = strings.TrimSpace(v); v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n, true
	}
	if t, err := http.ParseTime(v); err == nil {
		secs := int(time.Until(t).Seconds())
		if secs < 0 {
			secs = 0
		}
		return secs, true
	}
	return 0, false
}

// parseRetryAfterFromBody looks for retry_after / retryAfter values inside
// provider error JSON (e.g. {"retry_after": 120}) and returns seconds.
func parseRetryAfterFromBody(body string) int {
	for _, path := range []string{"retry_after", "retryAfter", "error.retry_after", "error.retryAfter", "error.body.retry_after"} {
		if val := gjson.Get(body, path).Int(); val > 0 {
			return int(val)
		}
	}
	return 0
}

// exponentialCooldown returns base * 2^level, capped at max.
func exponentialCooldown(base time.Duration, level int, max time.Duration) time.Duration {
	if level <= 0 {
		return base
	}
	d := float64(base) * math.Pow(2, float64(level))
	if d > float64(max) {
		return max
	}
	return time.Duration(d)
}

// incrementRateLimitLevel increments and returns the current escalation level
// for the given key. The counter resets when the previous failure is older than
// the global rate-limit ceiling, preventing unbounded growth across restarts.
func incrementRateLimitLevel(provider, modelID string) int {
	key := provider + "\x00" + modelID
	now := time.Now()
	val, _ := rateLimitBackoff.LoadOrStore(key, &rateLimitBackoffState{})
	s := val.(*rateLimitBackoffState)
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Sub(s.lastSeen) > rateLimitMaxCooldown {
		s.level = 0
	}
	level := s.level
	if s.level < 62 {
		s.level++
	}
	s.lastSeen = now
	return level
}

// resetRateLimitBackoffForTest clears the in-memory rate-limit escalation map.
func resetRateLimitBackoffForTest() {
	rateLimitBackoff = sync.Map{}
}

// rateLimitJitter is overridable in tests so assertions can be deterministic.
var rateLimitJitter = func(d time.Duration) time.Duration {
	return defaultJitter(d)
}

func jitteredCooldown(d time.Duration) time.Duration {
	return rateLimitJitter(d)
}

// defaultJitter adds a small random delay (up to 25% of d, capped at 1s).
func defaultJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	maxJitter := d / 4
	if maxJitter > time.Second {
		maxJitter = time.Second
	}
	if maxJitter <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(int64(maxJitter)))
}

// nextMidnightUTC returns the next 00:00 UTC time (midnight).
// Used for daily-quota providers (e.g. Cloudflare free tier) that reset at UTC midnight.
func nextMidnightUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}

// exactCooldown extracts the most precise cooldown horizon from upstream signals:
// 1. Retry-After header (seconds), 2. "resets in N (hour|h|min|m)" in body,
// 3. fallback def.
// Returns a pointer so callers can distinguish "unset" from "zero" easily.
var resetInRe = regexp.MustCompile(`(?i)resets?\s+in\s+(\d+)\s*(hour|h|min|m)`)

func exactCooldown(msg string, headers http.Header, def time.Duration) *time.Time {
	if headers != nil {
		if retryAfter := headers.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
				until := time.Now().Add(time.Duration(seconds) * time.Second)
				return &until
			}
		}
	}
	if msg != "" {
		if matches := resetInRe.FindStringSubmatch(msg); len(matches) == 3 {
			n, _ := strconv.Atoi(matches[1])
			if n > 0 {
				multiplier := time.Minute
				switch strings.ToLower(matches[2]) {
				case "hour", "h":
					multiplier = time.Hour
				}
				until := time.Now().Add(time.Duration(n) * multiplier)
				return &until
			}
		}
	}
	if def <= 0 {
		return nil
	}
	until := time.Now().Add(def)
	return &until
}

// defaultCooldownFor returns the fallback cooldown horizon for a category.
func defaultCooldownFor(cat ErrorCategory) time.Duration {
	if cat == ErrorQuota {
		return nextMidnightUTC().Sub(time.Now()) + time.Minute
	}
	return 5 * time.Minute
}
