package admin

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// modelTester is a file-level interface for testing provider connectivity via Models() endpoint.
type modelTester interface {
	Models(ctx context.Context, req *executor.Request) (*executor.Response, error)
}

// ConnectionHandler handles connection CRUD operations.
type ConnectionHandler struct {
	db       *sql.DB
	registry *executor.Registry
	store    *connstate.Store
	elig     *connstate.EligibilityManager
}

// NewConnectionHandler creates a new connection handler.
func NewConnectionHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store, elig *connstate.EligibilityManager) *ConnectionHandler {
	return &ConnectionHandler{db: database, registry: registry, store: store, elig: elig}
}

// List returns paginated connections for a provider.
func (h *ConnectionHandler) List(c *gin.Context) {
	providerID := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	status := c.Query("status")
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	// Build where clause
	where := "provider_type_id = ? AND is_active = 1"
	args := []interface{}{providerID}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if search != "" {
		where += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}

	// Count total
	var total int
	h.db.QueryRow("SELECT COUNT(*) FROM connections WHERE "+where, args...).Scan(&total)

	// Fetch page
	offset := (page - 1) * perPage
	queryArgs := append(args, perPage, offset)
	rows, err := h.db.Query(`
		SELECT id, provider_type_id, name, auth_type, status,
		       cooldown_until, last_error, last_error_code,
		       last_success_at, last_failure_at, failure_count,
		       capabilities, is_active, created_at, updated_at
		FROM connections WHERE `+where+`
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var conns []db.Connection
	for rows.Next() {
		conn := db.Connection{}
		rows.Scan(&conn.ID, &conn.ProviderTypeID, &conn.Name, &conn.AuthType,
			&conn.Status, &conn.CooldownUntil, &conn.LastError, &conn.LastErrorCode,
			&conn.LastSuccessAt, &conn.LastFailureAt, &conn.FailureCount,
			&conn.Capabilities, &conn.IsActive, &conn.CreatedAt, &conn.UpdatedAt)
		conns = append(conns, conn)
	}

	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, db.PaginatedResponse{
		Data: conns,
		Pagination: db.Pagination{
			Page:       page,
			PerPage:    perPage,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// Get returns a single connection.
func (h *ConnectionHandler) Get(c *gin.Context) {
	id := c.Param("id")
	conn := db.Connection{}
	err := h.db.QueryRow(`
		SELECT id, provider_type_id, name, auth_type, status,
		       cooldown_until, last_error, last_error_code,
		       last_success_at, last_failure_at, failure_count,
		       capabilities, is_active, created_at, updated_at
		FROM connections WHERE id = ?
	`, id).Scan(&conn.ID, &conn.ProviderTypeID, &conn.Name, &conn.AuthType,
		&conn.Status, &conn.CooldownUntil, &conn.LastError, &conn.LastErrorCode,
		&conn.LastSuccessAt, &conn.LastFailureAt, &conn.FailureCount,
		&conn.Capabilities, &conn.IsActive, &conn.CreatedAt, &conn.UpdatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, conn)
}

// Update modifies a connection.
func (h *ConnectionHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name         string `json:"name"`
		APIKey       string `json:"api_key"`
		Status       string `json:"status"`
		IsActive     *bool  `json:"is_active"`
		Capabilities string `json:"capabilities"`
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
	if req.APIKey != "" {
		sets = append(sets, "api_key = ?")
		args = append(args, req.APIKey)
	}
	if req.Status != "" {
		sets = append(sets, "status = ?")
		args = append(args, req.Status)
	}
	if req.IsActive != nil {
		sets = append(sets, "is_active = ?")
		args = append(args, boolToInt(*req.IsActive))
	}
	if req.Capabilities != "" {
		sets = append(sets, "capabilities = ?")
		args = append(args, req.Capabilities)
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().Unix())
	args = append(args, id)

	result, err := h.db.Exec("UPDATE connections SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete soft-deletes a connection.
func (h *ConnectionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	result, err := h.db.Exec(`UPDATE connections SET is_active = 0, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestConnection tests a single connection by calling its Models endpoint.
func (h *ConnectionHandler) TestConnection(c *gin.Context) {
	id := c.Param("id")

	// Load connection
	var conn struct {
		ID             string
		ProviderTypeID string
		APIKey         string
		AccessToken    string
		BaseURL        string
	}
	err := h.db.QueryRow(`
		SELECT c.id, c.provider_type_id, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''),
		       COALESCE(pt.base_url,'')
		FROM connections c JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.id = ?
	`, id).Scan(&conn.ID, &conn.ProviderTypeID, &conn.APIKey, &conn.AccessToken, &conn.BaseURL)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get executor
	exec, _, ok := h.registry.Get(conn.ProviderTypeID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown provider: " + conn.ProviderTypeID})
		return
	}

	// Try Models() endpoint
	start := time.Now()
	req := &executor.Request{
		APIKey:      conn.APIKey,
		AccessToken: conn.AccessToken,
		BaseURL:     conn.BaseURL,
		Provider:    conn.ProviderTypeID,
	}

	tester, ok := exec.(modelTester)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "skipped",
			"message":       "provider does not support model discovery",
		})
		return
	}

	resp, err := tester.Models(c.Request.Context(), req)
	latency := time.Since(start)

	if err != nil {
		det := connstate.DetectError(0, "", err, conn.ProviderTypeID, "", nil)
		if h.store != nil {
			h.store.RecordFailure(id, det)
		}
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "failed",
			"error":         err.Error(),
			"latency_ms":    latency.Milliseconds(),
		})
		return
	}

	if h.store != nil {
		h.store.RecordSuccess(id)
	}

	c.JSON(http.StatusOK, gin.H{
		"connection_id": id,
		"status":        "ok",
		"status_code":   resp.StatusCode,
		"latency_ms":    latency.Milliseconds(),
	})
}

// ResetStatus resets a connection's status and syncs in-memory state.
func (h *ConnectionHandler) ResetStatus(c *gin.Context) {
	id := c.Param("id")
	result, err := h.db.Exec(`
		UPDATE connections SET status = 'ready', cooldown_until = NULL,
		last_error = NULL, last_error_code = NULL, failure_count = 0, updated_at = ?
		WHERE id = ?
	`, time.Now().Unix(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}

	// Sync in-memory state
	if h.store != nil {
		h.store.UpdateStatus(id, connstate.StatusReady)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BulkUpdate updates multiple connections at once.
func (h *ConnectionHandler) BulkUpdate(c *gin.Context) {
	var req struct {
		IDs      []string `json:"ids" binding:"required"`
		Action   string   `json:"action" binding:"required"`
		Status   string   `json:"status"`
		IsActive *bool    `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().Unix()
	placeholders := make([]string, len(req.IDs))
	args := make([]interface{}, 0, len(req.IDs)+3)
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	switch req.Action {
	case "disable":
		args = append([]interface{}{false, now}, args...)
		h.db.Exec("UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN ("+inClause+")", args...)
	case "enable":
		args = append([]interface{}{true, now}, args...)
		h.db.Exec("UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN ("+inClause+")", args...)
	case "reset":
		args = append([]interface{}{now}, args...)
		h.db.Exec("UPDATE connections SET status = 'ready', failure_count = 0, updated_at = ? WHERE id IN ("+inClause+")", args...)
	case "delete":
		args = append([]interface{}{false, now}, args...)
		h.db.Exec("UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN ("+inClause+")", args...)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "affected": len(req.IDs)})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
