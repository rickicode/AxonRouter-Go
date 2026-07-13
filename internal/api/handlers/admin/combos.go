package admin

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
		       is_active, created_at, updated_at
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
		rows.Scan(&cb.ID, &cb.Name, &cb.Strategy, &cb.StickyLimit,
			&cb.TimeoutMs, &cb.IsSmart, &cb.SmartGoal,
			&cb.IsActive, &cb.CreatedAt, &cb.UpdatedAt)
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
		Name      string `json:"name" binding:"required"`
		Strategy  string `json:"strategy"`
		TimeoutMs int    `json:"timeout_ms"`
		Steps     []struct {
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
		req.Strategy = "priority"
	}
	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
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

	result, err := h.handler.CreateCombo(req.Name, req.Strategy, req.TimeoutMs, steps)
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
		Name     string `json:"name"`
		Strategy string `json:"strategy"`
		TimeoutMs int   `json:"timeout_ms"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sets := []string{}
	args := []interface{}{}
	if req.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, req.Name)
	}
	if req.Strategy != "" {
		sets = append(sets, "strategy = ?")
		args = append(args, req.Strategy)
	}
	if req.TimeoutMs > 0 {
		sets = append(sets, "timeout_ms = ?")
		args = append(args, req.TimeoutMs)
	}
	if req.IsActive != nil {
		sets = append(sets, "is_active = ?")
		args = append(args, boolToInt(*req.IsActive))
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().Unix(), id)

	result, err := h.db.Exec("UPDATE combos SET "+joinStrings(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found"})
		return
	}

	h.handler.RefreshFromDB()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a combo.
func (h *ComboHandler) Delete(c *gin.Context) {
	id := c.Param("id")
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

	stepID := uuid.New().String()
	_, err := h.db.Exec(`
		INSERT INTO combo_steps (id, combo_id, connection_id, model_id, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, stepID, comboID, req.ConnectionID, req.ModelID, req.Priority, req.Weight, time.Now().Unix())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.handler.RefreshFromDB()
	c.JSON(http.StatusCreated, gin.H{"id": stepID, "priority": req.Priority})
}

// RemoveStep removes a step from a combo.
func (h *ComboHandler) RemoveStep(c *gin.Context) {
	stepID := c.Param("stepId")
	result, err := h.db.Exec(`DELETE FROM combo_steps WHERE id = ?`, stepID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "step not found"})
		return
	}
	h.handler.RefreshFromDB()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SeedDefaults seeds default combos.
func (h *ComboHandler) SeedDefaults(c *gin.Context) {
	if err := combo.SeedDefaultCombos(h.db); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.handler.RefreshFromDB()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
