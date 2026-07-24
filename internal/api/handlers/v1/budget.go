package v1

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// budgetPeriodStart returns the UTC midnight that begins the current day or month.
// Budget windows reset implicitly because queries target the current period_start.
func budgetPeriodStart(now time.Time, periodType string) time.Time {
	y, m, d := now.UTC().Date()
	switch periodType {
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
}

// spendRecordID returns a collision-resistant id for a spend-history row.
func spendRecordID(apiKeyID string, now time.Time) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a timestamp-based prefix if the OS random source fails.
		return fmt.Sprintf("%s-%d", apiKeyID, now.UnixNano())
	}
	return fmt.Sprintf("%s-%d-%s", apiKeyID, now.UnixNano(), hex.EncodeToString(b))
}

// apiKeyBudget holds per-key USD budget configuration.
type apiKeyBudget struct {
	dailyLimit       float64
	monthlyLimit     float64
	warningThreshold float64
}

// loadAPIKeyBudget reads the budget configuration for an API key.
// A missing row is treated as unlimited with the default warning threshold.
func (h *Handler) loadAPIKeyBudget(apiKeyID string) (apiKeyBudget, error) {
	var b apiKeyBudget
	if apiKeyID == "" {
		return b, nil
	}
	err := h.db.QueryRow(`
		SELECT COALESCE(daily_limit_usd, 0),
		       COALESCE(monthly_limit_usd, 0),
		       COALESCE(warning_threshold, 0.8)
		FROM api_key_budgets
		WHERE api_key_id = ?
	`, apiKeyID).Scan(&b.dailyLimit, &b.monthlyLimit, &b.warningThreshold)
	if err == sql.ErrNoRows {
		return apiKeyBudget{warningThreshold: 0.8}, nil
	}
	if err != nil {
		return apiKeyBudget{warningThreshold: 0.8}, err
	}
	return b, nil
}

// currentSpend returns the USD spend for the current UTC day and month.
func (h *Handler) currentSpend(apiKeyID string) (daily, monthly float64, err error) {
	if apiKeyID == "" {
		return 0, 0, nil
	}
	now := time.Now().UTC()
	dayStart := budgetPeriodStart(now, "day").Unix()
	monthStart := budgetPeriodStart(now, "month").Unix()

	row := h.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN period_type = 'day' AND period_start = ? THEN cost_usd ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN period_type = 'month' AND period_start = ? THEN cost_usd ELSE 0 END), 0)
		FROM api_key_spend_history
		WHERE api_key_id = ?
	`, dayStart, monthStart, apiKeyID)
	var d, m float64
	if err := row.Scan(&d, &m); err != nil {
		return 0, 0, err
	}
	return d, m, nil
}

// checkAPIKeyBudget rejects the request with HTTP 429 when the API key's daily
// or monthly USD budget is exhausted. It logs a warning when spend crosses the
// configured warning threshold.
func (h *Handler) checkAPIKeyBudget(c *gin.Context) error {
	apiKeyID := c.GetString("api_key_id")
	if apiKeyID == "" {
		return nil
	}

	budget, err := h.loadAPIKeyBudget(apiKeyID)
	if err != nil {
		logging.Logger.Warn("checkAPIKeyBudget: failed to read budget", "error", err.Error())
		return nil
	}
	if budget.dailyLimit <= 0 && budget.monthlyLimit <= 0 {
		return nil
	}

	dailySpent, monthlySpent, err := h.currentSpend(apiKeyID)
	if err != nil {
		logging.Logger.Warn("checkAPIKeyBudget: failed to read spend", "error", err.Error())
		return nil
	}

	if budget.dailyLimit > 0 && dailySpent >= budget.dailyLimit {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "API key daily USD budget exhausted", "code": "api_key_daily_budget_exhausted"}})
		return errors.New("api key daily USD budget exhausted")
	}
	if budget.monthlyLimit > 0 && monthlySpent >= budget.monthlyLimit {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "API key monthly USD budget exhausted", "code": "api_key_monthly_budget_exhausted"}})
		return errors.New("api key monthly USD budget exhausted")
	}

	threshold := budget.warningThreshold
	if threshold <= 0 {
		threshold = 0.8
	}
	if budget.dailyLimit > 0 && dailySpent >= budget.dailyLimit*threshold {
		logging.Logger.Warn("API key daily USD budget warning threshold reached",
			"api_key_id", apiKeyID,
			"spent", dailySpent,
			"limit", budget.dailyLimit,
			"threshold", threshold)
	}
	if budget.monthlyLimit > 0 && monthlySpent >= budget.monthlyLimit*threshold {
		logging.Logger.Warn("API key monthly USD budget warning threshold reached",
			"api_key_id", apiKeyID,
			"spent", monthlySpent,
			"limit", budget.monthlyLimit,
			"threshold", threshold)
	}

	return nil
}

// recordAPIKeyCost persists a USD cost record for an API key.
// It inserts one row for the current UTC day bucket and one for the current
// UTC month bucket so budgets can be enforced with simple indexed aggregations.
// Reset on daily/monthly boundaries is implicit: queries target the current
// period_start only.
func (h *Handler) recordAPIKeyCost(apiKeyID, modelID string, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens int64, responseCostUsd float64) {
	if apiKeyID == "" {
		return
	}

	cost := responseCostUsd
	if cost <= 0 {
		cost = usage.EstimateCost(modelID, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens)
	}
	if cost <= 0 {
		return
	}

	now := time.Now().UTC()
	createdAt := now.Unix()
	dayStart := budgetPeriodStart(now, "day").Unix()
	monthStart := budgetPeriodStart(now, "month").Unix()
	idPrefix := spendRecordID(apiKeyID, now)

	if _, err := h.db.Exec(`
		INSERT INTO api_key_spend_history (id, api_key_id, cost_usd, period_type, period_start, created_at)
		VALUES (?, ?, ?, 'day', ?, ?), (?, ?, ?, 'month', ?, ?)
	`, idPrefix+"-d", apiKeyID, cost, dayStart, createdAt,
		idPrefix+"-m", apiKeyID, cost, monthStart, createdAt); err != nil {
		logging.Logger.Warn("recordAPIKeyCost: failed to insert spend history",
			"api_key_id", apiKeyID, "error", err.Error())
	}
}

// recordAPIKeyCostFromRequest estimates and records cost from a request/response
// pair when explicit token counts are not available.
func (h *Handler) recordAPIKeyCostFromRequest(apiKeyID string, reqBody, respBody []byte, estimateOutput bool) {
	if apiKeyID == "" || len(reqBody) == 0 {
		return
	}
	model := executor.JSONGet(reqBody, "model")
	inputTokens := usage.EstimateTokensFromRequest(reqBody)
	var outputTokens int64
	if estimateOutput {
		outputTokens = usage.EstimateTokensFromResponse(respBody)
	}
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	h.recordAPIKeyCost(apiKeyID, model, inputTokens, outputTokens, 0, 0, 0, 0)
}

// recordAPIKeyCostFromCounts records cost from explicit token counts.
func (h *Handler) recordAPIKeyCostFromCounts(apiKeyID, modelID string, counts StreamTokenCounts) {
	h.recordAPIKeyCost(apiKeyID, modelID,
		counts.InputTokens,
		counts.OutputTokens,
		counts.ReasoningTokens,
		counts.CachedTokens,
		counts.CacheCreationTokens,
		counts.CostUsd)
}
