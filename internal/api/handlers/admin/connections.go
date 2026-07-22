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
	"github.com/rickicode/AxonRouter-Go/internal/background"
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
	db           *sql.DB
	registry     *executor.Registry
	store        *connstate.Store
	elig         *connstate.EligibilityManager
	exhaustion   *quota.ExhaustionCache
	authMgr      *auth.Manager
	writeQueue   *db.WriteQueue
	lifecycleMgr *background.LifecycleManager
}

// NewConnectionHandler creates a new connection handler.
func NewConnectionHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store, elig *connstate.EligibilityManager, exhaustion *quota.ExhaustionCache, authMgr *auth.Manager, writeQueue *db.WriteQueue, lifecycleMgr *background.LifecycleManager) *ConnectionHandler {
	return &ConnectionHandler{db: database, registry: registry, store: store, elig: elig, exhaustion: exhaustion, authMgr: authMgr, writeQueue: writeQueue, lifecycleMgr: lifecycleMgr}
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
		AuthType       string
		APIKey         string
		AccessToken    string
		RefreshToken   string
		ExpiresAt      int64
		BaseURL        string
		PSDRaw         string
	}
	err := h.db.QueryRow(`
		SELECT c.id, c.provider_type_id, pt.format, COALESCE(c.auth_type,''), COALESCE(c.api_key,''),
		       COALESCE(c.oauth_token,''), COALESCE(c.oauth_refresh_token,''), COALESCE(c.oauth_expires_at,0),
		       COALESCE(pt.base_url,''), COALESCE(c.provider_specific_data, '')
		FROM connections c JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.id = ?
	`, id).Scan(&conn.ID, &conn.ProviderTypeID, &conn.Format, &conn.AuthType, &conn.APIKey,
		&conn.AccessToken, &conn.RefreshToken, &conn.ExpiresAt, &conn.BaseURL, &conn.PSDRaw)
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
	bodyBytes := buildTestBody(conn.Format, model, conn.ProviderTypeID)

	// Parse provider_specific_data
	var psdMap map[string]string
	if conn.PSDRaw != "" {
		json.Unmarshal([]byte(conn.PSDRaw), &psdMap)
	}

	ctx := c.Request.Context()
	accessToken := conn.AccessToken
	refreshToken := conn.RefreshToken
	expiresAt := conn.ExpiresAt

	// Refresh expired/near-expiry OAuth tokens before testing.
	if h.authMgr != nil && conn.AuthType == "oauth" && shouldRefreshTestToken(conn.ProviderTypeID, refreshToken, expiresAt) {
		creds := &auth.Credentials{
			AccessToken:      accessToken,
			RefreshToken:     refreshToken,
			ExpiresAt:        time.Unix(expiresAt, 0),
			ProviderSpecific: psdMap,
		}
		newCreds, refreshErr := h.authMgr.RefreshTokenForConnection(ctx, id, auth.ProviderType(conn.ProviderTypeID), creds)
		if refreshErr != nil {
			log.Printf("TestConnection proactive refresh failed for %s: %v", id, refreshErr)
		} else {
			accessToken = newCreds.AccessToken
			expiresAt = newCreds.ExpiresAt.Unix()
			refreshToken = newCreds.RefreshToken
			if refreshToken == "" {
				refreshToken = conn.RefreshToken
			}
			if len(newCreds.ProviderSpecific) > 0 {
				psdMap = newCreds.ProviderSpecific
			}
		}
	}

	req := &executor.Request{
		APIKey:               conn.APIKey,
		AccessToken:          accessToken,
		BaseURL:              conn.BaseURL,
		Body:                 bodyBytes,
		Provider:             conn.ProviderTypeID,
		Model:                model,
		ProviderSpecificData: psdMap,
	}
	latency, statusCode, testErr := h.runTestAttempt(ctx, exec, req)
	if testErr == nil {
		h.recordTestSuccess(id)
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "ok",
			"status_code":   statusCode,
			"latency_ms":    latency,
		})
		return
	}

	if upErr, ok := grokCLITestSoftSuccess(conn.ProviderTypeID, testErr); ok {
		h.recordTestSuccess(id)
		c.JSON(http.StatusOK, gin.H{
			"connection_id": id,
			"status":        "ok",
			"status_code":   upErr.StatusCode,
			"latency_ms":    latency,
			"message":       "Authentication succeeded, but credits/quota are exhausted. The connection remains ready.",
		})
		return
	}

	det := connstate.DetectError(ctx, 0, "", testErr, conn.ProviderTypeID, "", nil)
	if h.authMgr != nil && conn.AuthType == "oauth" && refreshToken != "" && isRefreshableTestError(conn.ProviderTypeID, testErr, det) {
		creds := &auth.Credentials{
			AccessToken:      accessToken,
			RefreshToken:     refreshToken,
			ExpiresAt:        time.Unix(expiresAt, 0),
			ProviderSpecific: psdMap,
		}
		newCreds, refreshErr := h.authMgr.RefreshTokenForConnection(ctx, id, auth.ProviderType(conn.ProviderTypeID), creds)
		if refreshErr == nil {
			accessToken = newCreds.AccessToken
			expiresAt = newCreds.ExpiresAt.Unix()
			refreshToken = newCreds.RefreshToken
			if refreshToken == "" {
				refreshToken = conn.RefreshToken
			}
			if len(newCreds.ProviderSpecific) > 0 {
				psdMap = newCreds.ProviderSpecific
			}

			req.AccessToken = accessToken
			req.ProviderSpecificData = psdMap
			latency, statusCode, testErr = h.runTestAttempt(ctx, exec, req)
			if testErr == nil {
				h.recordTestSuccess(id)
				c.JSON(http.StatusOK, gin.H{
					"connection_id": id,
					"status":        "ok",
					"status_code":   statusCode,
					"latency_ms":    latency,
				})
				return
			}
			if upErr, ok := grokCLITestSoftSuccess(conn.ProviderTypeID, testErr); ok {
				h.recordTestSuccess(id)
				c.JSON(http.StatusOK, gin.H{
					"connection_id": id,
					"status":        "ok",
					"status_code":   upErr.StatusCode,
					"latency_ms":    latency,
					"message":       "Authentication succeeded, but credits/quota are exhausted. The connection remains ready.",
				})
				return
			}
			det = connstate.DetectError(ctx, 0, "", testErr, conn.ProviderTypeID, "", nil)
		} else {
			log.Printf("TestConnection reactive refresh failed for %s: %v", id, refreshErr)
		}
	}

	h.recordTestFailure(id, det)
	c.JSON(http.StatusOK, gin.H{
		"connection_id": id,
		"status":        "failed",
		"error":         testErr.Error(),
		"latency_ms":    latency,
	})
}

