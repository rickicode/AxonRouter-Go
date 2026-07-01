package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// ProviderHandler handles provider CRUD operations.
type ProviderHandler struct {
	db       *sql.DB
	registry *executor.Registry
	store    *connstate.Store
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store) *ProviderHandler {
	return &ProviderHandler{db: database, registry: registry, store: store}
}

// List returns all providers with connection counts.
func (h *ProviderHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
		SELECT pt.id, pt.display_name, pt.format, pt.base_url, pt.is_custom, pt.custom_headers, pt.created_at,
		       COUNT(c.id) as connection_count
		FROM provider_types pt
		LEFT JOIN connections c ON c.provider_type_id = pt.id AND c.is_active = 1
		GROUP BY pt.id
		ORDER BY pt.display_name
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Collect all providers first, then close rows before nested queries
	var providers []db.ProviderWithCounts
	for rows.Next() {
		p := db.ProviderWithCounts{}
		rows.Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
			&p.IsCustom, &p.CustomHeaders, &p.CreatedAt, &p.ConnectionCount)
		providers = append(providers, p)
	}
	rows.Close()

	// Fill status counts after outer rows are closed (avoids SQLite deadlock)
	for i := range providers {
		providers[i].StatusCounts = h.getStatusCounts(providers[i].ID)
	}

	c.JSON(http.StatusOK, gin.H{"data": providers})
}

// Get returns a single provider with its connection status breakdown.
func (h *ProviderHandler) Get(c *gin.Context) {
	id := c.Param("id")
	p := db.ProviderWithCounts{}
	err := h.db.QueryRow(`
		SELECT id, display_name, format, base_url, is_custom, custom_headers, created_at
		FROM provider_types WHERE id = ?
	`, id).Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
		&p.IsCustom, &p.CustomHeaders, &p.CreatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = ? AND is_active = 1`, id).Scan(&p.ConnectionCount)
	p.StatusCounts = h.getStatusCounts(id)

	c.JSON(http.StatusOK, p)
}

// Create adds a custom provider.
func (h *ProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name           string            `json:"name" binding:"required"`
		DisplayName    string            `json:"display_name"`
		Format         string            `json:"format" binding:"required"`
		BaseURL        string            `json:"base_url" binding:"required"`
		CustomHeaders  map[string]string `json:"custom_headers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check uniqueness
	var exists int
	h.db.QueryRow(`SELECT COUNT(*) FROM provider_types WHERE id = ?`, req.Name).Scan(&exists)
	if exists > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "provider name already exists"})
		return
	}

	headersJSON := sql.NullString{}
	if req.CustomHeaders != nil {
		b, err := json.Marshal(req.CustomHeaders)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid custom_headers: " + err.Error()})
			return
		}
		headersJSON = sql.NullString{String: string(b), Valid: true}
	}

	now := time.Now().Unix()
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Name
	}
	_, err := h.db.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, is_custom, custom_headers, created_at)
		VALUES (?, ?, ?, ?, 1, ?, ?)
	`, req.Name, displayName, req.Format, req.BaseURL, headersJSON, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":           req.Name,
		"display_name": displayName,
		"format":       req.Format,
		"base_url":     req.BaseURL,
		"is_custom":    true,
	})
}

// Update modifies a provider.
func (h *ProviderHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		DisplayName   string            `json:"display_name"`
		BaseURL       string            `json:"base_url"`
		CustomHeaders map[string]string `json:"custom_headers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []string{}
	args := []interface{}{}
	if req.DisplayName != "" {
		updates = append(updates, "display_name = ?")
		args = append(args, req.DisplayName)
	}
	if req.BaseURL != "" {
		updates = append(updates, "base_url = ?")
		args = append(args, req.BaseURL)
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}

	args = append(args, id)
	query := "UPDATE provider_types SET " + joinStrings(updates, ", ") + " WHERE id = ?"
	result, err := h.db.Exec(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a custom provider (only if no connections).
func (h *ProviderHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	// Check if custom
	var isCustom bool
	h.db.QueryRow(`SELECT is_custom FROM provider_types WHERE id = ?`, id).Scan(&isCustom)
	if !isCustom {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete built-in provider"})
		return
	}

	// Check connections
	var connCount int
	h.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = ?`, id).Scan(&connCount)
	if connCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "provider has connections, delete them first"})
		return
	}

	h.db.Exec(`DELETE FROM provider_types WHERE id = ?`, id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ProviderHandler) getStatusCounts(providerID string) map[string]int {
	counts := make(map[string]int)
	rows, err := h.db.Query(`
		SELECT status, COUNT(*) FROM connections
		WHERE provider_type_id = ? AND is_active = 1
		GROUP BY status
	`, providerID)
	if err != nil {
		return counts
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		counts[status] = count
	}
	return counts
}

// TestAll tests all connections for a provider using streaming.
func (h *ProviderHandler) TestAll(c *gin.Context) {
	providerID := c.Param("id")

	// Get provider format
	var format string
	err := h.db.QueryRow(`SELECT format FROM provider_types WHERE id = ?`, providerID).Scan(&format)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	rows, err := h.db.Query(`
		SELECT c.id, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''),
		       COALESCE(pt.base_url,'')
		FROM connections c JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.provider_type_id = ? AND c.is_active = 1
	`, providerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type testResult struct {
		ConnectionID string `json:"connection_id"`
		Status       string `json:"status"`
		Error        string `json:"error,omitempty"`
		LatencyMs    int64  `json:"latency_ms"`
	}

	exec, _, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no executor for provider: " + providerID})
		return
	}

	bodyBytes := buildTestBody(format, defaultTestModel(providerID))
	var results []testResult
	for rows.Next() {
		var connID, apiKey, accessToken, baseURL string
		rows.Scan(&connID, &apiKey, &accessToken, &baseURL)

		start := time.Now()
		streamResult, err := exec.ExecuteStream(context.Background(), &executor.Request{
			APIKey:      apiKey,
			AccessToken: accessToken,
			BaseURL:     baseURL,
			Body:        bodyBytes,
			Provider:    providerID,
		})
		if err != nil {
			latency := time.Since(start).Milliseconds()
			if h.store != nil {
				det := connstate.DetectError(0, "", err, providerID, "", nil)
				h.store.RecordFailure(connID, det)
			}
			results = append(results, testResult{ConnectionID: connID, Status: "failed", Error: err.Error(), LatencyMs: latency})
			continue
		}

		// Drain stream
		var firstErr error
		for chunk := range streamResult.Chunks {
			if chunk.Err != nil {
				firstErr = chunk.Err
				break
			}
		}
		latency := time.Since(start).Milliseconds()

		if firstErr != nil {
			if h.store != nil {
				det := connstate.DetectError(0, "", firstErr, providerID, "", nil)
				h.store.RecordFailure(connID, det)
			}
			results = append(results, testResult{ConnectionID: connID, Status: "failed", Error: firstErr.Error(), LatencyMs: latency})
		} else {
			if h.store != nil {
				h.store.RecordSuccess(connID)
			}
			results = append(results, testResult{ConnectionID: connID, Status: "ok", LatencyMs: latency})
		}
	}

	c.JSON(http.StatusOK, gin.H{"provider_id": providerID, "results": results})
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}

// AddConnection adds a connection to a provider.
func (h *ProviderHandler) AddConnection(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		Name   string `json:"name" binding:"required"`
		APIKey string `json:"api_key"`
		AuthType string `json:"auth_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify provider exists
	var exists bool
	h.db.QueryRow(`SELECT COUNT(*) > 0 FROM provider_types WHERE id = ?`, providerID).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	connID := uuid.New().String()
	now := time.Now().Unix()
	if req.AuthType == "" {
		req.AuthType = "api_key"
	}

	apiKey := sql.NullString{}
	if req.APIKey != "" {
		apiKey = sql.NullString{String: req.APIKey, Valid: true}
	}

	initialStatus := "ready"
	if req.AuthType == "oauth" {
		initialStatus = "auth_failed" // not eligible until OAuth completes
	}

	_, err := h.db.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, status, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, connID, providerID, req.Name, req.AuthType, apiKey, initialStatus, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": connID, "name": req.Name, "status": initialStatus})
}

