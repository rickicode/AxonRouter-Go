package admin

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// DashboardHandler handles dashboard statistics.
type DashboardHandler struct {
	db      *sql.DB
	startAt time.Time
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(database *sql.DB) *DashboardHandler {
	return &DashboardHandler{
		db:      database,
		startAt: time.Now(),
	}
}

// Stats returns aggregated dashboard statistics.
func (h *DashboardHandler) Stats(c *gin.Context) {
	var totalProviders, totalConns, totalCombos int
	h.db.QueryRow(`SELECT COUNT(*) FROM provider_types`).Scan(&totalProviders)
	h.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE is_active = 1`).Scan(&totalConns)
	h.db.QueryRow(`SELECT COUNT(*) FROM combos WHERE is_active = 1`).Scan(&totalCombos)

	// Status breakdown
	statusCounts := make(map[string]int)
	rows, err := h.db.Query(`
		SELECT status, COUNT(*) FROM connections WHERE is_active = 1 GROUP BY status
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			rows.Scan(&status, &count)
			statusCounts[status] = count
		}
	}

	// Today's stats
	agg := usage.NewAggregator(h.db)
	requestsToday, tokensToday, costToday, _ := agg.GetTodayStats()

	// Buffer stats (ponytail: stub, real tracker ref needed)
	bufferLen := 0

	c.JSON(http.StatusOK, gin.H{
		"total_providers":   totalProviders,
		"total_connections": totalConns,
		"total_combos":      totalCombos,
		"status_counts":     statusCounts,
		"requests_today":    requestsToday,
		"tokens_today":      tokensToday,
		"cost_today":        costToday,
		"uptime_seconds":    int(time.Since(h.startAt).Seconds()),
		"buffer_length":     bufferLen,
	})
}

// ProviderSummary returns per-provider connection summaries.
func (h *DashboardHandler) ProviderSummary(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT pt.id, pt.display_name, pt.format,
		       COUNT(c.id) as total,
		       SUM(CASE WHEN c.status = 'ready' THEN 1 ELSE 0 END) as ready,
		       SUM(CASE WHEN c.status = 'rate_limited' THEN 1 ELSE 0 END) as rate_limited,
		       SUM(CASE WHEN c.status = 'quota_exhausted' THEN 1 ELSE 0 END) as quota_exhausted,
		       SUM(CASE WHEN c.status = 'balance_empty' THEN 1 ELSE 0 END) as balance_empty,
		       SUM(CASE WHEN c.status = 'auth_failed' THEN 1 ELSE 0 END) as auth_failed,
		       SUM(CASE WHEN c.status = 'suspended' THEN 1 ELSE 0 END) as suspended,
		       SUM(CASE WHEN c.status = 'disabled' THEN 1 ELSE 0 END) as disabled
		FROM provider_types pt
		LEFT JOIN connections c ON c.provider_type_id = pt.id AND c.is_active = 1
		GROUP BY pt.id
		ORDER BY COUNT(c.id) DESC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var summary []gin.H
	for rows.Next() {
		var id, name, format string
		var total, ready, rl, qe, be, af, sus, dis int
		rows.Scan(&id, &name, &format, &total, &ready, &rl, &qe, &be, &af, &sus, &dis)
		summary = append(summary, gin.H{
			"id":               id,
			"display_name":     name,
			"format":           format,
			"total":            total,
			"ready":            ready,
			"rate_limited":     rl,
			"quota_exhausted":  qe,
			"balance_empty":    be,
			"auth_failed":      af,
			"suspended":        sus,
			"disabled":         dis,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": summary})
}

// RecentLogs returns the last N log entries for the dashboard.
func (h *DashboardHandler) RecentLogs(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT id, timestamp, provider_type_id, model_id, modality,
		       latency_ms, status_code, error_message, cost_usd
		FROM request_logs
		ORDER BY timestamp DESC
		LIMIT 20
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var logs []db.RequestLog
	for rows.Next() {
		l := db.RequestLog{}
		rows.Scan(&l.ID, &l.Timestamp, &l.ProviderTypeID, &l.ModelID,
			&l.Modality, &l.LatencyMs, &l.StatusCode, &l.ErrorMessage, &l.CostUsd)
		logs = append(logs, l)
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}
