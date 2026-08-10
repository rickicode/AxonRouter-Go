package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api/middleware"
	"golang.org/x/crypto/bcrypt"
)

// APIKeyHandler manages proxy API keys.
type APIKeyHandler struct {
	db    *sql.DB
	cache *middleware.AuthCache
}

// NewAPIKeyHandler creates a new API key handler.
func NewAPIKeyHandler(db *sql.DB, cache *middleware.AuthCache) *APIKeyHandler {
	return &APIKeyHandler{db: db, cache: cache}
}

// apiKeyView is the masked representation returned by List/Get.
type apiKeyView struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	RateLimitPerMin  int      `json:"rate_limit_per_min"`
	MaxTokens        int64    `json:"max_tokens"`
	IsActive         bool     `json:"is_active"`
	CreatedAt        int64    `json:"created_at"`
	ExpiresAt        int64    `json:"expires_at"`
	AllowedModels    []string `json:"allowed_models"`
	DailyLimitUSD    float64  `json:"daily_limit_usd"`
	MonthlyLimitUSD  float64  `json:"monthly_limit_usd"`
	WarningThreshold float64  `json:"warning_threshold"`
	DailySpendUSD    float64  `json:"daily_spend_usd"`
	MonthlySpendUSD  float64  `json:"monthly_spend_usd"`
}