// runTestAttempt executes the stream and drains the first chunk, returning the
// latency, HTTP status code on success, and the first error encountered.
func (h *ConnectionHandler) runTestAttempt(ctx context.Context, exec executor.Executor, req *executor.Request) (int64, int, error) {
	start := time.Now()
	streamResult, err := exec.ExecuteStream(ctx, req)
	if err != nil {
		return time.Since(start).Milliseconds(), 0, err
	}

	var firstErr error
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			firstErr = chunk.Err
			break
		}
	}
	latency := time.Since(start).Milliseconds()
	if firstErr != nil {
		return latency, 0, firstErr
	}
	return latency, streamResult.StatusCode, nil
}

// grokCLITestSoftSuccess reports whether a Grok CLI connection test error is an
// HTTP 402 indicating valid authentication but exhausted credits/quota. This is
// intentionally limited to grok-cli: other providers treat 402 as a hard
// balance/quota failure and must not be masked as success.
func grokCLITestSoftSuccess(providerID string, err error) (*executor.UpstreamError, bool) {
	if providerID != "grok-cli" {
		return nil, false
	}
	upErr, ok := err.(*executor.UpstreamError)
	if !ok {
		return nil, false
	}
	if upErr.StatusCode == http.StatusPaymentRequired {
		return upErr, true
	}
	return nil, false
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

// execWrite runs a DB write through the single-writer queue when available,
// falling back to a direct exec so callers can run with or without a queue.
func (h *ConnectionHandler) execWrite(ctx context.Context, label string, fn func(*sql.DB) error) error {
	if h.writeQueue != nil {
		return h.writeQueue.Do(ctx, label, fn)
	}
	return fn(h.db)
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
	var accessToken, psdRaw string
	err := h.db.QueryRow(`
		SELECT provider_type_id, COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data, '')
		FROM connections WHERE id = ?
	`, connID).Scan(&providerTypeID, &accessToken, &refreshToken, &expiresAt, &psdRaw)
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

	providerSpecific := map[string]string{}
	if psdRaw != "" {
		var raw map[string]any
		if e := json.Unmarshal([]byte(psdRaw), &raw); e == nil {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					providerSpecific[k] = s
				}
			}
		}
	}

	creds := &auth.Credentials{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		ExpiresAt:        time.Unix(expiresAt, 0),
		ProviderSpecific: providerSpecific,
	}

	newCreds, err := h.authMgr.RefreshTokenForConnection(c.Request.Context(), connID, auth.ProviderType(providerTypeID), creds)
	if err != nil {
		log.Printf("Manual token refresh failed for %s: %v", connID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
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

// CleanupConnections runs the connection lifecycle cleanup synchronously and
// returns how many stale disabled/auth_failed rows were deleted.
// POST /api/admin/system/connection-cleanup
func (h *ConnectionHandler) CleanupConnections(c *gin.Context) {
	if h.lifecycleMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lifecycle manager not configured"})
		return
	}

	deleted, err := h.lifecycleMgr.Cleanup()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cleanup failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}
