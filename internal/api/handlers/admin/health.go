package admin

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// HealthHandler exposes liveness and operational metrics.
type HealthHandler struct {
	db      *sql.DB
	store   *connstate.Store
	tracker *usage.Tracker
}

// NewHealthHandler creates a new health/metrics handler.
func NewHealthHandler(database *sql.DB, store *connstate.Store, tracker *usage.Tracker) *HealthHandler {
	return &HealthHandler{
		db:      database,
		store:   store,
		tracker: tracker,
	}
}

// Health returns a simple liveness check. It is reachable without admin auth
// so the dashboard and load balancers can use it for online checks.
func (h *HealthHandler) Health(c *gin.Context) {
	dbStatus := "ok"
	if err := h.db.QueryRow(`SELECT 1`).Scan(new(int)); err != nil {
		dbStatus = "error"
	}

	status := http.StatusOK
	if dbStatus != "ok" {
		status = http.StatusServiceUnavailable
	}

	c.JSON(status, gin.H{
		"status": dbStatus,
		"db":     dbStatus,
	})
}

// Metrics returns operational counters for observability.
func (h *HealthHandler) Metrics(c *gin.Context) {
	var rateLimited, quotaExhausted int
	h.db.QueryRow(`
		SELECT COUNT(*) FROM connections WHERE is_active = 1 AND status = 'rate_limited'
	`).Scan(&rateLimited)
	h.db.QueryRow(`
		SELECT COUNT(*) FROM connections WHERE is_active = 1 AND status = 'quota_exhausted'
	`).Scan(&quotaExhausted)

	agg := usage.NewAggregator(h.db)
	requestsToday, tokensToday, costToday, _ := agg.GetTodayStats()

	c.JSON(http.StatusOK, gin.H{
		"buffer_length":        h.tracker.Buffered(),
		"healthy_connections":  h.store.HealthyCount(),
		"dropped_usage_events": h.tracker.Dropped(),
		"rate_limited":         rateLimited,
		"quota_exhausted":      quotaExhausted,
		"requests_today":       requestsToday,
		"tokens_today":         tokensToday,
		"cost_today":           costToday,
	})
}
