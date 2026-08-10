package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	freebuffSessionURL = "https://www.codebuff.com/api/v1/freebuff/session"
	freebuffQuotaUA    = "codebuff-cli/0.0.138"
)

// fetchFreebuffQuota reads the shared daily session quota from the session
// endpoint. It MUST use GET — a POST would CLAIM a session and burn 1.0 unit of
// the daily quota, which a quota tracker must never do.
func fetchFreebuffQuota(accessToken string) ([]QuotaItem, string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, "", fmt.Errorf("freebuff access token not available")
	}
	req, err := http.NewRequest(http.MethodGet, freebuffSessionURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("freebuff quota request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", freebuffQuotaUA)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("freebuff quota request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("freebuff quota response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("freebuff credential invalid or expired — re-login in the dashboard")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, "", freebuffForbiddenError(body)
	}
	if resp.StatusCode == http.StatusNotFound {
		// No session row at all → pre-join state, no quota to report.
		return nil, "Freebuff", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("freebuff quota API error (%d)", resp.StatusCode)
	}
	return parseFreebuffQuota(body)
}

// freebuffForbiddenError turns a 403 from the session endpoint into a friendly
// message. A 403 is usually a server-side gate status (country_blocked / banned),
// not a credential problem — telling the user to re-login would be misleading.
func freebuffForbiddenError(body []byte) error {
	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	switch payload.Status {
	case "country_blocked":
		return fmt.Errorf("freebuff is not available in your region")
	case "banned":
		return fmt.Errorf("your freebuff account has been banned")
	}
	if payload.Message != "" {
		return fmt.Errorf("freebuff quota access denied (403): %s", payload.Message)
	}
	return fmt.Errorf("freebuff quota access denied (403)")
}

// parseFreebuffQuota parses the session quota payload into quota items.
// recentCount is fractional (a long agent run can consume 1.3 units) and
// includes the active session's own 1.0-unit reservation. `limit` can be raised
// by referral/streak rewards.
func parseFreebuffQuota(body []byte) ([]QuotaItem, string, error) {
	var payload freebuffQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", fmt.Errorf("freebuff quota response: %w", err)
	}
	rateLimits := payload.RateLimitsByModel
	if rateLimits == nil {
		rateLimits = map[string]freebuffRateLimit{}
	}
	// An active session carries its own `rateLimit` row — fold it in when the
	// shared map omits the model (older servers).
	if payload.Status == "active" && payload.Model != "" {
		if _, ok := rateLimits[payload.Model]; !ok && payload.RateLimit != nil {
			rateLimits[payload.Model] = *payload.RateLimit
		}
	}

	quotas := make([]QuotaItem, 0, len(rateLimits))
	for model, limit := range rateLimits {
		name := model
		if display, ok := freebuffModelNames[model]; ok {
			name = display
		}
		total := limit.Limit
		used := limit.RecentCount
		remaining := 100.0
		if total > 0 {
			remaining = (total - used) / total * 100
			if remaining < 0 {
				remaining = 0
			}
		}
		quotas = append(quotas, QuotaItem{
			Name:         name,
			ModelKey:     model,
			Used:         used,
			Total:        total,
			RemainingPct: remaining,
			ResetAt:      limit.ResetAt,
			Unlimited:    false,
			Family:       "freebuff",
		})
	}
	return quotas, freebuffPlan(payload.AccessTier), nil
}

type freebuffQuotaResponse struct {
	AccessTier        string                       `json:"accessTier"`
	Status            string                       `json:"status"`
	Model             string                       `json:"model"`
	RateLimit         *freebuffRateLimit           `json:"rateLimit"`
	RateLimitsByModel map[string]freebuffRateLimit `json:"rateLimitsByModel"`
}

type freebuffRateLimit struct {
	Limit       float64 `json:"limit"`
	RecentCount float64 `json:"recentCount"`
	ResetAt     string  `json:"resetAt"`
}

var freebuffModelNames = map[string]string{
	"deepseek/deepseek-v4-flash": "DeepSeek v4 Flash",
	"deepseek/deepseek-v4-pro":   "DeepSeek v4 Pro",
	"mimo/mimo-v2.5":             "MiMo v2.5",
	"minimax/minimax-m3":         "MiniMax M3",
	"openai/gpt-5.6-luna":        "GPT-5.6 Luna",
}

func freebuffPlan(tier string) string {
	if strings.EqualFold(tier, "limited") {
		return "Freebuff (Limited)"
	}
	return "Freebuff"
}
