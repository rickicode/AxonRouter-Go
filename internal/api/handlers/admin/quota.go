package admin

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// QuotaHandler handles quota-related API endpoints.
type QuotaHandler struct {
	db         *sql.DB
	store      *connstate.Store
	elig       *connstate.EligibilityManager
	exhaustion *quota.ExhaustionCache
}

// NewQuotaHandler creates a new quota handler backed only by the database.
// It is kept for backward compatibility; production code uses NewQuotaHandlerWithStore.
func NewQuotaHandler(database *sql.DB) *QuotaHandler {
	return &QuotaHandler{db: database}
}

// NewQuotaHandlerWithStore creates a new quota handler with access to the in-memory
// routing state so reset operations can clear cooldowns and exhaustion immediately.
func NewQuotaHandlerWithStore(
	database *sql.DB,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	exhaustion *quota.ExhaustionCache,
) *QuotaHandler {
	return &QuotaHandler{
		db:         database,
		store:      store,
		elig:       elig,
		exhaustion: exhaustion,
	}
}

// List returns cached quota data with filters, search, and pagination.
// GET /api/admin/quota?provider=&search=&status=&page=1&per_page=50
func (h *QuotaHandler) List(c *gin.Context) {
	providerID := c.Query("provider")
	search := c.Query("search")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	data, err := quota.LoadQuotaCache(h.db, providerID, search, status, page, perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

// Summary returns aggregated quota stats for the dashboard header.
// GET /api/admin/quota/summary
func (h *QuotaHandler) Summary(c *gin.Context) {
	rows, err := h.db.Query(`
SELECT provider_type_id, status, COUNT(*) as cnt
FROM quota_cache
GROUP BY provider_type_id, status
`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type providerSummary struct {
		ProviderID  string         `json:"provider_id"`
		DisplayName string         `json:"display_name"`
		Color       string         `json:"color"`
		IconFile    string         `json:"icon_file"`
		Total       int            `json:"total"`
		Statuses    map[string]int `json:"statuses"`
		NextReset   string         `json:"next_reset,omitempty"`
		SpentUSD    float64        `json:"spent_usd"`
	}
	resets, _ := quota.NextProviderResets(h.db)
	costByProvider, totalCost, _ := usage.CostThisMonth(h.db)
	providerMap := make(map[string]*providerSummary)
	for rows.Next() {
		var providerID, status string
		var count int
		if err := rows.Scan(&providerID, &status, &count); err != nil {
			continue
		}
		if _, ok := providerMap[providerID]; !ok {
			name := providerID
			color := "#888888"
			iconFile := ""
			if meta, ok := quota.ProviderMeta(providerID); ok {
				name = meta.DisplayName
				color = meta.Color
				iconFile = meta.IconFile
			}
			providerMap[providerID] = &providerSummary{
				ProviderID:  providerID,
				DisplayName: name,
				Color:       color,
				IconFile:    iconFile,
				Statuses:    make(map[string]int),
			}
		}
		providerMap[providerID].Statuses[status] = count
		providerMap[providerID].Total += count
	}
	var summaries []providerSummary
	for _, s := range providerMap {
		s.NextReset = resets[s.ProviderID]
		s.SpentUSD = costByProvider[s.ProviderID]
		summaries = append(summaries, *s)
	}
	c.JSON(http.StatusOK, gin.H{
		"providers":  summaries,
		"spent_usd":  totalCost,
		"next_reset": earliestReset(resets),
	})
}

func earliestReset(resets map[string]string) string {
	var earliest time.Time
	var earliestStr string
	for _, s := range resets {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			continue
		}
		if earliestStr == "" || t.Before(earliest) {
			earliest = t
			earliestStr = s
		}
	}
	return earliestStr
}

// Refresh fetches fresh quota for a single connection and saves to cache.
// POST /api/admin/quota/:connId/refresh
func (h *QuotaHandler) Refresh(c *gin.Context) {
	connID := c.Param("connId")
	if connID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection id required"})
		return
	}
	data, err := quota.FetchConnectionQuota(h.db, connID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	// Save to cache
	quota.SaveQuotaCache(h.db, []quota.ProviderQuota{{
		ProviderID:   data.ProviderID,
		ProviderName: data.ProviderName,
		Connections:  []quota.ConnectionQuota{*data},
	}})
	c.JSON(http.StatusOK, data)
}

// Reset clears local quota/cooldown routing state for a single connection/auth and
// returns the updated identifier plus the list of affected models.
// POST /api/admin/quota/:connId/reset
func (h *QuotaHandler) Reset(c *gin.Context) {
	connID := c.Param("connId")
	if connID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection id required"})
		return
	}

	var providerID, status, disabledReason string
	var isActive int
	err := h.db.QueryRow(`
SELECT provider_type_id, status, COALESCE(disabled_reason,''), is_active
FROM connections WHERE id = ?
`, connID).Scan(&providerID, &status, &disabledReason, &isActive)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Only heal non-terminal statuses. Terminal failures such as auth_failed or
	// balance_empty should remain disabled so an operator must re-enable them
	// explicitly after fixing the root cause.
	if !connstate.Status(status).IsHealable() && status != string(connstate.StatusReady) {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("connection is in terminal state %q and cannot be reset", status),
		})
		return
	}

	// Persist the reset: clear cooldown, errors, and failure counters. Re-enable
	// the connection only if it was disabled by a healable cooldown/quota reason.
	newIsActive := 1
	newDisabledReason := disabledReason
	if connstate.Status(status).IsRoutingTerminal() {
		if disabledReason == "auth_failed" || disabledReason == "balance_empty" {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("connection disabled_reason %q prevents quota reset", disabledReason),
			})
			return
		}
		newDisabledReason = ""
	}

	now := time.Now().Unix()
	_, err = h.db.Exec(`
UPDATE connections
SET status = 'ready',
    cooldown_until = NULL,
    last_error = NULL,
    last_error_code = NULL,
    failure_count = 0,
    consecutive_ban_count = 0,
    disabled_reason = ?,
    is_active = ?,
    updated_at = ?
WHERE id = ?
`, newDisabledReason, newIsActive, now, connID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sync in-memory state.
	models := h.clearInMemoryQuotaState(connID)
	// Recompute eligibility so the connection is immediately routable again.
	if h.elig != nil {
		h.elig.Update(h.store)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"connection_id": connID,
		"provider_type": providerID,
		"models":        models,
	})
}

