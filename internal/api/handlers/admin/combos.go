package admin

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// ComboHandler handles combo CRUD operations.
type ComboHandler struct {
	db      *sql.DB
	handler *combo.Handler
}

// NewComboHandler creates a new combo handler.
func NewComboHandler(database *sql.DB, handler *combo.Handler) *ComboHandler {
	return &ComboHandler{db: database, handler: handler}
}

// List returns all combos.
func (h *ComboHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	var total int
	h.db.QueryRow(`SELECT COUNT(*) FROM combos WHERE is_active = 1`).Scan(&total)

	offset := (page - 1) * perPage
	rows, err := h.db.Query(`
	SELECT id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal,
	fusion_config, is_active, created_at, updated_at
	FROM combos WHERE is_active = 1
	ORDER BY created_at DESC
	LIMIT ? OFFSET ?
	`, perPage, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var combos []db.Combo
	for rows.Next() {
		cb := db.Combo{}
		var fusionConfig sql.NullString
		if err := rows.Scan(&cb.ID, &cb.Name, &cb.Strategy, &cb.StickyLimit,
			&cb.TimeoutMs, &cb.IsSmart, &cb.SmartGoal, &fusionConfig,
			&cb.IsActive, &cb.CreatedAt, &cb.UpdatedAt); err != nil {
			log.Printf("WARN: failed to scan combo row in admin list: %v", err)
			continue
		}
		cb.FusionConfig = fusionConfig.String
		combos = append(combos, cb)
	}

	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, db.PaginatedResponse{
		Data: combos,
		Pagination: db.Pagination{
			Page:       page,
			PerPage:    perPage,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// Get returns a single combo with its steps.
func (h *ComboHandler) Get(c *gin.Context) {
	id := c.Param("id")
	result, ok := h.handler.GetCombo(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// Create creates a new combo with steps.
func (h *ComboHandler) Create(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		Strategy     string `json:"strategy"`
		TimeoutMs    int    `json:"timeout_ms"`
		StickyLimit  int    `json:"sticky_limit"`
		IsSmart      bool   `json:"is_smart"`
		SmartGoal    string `json:"smart_goal"`
		FusionConfig string `json:"fusion_config"`
		Steps        []struct {
			ConnectionID string `json:"connection_id"`
			ModelID      string `json:"model_id" binding:"required"`
			Priority     int    `json:"priority"`
			Weight       int    `json:"weight"`
		} `json:"steps"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Strategy == "" {
		req.Strategy = h.handler.DefaultStrategy()
	}
	if !combo.IsValidStrategy(req.Strategy) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy: " + req.Strategy})
		return
	}

	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
	}
	if req.StickyLimit == 0 {
		req.StickyLimit = 1
	}

	var steps []combo.CreateStepInput
	for i, s := range req.Steps {
		priority := s.Priority
		if priority == 0 {
			priority = i + 1
		}
		weight := s.Weight
		if weight == 0 {
			weight = 100
		}
		steps = append(steps, combo.CreateStepInput{
			ConnectionID: s.ConnectionID,
			ModelID:      s.ModelID,
			Priority:     priority,
			Weight:       weight,
		})
	}

	// Validate fusion configuration early so invalid combos fail at creation time.
	if req.Strategy == "fusion" || req.FusionConfig != "" {
		cfg, err := combo.ParseFusionConfig(req.FusionConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := cfg.Validate(len(steps)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	result, err := h.handler.CreateCombo(req.Name, req.Strategy, req.TimeoutMs, req.StickyLimit, req.IsSmart, req.SmartGoal, req.FusionConfig, steps)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Update modifies a combo.
func (h *ComboHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name         string  `json:"name"`
		Strategy     string  `json:"strategy"`
		TimeoutMs    int     `json:"timeout_ms"`
		StickyLimit  *int    `json:"sticky_limit"`
		IsSmart      *bool   `json:"is_smart"`
		SmartGoal    *string `json:"smart_goal"`
		FusionConfig string  `json:"fusion_config"`
		IsActive     *bool   `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Strategy != "" && !isValidComboStrategy(req.Strategy) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy: " + req.Strategy})
		return
	}

	if req.FusionConfig != "" {
		cfg, err := combo.ParseFusionConfig(req.FusionConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var stepCount int
		if err := h.db.QueryRow(`SELECT COUNT(*) FROM combo_steps WHERE combo_id = ?`, id).Scan(&stepCount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := cfg.Validate(stepCount); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	if err := h.handler.UpdateCombo(id, combo.UpdateComboInput{
		Name:         req.Name,
		Strategy:     req.Strategy,
		TimeoutMs:    req.TimeoutMs,
		StickyLimit:  req.StickyLimit,
		IsSmart:      req.IsSmart,
		SmartGoal:    req.SmartGoal,
		FusionConfig: req.FusionConfig,
		IsActive:     req.IsActive,
	}); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "combo not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Strategy != "" {
		h.handler.ResetRotationCounter(id)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a combo.
func (h *ComboHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	var exists int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM combos WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found"})
		return
	}
	if err := h.handler.DeleteCombo(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// AddStep adds a step to a combo.
func (h *ComboHandler) AddStep(c *gin.Context) {
	comboID := c.Param("id")
	var req struct {
		ConnectionID string `json:"connection_id"`
		ModelID      string `json:"model_id" binding:"required"`
		Priority     int    `json:"priority"`
		Weight       int    `json:"weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Weight == 0 {
		req.Weight = 100
	}
	if req.Priority == 0 {
		// Auto-assign next priority
		var maxPriority int
		h.db.QueryRow(`SELECT COALESCE(MAX(priority), 0) FROM combo_steps WHERE combo_id = ?`, comboID).Scan(&maxPriority)
		req.Priority = maxPriority + 1
	}

	stepID, err := h.handler.AddComboStep(comboID, combo.AddComboStepInput{
		ConnectionID: req.ConnectionID,
		ModelID:      req.ModelID,
		Priority:     req.Priority,
		Weight:       req.Weight,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": stepID, "priority": req.Priority})
}

// RemoveStep removes a step from a combo.
func (h *ComboHandler) RemoveStep(c *gin.Context) {
	stepID := c.Param("stepId")
	if err := h.handler.RemoveComboStep(stepID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "step not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ComboMetric aggregates request_logs usage for a single combo.
type ComboMetric struct {
	ComboID      string  `json:"combo_id"`
	ComboName    string  `json:"combo_name"`
	Requests     int     `json:"requests"`
	Successes    int     `json:"successes"`
	Errors       int     `json:"errors"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// Metrics returns aggregate usage counts per combo from request_logs.
// Query param `window` is the lookback window in seconds (default 86400, max 2592000).
func (h *ComboHandler) Metrics(c *gin.Context) {
	window := 24 * time.Hour
	if w, err := strconv.Atoi(c.DefaultQuery("window", "86400")); err == nil && w > 0 {
		maxWindow := 30 * 24 * time.Hour
		if time.Duration(w)*time.Second <= maxWindow {
			window = time.Duration(w) * time.Second
		} else {
			window = maxWindow
		}
	}
	cutoff := time.Now().Add(-window).UnixMilli()

	rows, err := h.db.Query(`
		SELECT
			c.id,
			c.name,
			COUNT(r.id) AS requests,
			COALESCE(SUM(CASE WHEN r.status_code >= 200 AND r.status_code < 400 THEN 1 ELSE 0 END), 0) AS successes,
			COALESCE(SUM(CASE WHEN r.status_code >= 400 OR r.status_code IS NULL THEN 1 ELSE 0 END), 0) AS errors,
			COALESCE(SUM(r.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(r.output_tokens), 0) AS output_tokens,
			COALESCE(AVG(r.latency_ms), 0) AS avg_latency_ms
		FROM combos c
		LEFT JOIN request_logs r ON r.combo_id = c.id AND r.timestamp > ?
		GROUP BY c.id, c.name
		ORDER BY requests DESC
	`, cutoff)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	metrics := []ComboMetric{}
	totals := ComboMetric{ComboName: "total"}
	for rows.Next() {
		var m ComboMetric
		var avg sql.NullFloat64
		if err := rows.Scan(&m.ComboID, &m.ComboName, &m.Requests, &m.Successes, &m.Errors, &m.InputTokens, &m.OutputTokens, &avg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if avg.Valid {
			m.AvgLatencyMs = avg.Float64
		}
		metrics = append(metrics, m)
		totals.Requests += m.Requests
		totals.Successes += m.Successes
		totals.Errors += m.Errors
		totals.InputTokens += m.InputTokens
		totals.OutputTokens += m.OutputTokens
	}

	if totals.Requests > 0 {
		weightedLatencySum := 0.0
		for _, m := range metrics {
			weightedLatencySum += m.AvgLatencyMs * float64(m.Requests)
		}
		totals.AvgLatencyMs = weightedLatencySum / float64(totals.Requests)
	}

	c.JSON(http.StatusOK, gin.H{"data": metrics, "totals": totals})
}

// isValidComboStrategy returns true for supported strategy values.
func isValidComboStrategy(strategy string) bool {
	return combo.IsValidStrategy(strategy)
}

// SeedDefaults seeds default combos.
func (h *ComboHandler) SeedDefaults(c *gin.Context) {
	if err := combo.SeedDefaultCombos(h.db); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.handler.RefreshFromDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
