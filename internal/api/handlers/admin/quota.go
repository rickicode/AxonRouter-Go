package admin

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// QuotaHandler handles quota-related API endpoints.
type QuotaHandler struct {
	db *sql.DB
}

// NewQuotaHandler creates a new quota handler.
func NewQuotaHandler(database *sql.DB) *QuotaHandler {
	return &QuotaHandler{db: database}
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
		Total       int            `json:"total"`
		Statuses    map[string]int `json:"statuses"`
	}

	providerMap := make(map[string]*providerSummary)
	for rows.Next() {
		var providerID, status string
		var count int
		if err := rows.Scan(&providerID, &status, &count); err != nil {
			continue
		}
		if _, ok := providerMap[providerID]; !ok {
			name := providerID
			if meta, ok := quota.ProviderMeta(providerID); ok {
				name = meta.DisplayName
			}
			providerMap[providerID] = &providerSummary{
				ProviderID:  providerID,
				DisplayName: name,
				Statuses:    make(map[string]int),
			}
		}
		providerMap[providerID].Statuses[status] = count
		providerMap[providerID].Total += count
	}

	var summaries []providerSummary
	for _, s := range providerMap {
		summaries = append(summaries, *s)
	}

	c.JSON(http.StatusOK, gin.H{"providers": summaries})
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