// budgetPeriodStart returns the UTC midnight that begins the current day or month.
func budgetPeriodStart(now time.Time, periodType string) time.Time {
	y, m, d := now.UTC().Date()
	switch periodType {
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
}

// loadCurrentSpend reads the current daily and monthly USD spend for a given API key.
func (h *APIKeyHandler) loadCurrentSpend(apiKeyID string) (daily, monthly float64) {
	if apiKeyID == "" {
		return 0, 0
	}
	now := time.Now().UTC()
	dayStart := budgetPeriodStart(now, "day").Unix()
	monthStart := budgetPeriodStart(now, "month").Unix()
	row := h.db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN period_type = 'day' AND period_start = ? THEN cost_usd ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN period_type = 'month' AND period_start = ? THEN cost_usd ELSE 0 END), 0)
		FROM api_key_spend_history
		WHERE api_key_id = ?
	`, dayStart, monthStart, apiKeyID)
	var d, m float64
	if err := row.Scan(&d, &m); err != nil {
		return 0, 0
	}
	return d, m
}

// loadCurrentSpendBatch reads current daily and monthly USD spend for multiple API keys in one query.
func (h *APIKeyHandler) loadCurrentSpendBatch(apiKeyIDs []string) map[string][2]float64 {
	result := make(map[string][2]float64, len(apiKeyIDs))
	if len(apiKeyIDs) == 0 {
		return result
	}
	now := time.Now().UTC()
	dayStart := budgetPeriodStart(now, "day").Unix()
	monthStart := budgetPeriodStart(now, "month").Unix()

	placeholders := ""
	args := make([]any, 0, len(apiKeyIDs)+2)
	args = append(args, dayStart, monthStart)
	for i, id := range apiKeyIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	rows, err := h.db.Query(`
		SELECT api_key_id,
			COALESCE(SUM(CASE WHEN period_type = 'day' AND period_start = ? THEN cost_usd ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN period_type = 'month' AND period_start = ? THEN cost_usd ELSE 0 END), 0)
		FROM api_key_spend_history
		WHERE api_key_id IN (`+placeholders+`)
		GROUP BY api_key_id
	`, args...)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var d, m float64
		if err := rows.Scan(&id, &d, &m); err == nil {
			result[id] = [2]float64{d, m}
		}
	}
	return result
}

// List returns all API keys (masked) with current spend data.
func (h *APIKeyHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT k.id, COALESCE(k.name, ''), COALESCE(k.key_value, ''), k.rate_limit_per_min, k.max_tokens, k.is_active, k.created_at,
			COALESCE(k.expires_at, 0), COALESCE(k.allowed_models, ''),
			COALESCE(b.daily_limit_usd, 0), COALESCE(b.monthly_limit_usd, 0), COALESCE(b.warning_threshold, 0.8)
		FROM api_keys k
		LEFT JOIN api_key_budgets b ON b.api_key_id = k.id
		ORDER BY k.created_at DESC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	keys := make([]apiKeyView, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var k apiKeyView
		var isActive int
		var keyValue string
		var maxTokens int64
		var allowedModelsRaw string
		if err := rows.Scan(&k.ID, &k.Name, &keyValue, &k.RateLimitPerMin, &maxTokens, &isActive, &k.CreatedAt, &k.ExpiresAt, &allowedModelsRaw,
			&k.DailyLimitUSD, &k.MonthlyLimitUSD, &k.WarningThreshold); err != nil {
			continue
		}
		k.IsActive = isActive == 1
		k.Key = keyValue
		k.MaxTokens = maxTokens
		if allowedModelsRaw != "" {
			var models []string
			if err := json.Unmarshal([]byte(allowedModelsRaw), &models); err == nil {
				k.AllowedModels = models
			}
		}
		ids = append(ids, k.ID)
		keys = append(keys, k)
	}

	// Batch-load current spend for all keys.
	spendMap := h.loadCurrentSpendBatch(ids)
	for i := range keys {
		if s, ok := spendMap[keys[i].ID]; ok {
			keys[i].DailySpendUSD = s[0]
			keys[i].MonthlySpendUSD = s[1]
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// Get returns a single API key (masked) including its budget configuration and current spend.
func (h *APIKeyHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var k apiKeyView
	var isActive int
	var keyValue string
	var allowedModelsRaw string
	err := h.db.QueryRow(`
		SELECT k.id, COALESCE(k.name, ''), COALESCE(k.key_value, ''), k.rate_limit_per_min, k.max_tokens, k.is_active, k.created_at,
			COALESCE(k.expires_at, 0), COALESCE(k.allowed_models, ''),
			COALESCE(b.daily_limit_usd, 0), COALESCE(b.monthly_limit_usd, 0), COALESCE(b.warning_threshold, 0.8)
		FROM api_keys k
		LEFT JOIN api_key_budgets b ON b.api_key_id = k.id
		WHERE k.id = ?
	`, id).Scan(&k.ID, &k.Name, &keyValue, &k.RateLimitPerMin, &k.MaxTokens, &isActive, &k.CreatedAt, &k.ExpiresAt, &allowedModelsRaw,
		&k.DailyLimitUSD, &k.MonthlyLimitUSD, &k.WarningThreshold)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	k.IsActive = isActive == 1
	k.Key = keyValue
	if allowedModelsRaw != "" {
		var models []string
		if err := json.Unmarshal([]byte(allowedModelsRaw), &models); err == nil {
			k.AllowedModels = models
		}
	}

	k.DailySpendUSD, k.MonthlySpendUSD = h.loadCurrentSpend(id)

	c.JSON(http.StatusOK, gin.H{"data": k})
}

// Create generates a new API key.
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req struct {
		Name             string   `json:"name"`
		RateLimitPerMin  int      `json:"rate_limit_per_min"`
		MaxTokens        int64    `json:"max_tokens"`
		ExpiresAt        *int64   `json:"expires_at"`
		AllowedModels    []string `json:"allowed_models"`
		DailyLimitUSD    float64  `json:"daily_limit_usd"`
		MonthlyLimitUSD  float64  `json:"monthly_limit_usd"`
		WarningThreshold float64  `json:"warning_threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Defaults are fine
	}
	if req.RateLimitPerMin <= 0 {
		req.RateLimitPerMin = 600
	}
	if req.WarningThreshold == 0 {
		req.WarningThreshold = 0.8
	}
	now := time.Now().Unix()
	if req.ExpiresAt != nil && *req.ExpiresAt <= now {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be in the future"})
		return
	}
	// Generate random key
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key"})
		return
	}
	hexPart := hex.EncodeToString(raw)
	id := "ax-" + hexPart[:16]
	keyValue := "ax-" + hexPart
	// Hash with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(keyValue), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash key"})
		return
	}
	name := sql.NullString{}
	if req.Name != "" {
		name = sql.NullString{String: req.Name, Valid: true}
	}
	expiresAt := sql.NullInt64{}
	if req.ExpiresAt != nil {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}
	allowedModels := sql.NullString{}
	if len(req.AllowedModels) > 0 {
		b, err := json.Marshal(req.AllowedModels)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serialize allowed_models"})
			return
		}
		allowedModels = sql.NullString{String: string(b), Valid: true}
	}
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
		INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at, allowed_models)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
	`, id, string(hash), keyValue, name, req.RateLimitPerMin, req.MaxTokens, now, expiresAt, allowedModels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, err = tx.Exec(`
		INSERT INTO api_key_budgets (api_key_id, daily_limit_usd, monthly_limit_usd, warning_threshold, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(api_key_id) DO UPDATE SET
			daily_limit_usd = excluded.daily_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			warning_threshold = excluded.warning_threshold,
			updated_at = excluded.updated_at
	`, id, req.DailyLimitUSD, req.MonthlyLimitUSD, req.WarningThreshold, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	expiresAtResponse := int64(0)
	if req.ExpiresAt != nil {
		expiresAtResponse = *req.ExpiresAt
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":                id,
		"key":               keyValue, // Only shown once
		"name":              req.Name,
		"max_tokens":        req.MaxTokens,
		"expires_at":        expiresAtResponse,
		"allowed_models":    req.AllowedModels,
		"daily_limit_usd":   req.DailyLimitUSD,
		"monthly_limit_usd": req.MonthlyLimitUSD,
		"warning_threshold": req.WarningThreshold,
		"message":           "Save this key - it will not be shown again",
	})
}

// GetValue returns the raw API key value for a given id. The value is only
// available at creation time and stored for convenience; it is served here so
// the dashboard can copy the full key and so CLI tool configs can embed it
// directly from the selected key (no manual paste needed).
func (h *APIKeyHandler) GetValue(c *gin.Context) {
	id := c.Param("id")
	var raw string
	err := h.db.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&raw)
	if err != nil || raw == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "api key value is not available"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "key": raw})
}

// Delete removes an API key and clears its usage, budget, spend history, and cache entry.
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	var keyValue string
	if err := tx.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&keyValue); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM api_key_usage WHERE api_key_id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM api_key_spend_history WHERE api_key_id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM api_key_budgets WHERE api_key_id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM api_keys WHERE id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.cache != nil {
		if keyValue != "" {
			h.cache.Invalidate(keyValue)
		} else {
			h.cache.InvalidateAll()
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ToggleActive enables or disables an API key and applies the supplied max_tokens
// and budget values. A max_tokens of 0 means the key has no token budget limit.
// Because the JSON binding cannot distinguish an omitted field from an explicit 0,
// callers that want to keep the existing limit must read it first and send it back.
func (h *APIKeyHandler) ToggleActive(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		IsActive         bool    `json:"is_active"`
		MaxTokens        int64   `json:"max_tokens"`
		DailyLimitUSD    float64 `json:"daily_limit_usd"`
		MonthlyLimitUSD  float64 `json:"monthly_limit_usd"`
		WarningThreshold float64 `json:"warning_threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.WarningThreshold == 0 {
		req.WarningThreshold = 0.8
	}
	active := 0
	if req.IsActive {
		active = 1
	}
	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE api_keys SET is_active = ?, max_tokens = ? WHERE id = ?`, active, req.MaxTokens, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, err = tx.Exec(`
		INSERT INTO api_key_budgets (api_key_id, daily_limit_usd, monthly_limit_usd, warning_threshold, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(api_key_id) DO UPDATE SET
			daily_limit_usd = excluded.daily_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			warning_threshold = excluded.warning_threshold,
			updated_at = excluded.updated_at
	`, id, req.DailyLimitUSD, req.MonthlyLimitUSD, req.WarningThreshold, time.Now().Unix())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.cache != nil {
		var keyValue string
		if err := h.db.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&keyValue); err == nil {
			if keyValue != "" {
				h.cache.Invalidate(keyValue)
			} else {
				h.cache.InvalidateAll()
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
