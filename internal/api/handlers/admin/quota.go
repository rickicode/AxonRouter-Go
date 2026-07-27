package admin

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// QuotaHandler handles quota-related API endpoints.
type QuotaHandler struct {
	db    *sql.DB
	store *connstate.Store
}

// NewQuotaHandler creates a new quota handler.
func NewQuotaHandler(database *sql.DB, store *connstate.Store) *QuotaHandler {
	return &QuotaHandler{db: database, store: store}
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

// ResetQuota clears quota/cooldown routing state for a single connection and
// returns the updated auth index (connection ID) plus any model identifiers
// that had active cooldowns before the reset. This mirrors CLIProxyAPI's
// reset-quota management endpoint semantics.
// POST /api/admin/quota/:connId/reset
func (h *QuotaHandler) ResetQuota(c *gin.Context) {
	connID := c.Param("connId")
	if connID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection id required"})
		return
	}

	updated, models, err := h.store.ResetQuota(connID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"auth_index": updated.ID,
		"models":     models,
	})
}
