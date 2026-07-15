package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// modelTester is an interface for testing provider connectivity via Models() endpoint.
// Used by ListModels for dynamic model discovery.
type modelTester interface {
	Models(ctx context.Context, req *executor.Request) (*executor.Response, error)
}

// ConnectionHandler handles connection CRUD operations.
type ConnectionHandler struct {
	db         *sql.DB
	registry   *executor.Registry
	store      *connstate.Store
	elig       *connstate.EligibilityManager
	exhaustion *quota.ExhaustionCache
	authMgr    *auth.Manager
	writeQueue *db.WriteQueue
}

// NewConnectionHandler creates a new connection handler.
func NewConnectionHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store, elig *connstate.EligibilityManager, exhaustion *quota.ExhaustionCache, authMgr *auth.Manager, writeQueue *db.WriteQueue) *ConnectionHandler {
	return &ConnectionHandler{db: database, registry: registry, store: store, elig: elig, exhaustion: exhaustion, authMgr: authMgr, writeQueue: writeQueue}
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
 COALESCE(priority, 0), cooldown_until, last_error, last_error_code,
 last_success_at, last_failure_at, failure_count,
 capabilities, is_active, created_at, updated_at,
 COALESCE(oauth_expires_at, 0),
 COALESCE(provider_specific_data, '')
 FROM connections WHERE `+where+`
 ORDER BY priority DESC, created_at DESC
 LIMIT ? OFFSET ?
 `, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var conns []gin.H
	for rows.Next() {
		conn := db.Connection{}
		var psd string
		rows.Scan(&conn.ID, &conn.ProviderTypeID, &conn.Name, &conn.AuthType,
			&conn.Status, &conn.Priority, &conn.CooldownUntil, &conn.LastError, &conn.LastErrorCode,
			&conn.LastSuccessAt, &conn.LastFailureAt, &conn.FailureCount,
			&conn.Capabilities, &conn.IsActive, &conn.CreatedAt, &conn.UpdatedAt,
			&conn.OAuthExpiresAt, &psd)
		entry := connToJSON(conn)
		if psd != "" {
			entry["provider_specific_data"] = psd
		} else {
			entry["provider_specific_data"] = nil
		}
		conns = append(conns, entry)
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
	var psd sql.NullString
	err := h.db.QueryRow(`
		SELECT id, provider_type_id, name, auth_type, status,
		       cooldown_until, last_error, last_error_code,
		       last_success_at, last_failure_at, failure_count,
		       capabilities, is_active, created_at, updated_at,
		       COALESCE(provider_specific_data, ''),
		       COALESCE(oauth_expires_at, 0)
		FROM connections WHERE id = ?
	`, id).Scan(&conn.ID, &conn.ProviderTypeID, &conn.Name, &conn.AuthType,
		&conn.Status, &conn.CooldownUntil, &conn.LastError, &conn.LastErrorCode,
		&conn.LastSuccessAt, &conn.LastFailureAt, &conn.FailureCount,
		&conn.Capabilities, &conn.IsActive, &conn.CreatedAt, &conn.UpdatedAt,
		&psd, &conn.OAuthExpiresAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := connToJSON(conn)
	resp["provider_specific_data"] = nullStr(psd)
	c.JSON(http.StatusOK, resp)
}

// Update modifies a connection.
func (h *ConnectionHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name                 string `json:"name"`
		APIKey               string `json:"api_key"`
		Status               string `json:"status"`
		IsActive             *bool  `json:"is_active"`
		Capabilities         string `json:"capabilities"`
		ProviderSpecificData string `json:"provider_specific_data"`
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
	if req.ProviderSpecificData != "" {
		sets = append(sets, "provider_specific_data = ?")
		args = append(args, req.ProviderSpecificData)
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

	// Sync in-memory state
	if h.store != nil && req.Status != "" {
		h.store.UpdateStatus(id, connstate.Status(req.Status))
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}
	if req.IsActive != nil && !*req.IsActive && h.store != nil {
		h.store.UpdateStatus(id, connstate.StatusDisabled)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete soft-deletes a connection.
func (h *ConnectionHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	// Block deletion of any default direct connection (OpenCode Free, MiMoCode, etc.).
	var psd string
	h.db.QueryRow("SELECT COALESCE(provider_specific_data, '') FROM connections WHERE id = ?", id).Scan(&psd)
	if strings.Contains(psd, `"direct":"true"`) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete the default direct connection"})
		return
	}

	result, err := h.db.Exec(`UPDATE connections SET is_active = 0, updated_at = ? WHERE id = ?`, time.Now().Unix(), id)
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
		h.store.UpdateStatus(id, connstate.StatusDisabled)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestConnection tests a single connection by sending a minimal streaming request.
// Uses ExecuteStream() for all providers — each executor sets correct headers/format.
func (h *ConnectionHandler) TestConnection(c *gin.Context) {
	id := c.Param("id")

	// Load connection + provider format
	var conn struct {
		ID             string
		ProviderTypeID string
		Format         string
		APIKey         string
		AccessToken    string
		BaseURL        string
		PSDRaw         string
	}
	err := h.db.QueryRow(`
		SELECT c.id, c.provider_type_id, pt.format, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''),
		       COALESCE(pt.base_url,''), COALESCE(c.provider_specific_data, '')
		FROM connections c JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.id = ?
	`, id).Scan(&conn.ID, &conn.ProviderTypeID, &conn.Format, &conn.APIKey, &conn.AccessToken, &conn.BaseURL, &conn.PSDRaw)
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

	// Build format-specific test body with default model for this provider
	model := defaultTestModel(conn.ProviderTypeID)
	bodyBytes := buildTestBody(conn.Format, model)

	// Parse provider_specific_data
	var psdMap map[string]string
	if conn.PSDRaw != "" {
		json.Unmarshal([]byte(conn.PSDRaw), &psdMap)
	}

	start := time.Now()
	streamResult, err := exec.ExecuteStream(c.Request.Context(), &executor.Request{
		APIKey:               conn.APIKey,
		AccessToken:          conn.AccessToken,
		BaseURL:              conn.BaseURL,
		Body:                 bodyBytes,
		Provider:             conn.ProviderTypeID,
		Model:                model,
		ProviderSpecificData: psdMap,
	})
	if err != nil {
		latency := time.Since(start).Milliseconds()
		det := connstate.DetectError(context.Background(), 0, "", err, conn.ProviderTypeID, "", nil)
		h.recordTestFailure(id, det)
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "failed",
			"error":         err.Error(),
			"latency_ms":    latency,
		})
		return
	}

	// Read first chunk to verify connectivity, drain rest
	var firstErr error
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			firstErr = chunk.Err
			break
		}
	}
	latency := time.Since(start).Milliseconds()

	if firstErr != nil {
		det := connstate.DetectError(context.Background(), 0, "", firstErr, conn.ProviderTypeID, "", nil)
		h.recordTestFailure(id, det)
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "failed",
			"error":         firstErr.Error(),
			"latency_ms":    latency,
		})
		return
	}

	h.recordTestSuccess(id)

	c.JSON(http.StatusOK, gin.H{
		"connection_id": id,
		"status":        "ok",
		"status_code":   streamResult.StatusCode,
		"latency_ms":    latency,
	})
}

// recordTestSuccess marks a connection as ready after a successful test,
// clears any cooldown/error/exhaustion state, and refreshes eligibility.
func (h *ConnectionHandler) recordTestSuccess(connID string) {
	now := time.Now().Unix()
	if _, err := h.db.Exec(`
		UPDATE connections SET status = 'ready', cooldown_until = NULL,
		last_error = NULL, last_error_code = NULL, failure_count = 0,
		last_success_at = ?, updated_at = ? WHERE id = ?
	`, now, now, connID); err != nil {
		log.Printf("recordTestSuccess db update failed for %s: %v", connID, err)
	}
	if h.store != nil {
		h.store.RecordSuccess(connID)
		h.store.UpdateStatus(connID, connstate.StatusReady)
	}
	if h.exhaustion != nil {
		h.exhaustion.Clear(connID)
	}
	if h.elig != nil {
		h.elig.Update(h.store)
	}
}

// recordTestFailure persists a test failure just like the proxy path does:
// cooldown, last_error, failure_count, last_failure_at in the DB plus the
// in-memory exhaustion cache and status update.
func (h *ConnectionHandler) recordTestFailure(connID string, det connstate.ErrorDetection) {
	if h.store != nil {
		h.store.RecordFailure(connID, det)
	}
	status := det.Status
	if det.Category == connstate.ErrorQuota {
		status = connstate.StatusQuotaExhausted
	}
	if status != "" && h.store != nil {
		h.store.UpdateStatus(connID, status)
	}
	if status != "" {
		errCode := string(det.Category)
		now := time.Now().Unix()
		if det.CooldownUntil != nil {
			if _, err := h.db.Exec(`
				UPDATE connections SET status = ?, cooldown_until = ?, last_error = ?, last_error_code = ?,
				failure_count = failure_count + 1, last_failure_at = ?, updated_at = ? WHERE id = ?
			`, status, det.CooldownUntil.Unix(), det.Message, errCode, now, now, connID); err != nil {
				log.Printf("recordTestFailure db update failed for %s: %v", connID, err)
			}
		} else {
			if _, err := h.db.Exec(`
				UPDATE connections SET status = ?, last_error = ?, last_error_code = ?,
				failure_count = failure_count + 1, last_failure_at = ?, updated_at = ? WHERE id = ?
			`, status, det.Message, errCode, now, now, connID); err != nil {
				log.Printf("recordTestFailure db update failed for %s: %v", connID, err)
			}
		}
	}
	if h.exhaustion != nil {
		switch det.Category {
		case connstate.ErrorRateLimit:
			h.exhaustion.MarkExhausted(connID, quota.TTLFromCooldown(det.CooldownUntil, quota.DefaultExhaustionTTL))
		case connstate.ErrorQuota:
			ttl := 24 * time.Hour
			if det.CooldownUntil != nil {
				ttl = time.Until(*det.CooldownUntil)
			}
			h.exhaustion.MarkExhausted(connID, ttl)
		}
	}
	if h.elig != nil {
		h.elig.Update(h.store)
	}
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
		if h.exhaustion != nil {
			h.exhaustion.Clear(id)
		}
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

	var query string
	var status connstate.Status
	switch req.Action {
	case "disable":
		query = "UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN (" + inClause + ")"
		args = append([]interface{}{false, now}, args...)
		status = connstate.StatusDisabled
	case "enable":
		query = "UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN (" + inClause + ")"
		args = append([]interface{}{true, now}, args...)
		status = connstate.StatusReady
	case "reset":
		query = "UPDATE connections SET status = 'ready', failure_count = 0, updated_at = ? WHERE id IN (" + inClause + ")"
		args = append([]interface{}{now}, args...)
		status = connstate.StatusReady
	case "delete":
		query = "UPDATE connections SET is_active = ?, updated_at = ? WHERE id IN (" + inClause + ")"
		args = append([]interface{}{false, now}, args...)
		status = connstate.StatusDisabled
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown action: " + req.Action})
		return
	}

	run := func(d *sql.DB) error {
		tx, e := d.Begin()
		if e != nil {
			return e
		}
		defer func() {
			if e != nil {
				_ = tx.Rollback()
			}
		}()
		if _, e = tx.Exec(query, args...); e != nil {
			return e
		}
		return tx.Commit()
	}
	var doErr error
	if h.writeQueue == nil {
		doErr = run(h.db)
	} else {
		doErr = h.writeQueue.Do(c.Request.Context(), "bulk-update-connections", run)
	}
	if doErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": doErr.Error()})
		return
	}

	// Only mutate in-memory state after the DB write committed successfully.
	if h.store != nil {
		for _, id := range req.IDs {
			h.store.UpdateStatus(id, status)
		}
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "affected": len(req.IDs)})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(v sql.NullString) interface{} {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullInt64(v sql.NullInt64) interface{} {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func connToJSON(conn db.Connection) gin.H {
	var expAt int64
	if conn.OAuthExpiresAt.Valid {
		expAt = conn.OAuthExpiresAt.Int64
	}
	return gin.H{
		"id":               conn.ID,
		"provider_type_id": conn.ProviderTypeID,
		"name":             conn.Name,
		"auth_type":        conn.AuthType,
		"status":           conn.Status,
		"priority":         conn.Priority,
		"cooldown_until":   nullInt64(conn.CooldownUntil),
		"last_error":       nullStr(conn.LastError),
		"last_error_code":  nullStr(conn.LastErrorCode),
		"last_success_at":  nullInt64(conn.LastSuccessAt),
		"last_failure_at":  nullInt64(conn.LastFailureAt),
		"failure_count":    conn.FailureCount,
		"capabilities":     nullStr(conn.Capabilities),
		"is_active":        conn.IsActive,
		"created_at":       conn.CreatedAt,
		"updated_at":       conn.UpdatedAt,
		"oauth_expires_at": expAt,
	}
}

// RefreshToken manually refreshes an OAuth token for a connection.
func (h *ConnectionHandler) RefreshToken(c *gin.Context) {
	connID := c.Param("id")

	var providerTypeID, refreshToken string
	var expiresAt int64
	var accessToken string
	err := h.db.QueryRow(`
		SELECT provider_type_id, COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0)
		FROM connections WHERE id = ?
	`, connID).Scan(&providerTypeID, &accessToken, &refreshToken, &expiresAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}

	// GitHub device-code OAuth does not return a refresh token, but the
	// Copilot bearer token can still be refreshed from the stored access token.
	if refreshToken == "" && providerTypeID != "copilot" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no refresh token available"})
		return
	}

	if h.authMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth manager not configured"})
		return
	}

	creds := &auth.Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Unix(expiresAt, 0),
	}

	newCreds, err := h.authMgr.RefreshToken(c.Request.Context(), auth.ProviderType(providerTypeID), creds)
	if err != nil {
		log.Printf("Manual token refresh failed for %s: %v", connID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().Unix()
	if len(newCreds.ProviderSpecific) > 0 {
		psdJSON, _ := json.Marshal(newCreds.ProviderSpecific)
		_, err = h.db.Exec(`
			UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, provider_specific_data = ?, updated_at = ? WHERE id = ?
		`, newCreds.AccessToken, newCreds.RefreshToken, newCreds.ExpiresAt.Unix(), psdJSON, now, connID)
	} else {
		_, err = h.db.Exec(`
			UPDATE connections SET oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?, updated_at = ? WHERE id = ?
		`, newCreds.AccessToken, newCreds.RefreshToken, newCreds.ExpiresAt.Unix(), now, connID)
	}
	if err != nil {
		log.Printf("Failed to persist refreshed token for %s: %v", connID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist token"})
		return
	}

	if h.store != nil {
		h.store.UpdateStatus(connID, connstate.StatusReady)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"expires_at": newCreds.ExpiresAt.Unix(),
		"message":    "Token refreshed successfully",
	})
}
