package connstate

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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
	Category      ErrorCategory
	Retryable     bool
	Message       string
	Status        Status
	CooldownUntil *time.Time
	Scope         string // "connection" or "model"
	ModelID       string // model ID if scope is "model"
}

// Detector detects error types from HTTP responses and error messages.
type Detector struct{}

// NewDetector creates a new error detector.
func NewDetector() *Detector {
	return &Detector{}
}

// DetectError classifies an error from HTTP status code, response body, and headers.
// modelID is passed through so callers can identify which model was rate-limited.
// headers is used to extract rate limit info and retry-after values.
func DetectError(statusCode int, body string, err error, providerPrefix string, modelID string, headers http.Header) ErrorDetection {
	d := NewDetector()
	var msg string
	if err != nil {
		msg = err.Error()
	} else {
		msg = body
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
	cat := d.ClassifyFromMessage(msg)
	if statusCode > 0 {
		cat = d.ClassifyFromResponse(statusCode, msg)
	}

	det := ErrorDetection{
		Category:  cat,
		Retryable: d.IsRetryable(cat),
		Message:   msg,
		Scope:     "connection",
		ModelID:   modelID,
	}
	switch cat {
	case ErrorRateLimit:
		det.Status = StatusRateLimited
		det.Scope = "model"
		cooldown := 60 * time.Second
		if headers != nil {
			if retryAfter := headers.Get("Retry-After"); retryAfter != "" {
				if seconds, err2 := strconv.Atoi(retryAfter); err2 == nil && seconds > 0 {
					cooldown = time.Duration(seconds) * time.Second
				}
			}
		}
		if cooldown == 60*time.Second && msg != "" {
			if retryBody := parseRetryAfterFromBody(msg); retryBody > 0 {
				cooldown = time.Duration(retryBody) * time.Second
			}
		}
		until := time.Now().Add(cooldown)
		det.CooldownUntil = &until
	case ErrorAuth:
		det.Status = StatusAuthFailed
	case ErrorBalanceEmpty:
		det.Status = StatusBalanceEmpty
	case ErrorQuota:
		det.Status = StatusQuotaExhausted
		until := nextMidnightUTC().Add(time.Minute)
		det.CooldownUntil = &until
	case ErrorServer, ErrorTimeout, ErrorNetwork:
		det.Status = StatusDegraded
	}
	return det
}

// ClassifyFromResponse classifies an error from HTTP status code and response body.
func (d *Detector) ClassifyFromResponse(statusCode int, body string) ErrorCategory {
	body = strings.ToLower(body)

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return ErrorAuth
	case statusCode == http.StatusTooManyRequests:
		// CF daily limit (4006) and similar providers return 429 with quota body.
		// Check body for quota patterns BEFORE defaulting to ErrorRateLimit.
		if containsAny(body, QuotaPatterns...) {
			return ErrorQuota
		}
		if containsAny(body, BalanceEmptyPatterns...) {
			return ErrorBalanceEmpty
		}
		return ErrorRateLimit
	case statusCode == http.StatusPaymentRequired:
		// BalanceEmpty checked before Quota — balance-empty is a stricter classification (manual action required)
		if containsAny(body, BalanceEmptyPatterns...) {
			return ErrorBalanceEmpty
		}
		return ErrorQuota
	case statusCode == http.StatusNotFound:
		if strings.Contains(body, "model") {
			return ErrorModelNotFound
		}
		return ErrorServer
	case statusCode >= 500:
		return ErrorServer
	case statusCode == http.StatusRequestTimeout:
		return ErrorTimeout
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

// parseRetryAfterFromBody looks for retry_after / retryAfter values inside
// provider error JSON (e.g. {"retry_after": 120}) and returns seconds.
func parseRetryAfterFromBody(body string) int {
	for _, path := range []string{"retry_after", "retryAfter", "error.retry_after", "error.retryAfter"} {
		if val := gjson.Get(body, path).Int(); val > 0 {
			return int(val)
		}
	}
	return 0
}

// nextMidnightUTC returns the next 00:00 UTC time (midnight).
// Used for daily-quota providers (e.g. Cloudflare free tier) that reset at UTC midnight.
func nextMidnightUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}