// clearInMemoryQuotaState clears cooldowns and exhaustion marks for a connection
// and returns the list of affected model identifiers.
func (h *QuotaHandler) clearInMemoryQuotaState(connID string) []string {
	seen := make(map[string]struct{})

	// Collect scoped exhaustion entries and model cooldowns first.
	var scopes []string
	if h.exhaustion != nil {
		scopes = h.exhaustion.ScopesForConn(connID)
	}
	if h.store != nil {
		if cs := h.store.Get(connID); cs != nil {
			for modelID := range cs.ModelCooldowns() {
				seen[modelID] = struct{}{}
			}
		}
	}

	// Clear connection-wide and scoped exhaustion.
	if h.exhaustion != nil {
		h.exhaustion.Clear(connID)
		for _, scope := range scopes {
			h.exhaustion.Clear(quota.ExhaustKey(connID, scope))
			// Scopes are from ExhaustKey(connID, scope); the model/family portion may
			// itself be a model id. Track it for the response.
			if _, prefixPart, ok := strings.Cut(scope, "\x00"); ok {
				seen[prefixPart] = struct{}{}
			}
		}
	}

	// Reset connection status and clear per-model cooldowns in the live store.
	if h.store != nil {
		h.store.UpdateStatus(connID, connstate.StatusReady)
		if cs := h.store.Get(connID); cs != nil {
			cs.ResetBanCount()
			for modelID := range cs.ModelCooldowns() {
				seen[modelID] = struct{}{}
				cs.ClearModelCooldown(modelID)
			}
		}
	}

	models := make([]string, 0, len(seen))
	for m := range seen {
		models = append(models, m)
	}
	return models
}