// BulkAddConnections adds multiple connections at once.
func (h *ProviderHandler) BulkAddConnections(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		Connections []struct {
			Name   string `json:"name"`
			APIKey string `json:"api_key"`
		} `json:"connections"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().Unix()
	var created int
	for _, conn := range req.Connections {
		connID := uuid.New().String()
		apiKey := sql.NullString{String: conn.APIKey, Valid: conn.APIKey != ""}
		_, err := h.db.Exec(`
			INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, status, is_active, created_at, updated_at)
			VALUES (?, ?, ?, 'api_key', ?, 'ready', 1, ?, ?)
		`, connID, providerID, conn.Name, apiKey, now, now)
		if err == nil {
			created++
		}
	}

	c.JSON(http.StatusCreated, gin.H{"created": created, "total": len(req.Connections)})
}

// ValidateKey checks if an API key is valid for a provider by attempting a lightweight model list request.
func (h *ProviderHandler) ValidateKey(c *gin.Context) {
	var req struct {
		Provider string `json:"provider" binding:"required"`
		APIKey   string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get provider base URL and format
	var provider struct {
		ID      string
		Format  string
		BaseURL string
	}
	dbErr := h.db.QueryRow(`SELECT id, format, base_url FROM provider_types WHERE id = ?`, req.Provider).Scan(&provider.ID, &provider.Format, &provider.BaseURL)
	if dbErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Resolve executor
	executorID := req.Provider
	if dbErr == nil {
		executorID = provider.ID
	}
	exec, _, ok := h.registry.Get(executorID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no executor for provider"})
		return
	}

	// Try a lightweight request to validate the key
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	testModel := defaultTestModel(req.Provider)
	if testModel == "" {
		// No known model — just check if the key can list models
		testModel = "gpt-4o-mini"
	}

	resp, err := exec.ExecuteStream(ctx, &executor.Request{
		APIKey:   req.APIKey,
		BaseURL:  provider.BaseURL,
		Body:     buildTestBody(provider.Format, testModel),
		Provider: req.Provider,
		Model:    testModel,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}

	var valid bool
	for chunk := range resp.Chunks {
		if chunk.Err != nil {
			break
		}
		if chunk.Payload != nil {
			valid = true
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"valid": valid})
}
