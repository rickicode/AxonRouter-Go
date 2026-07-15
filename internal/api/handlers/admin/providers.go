package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/provider"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// ProviderHandler handles provider CRUD operations.
type ProviderHandler struct {
	db *sql.DB
	registry *executor.Registry
	store *connstate.Store
	elig *connstate.EligibilityManager
	providerCfg *providercfg.Manager
	writeQueue *db.WriteQueue
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(database *sql.DB, registry *executor.Registry, store *connstate.Store, elig *connstate.EligibilityManager, providerCfg *providercfg.Manager, writeQueue *db.WriteQueue) *ProviderHandler {
	return &ProviderHandler{db: database, registry: registry, store: store, elig: elig, providerCfg: providerCfg, writeQueue: writeQueue}
}

// List returns all providers with connection counts.
func (h *ProviderHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`
	SELECT pt.id, pt.display_name, pt.format, pt.base_url, pt.is_custom, pt.custom_headers, pt.category, pt.service_kinds, pt.created_at,
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
		var serviceKindsRaw string
		rows.Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
			&p.IsCustom, &p.CustomHeaders, &p.Category, &serviceKindsRaw, &p.CreatedAt, &p.ConnectionCount)
		p.ServiceKinds = parseServiceKinds(serviceKindsRaw)
		providers = append(providers, p)
	}
	rows.Close()

	// Fill status counts after outer rows are closed (avoids SQLite deadlock)
	for i := range providers {
		providers[i].StatusCounts = h.getStatusCounts(providers[i].ID)
		if info, ok := provider.Registry[providers[i].ID]; ok {
			providers[i].Aliases = info.Aliases
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": providers})
}

// Get returns a single provider with its connection status breakdown.
func (h *ProviderHandler) Get(c *gin.Context) {
	id := c.Param("id")
	p := db.ProviderWithCounts{}
	var serviceKindsRaw string
	err := h.db.QueryRow(`
	SELECT id, display_name, format, base_url, is_custom, custom_headers, category, service_kinds, created_at
	FROM provider_types WHERE id = ?
	`, id).Scan(&p.ID, &p.DisplayName, &p.Format, &p.BaseURL,
		&p.IsCustom, &p.CustomHeaders, &p.Category, &serviceKindsRaw, &p.CreatedAt)
	p.ServiceKinds = parseServiceKinds(serviceKindsRaw)
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
	if info, ok := provider.Registry[id]; ok {
		p.Aliases = info.Aliases
	}
	c.JSON(http.StatusOK, p)
}

// Create adds a custom provider.
func (h *ProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		DisplayName string `json:"display_name"`
		Format string `json:"format" binding:"required"`
		BaseURL string `json:"base_url" binding:"required"`
		CustomHeaders map[string]string `json:"custom_headers"`
		Category string `json:"category"`
		ServiceKinds []string `json:"service_kinds"`
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
	category := req.Category
	if category == "" {
		category = "compatible"
	}
	serviceKinds := req.ServiceKinds
	if len(serviceKinds) == 0 {
		serviceKinds = []string{"llm"}
	}
	serviceKindsJSON, _ := json.Marshal(serviceKinds)

	_, err := h.db.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, is_custom, custom_headers, category, service_kinds, created_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)
	`, req.Name, displayName, req.Format, req.BaseURL, headersJSON, category, string(serviceKindsJSON), now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	executor.RegisterCustomProviders(h.db)

	c.JSON(http.StatusCreated, gin.H{
		"id": req.Name,
		"display_name": displayName,
		"format": req.Format,
		"base_url": req.BaseURL,
		"is_custom": true,
		"category": category,
		"service_kinds": serviceKinds,
	})
}

// Update modifies a provider.
func (h *ProviderHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		DisplayName string `json:"display_name"`
		BaseURL string `json:"base_url"`
		CustomHeaders map[string]string `json:"custom_headers"`
		Category string `json:"category"`
		ServiceKinds []string `json:"service_kinds"`
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
	if req.Category != "" {
		updates = append(updates, "category = ?")
		args = append(args, req.Category)
	}
	if len(req.ServiceKinds) > 0 {
		serviceKindsJSON, _ := json.Marshal(req.ServiceKinds)
		updates = append(updates, "service_kinds = ?")
		args = append(args, string(serviceKindsJSON))
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
	executor.GetRegistry().Unregister(id)
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

// parseServiceKinds parses the JSON service_kinds column and falls back to ["llm"]
// when the value is empty, malformed, or an empty array.
func parseServiceKinds(raw string) []string {
	if raw == "" {
		return []string{"llm"}
	}
	var kinds []string
	if err := json.Unmarshal([]byte(raw), &kinds); err != nil || len(kinds) == 0 {
		return []string{"llm"}
	}
	return kinds
}

// testAllBatchSize limits how many connections are tested in parallel at once.
// Connections are processed in fixed batches (not a sliding semaphore) so a
// provider with hundreds or thousands of connections never creates more than
// 10 in-flight goroutines/streams.
const testAllBatchSize = 10

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
		SELECT c.id, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''), COALESCE(pt.base_url,''), COALESCE(c.provider_specific_data, '')
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.provider_type_id = ? AND c.is_active = 1
	`, providerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type testInput struct {
		connID  string
		apiKey  string
		access  string
		baseURL string
		psdMap  map[string]string
	}
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
	ctx := c.Request.Context()

	var inputs []testInput
	for rows.Next() {
		var connID, apiKey, accessToken, baseURL, psdRaw string
		if err := rows.Scan(&connID, &apiKey, &accessToken, &baseURL, &psdRaw); err != nil {
			continue
		}
		var psdMap map[string]string
		if psdRaw != "" {
			json.Unmarshal([]byte(psdRaw), &psdMap)
		}
		inputs = append(inputs, testInput{connID: connID, apiKey: apiKey, access: accessToken, baseURL: baseURL, psdMap: psdMap})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	results := make([]testResult, len(inputs))

	// Process in fixed batches to cap peak concurrency.
	for batchStart := 0; batchStart < len(inputs); batchStart += testAllBatchSize {
		batchEnd := batchStart + testAllBatchSize
		if batchEnd > len(inputs) {
			batchEnd = len(inputs)
		}

		var wg sync.WaitGroup
		for i := batchStart; i < batchEnd; i++ {
			i, in := i, inputs[i]
			wg.Add(1)
			go func() {
				defer wg.Done()

				start := time.Now()
				streamResult, err := exec.ExecuteStream(ctx, &executor.Request{
					APIKey:               in.apiKey,
					AccessToken:          in.access,
					BaseURL:              in.baseURL,
					Body:                 bodyBytes,
					Provider:             providerID,
					ProviderSpecificData: in.psdMap,
				})
				if err != nil {
					latency := time.Since(start).Milliseconds()
					if h.store != nil {
						det := connstate.DetectError(context.Background(), 0, "", err, providerID, "", nil)
						h.store.RecordFailure(in.connID, det)
					}
					results[i] = testResult{ConnectionID: in.connID, Status: "failed", Error: err.Error(), LatencyMs: latency}
					return
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
						det := connstate.DetectError(context.Background(), 0, "", firstErr, providerID, "", nil)
						h.store.RecordFailure(in.connID, det)
					}
					results[i] = testResult{ConnectionID: in.connID, Status: "failed", Error: firstErr.Error(), LatencyMs: latency}
				} else {
					if h.store != nil {
						h.store.RecordSuccess(in.connID)
					}
					results[i] = testResult{ConnectionID: in.connID, Status: "ok", LatencyMs: latency}
				}
			}()
		}

		wg.Wait()
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
		Name                 string            `json:"name" binding:"required"`
		APIKey               string            `json:"api_key"`
		AuthType             string            `json:"auth_type"`
		Priority             int               `json:"priority"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
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

	// Validate CF connections require an Account ID
	if providerID == "cf" {
		accountID := req.ProviderSpecificData["accountId"]
		if accountID == "" {
			accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		if accountID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cloudflare Workers AI requires an Account ID. Add it in provider_specific_data.accountId or set CLOUDFLARE_ACCOUNT_ID env var."})
			return
		}
	}

	// OpenCode Free (oc): additional connections must use a proxy pool.
	// The default direct connection is auto-seeded by migration and cannot be added via API.
	if providerID == "oc" {
		proxyPoolID := req.ProviderSpecificData["proxyPoolId"]
		if proxyPoolID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "OpenCode Free connections require a proxy pool. Select one from the proxy pools list."})
			return
		}
		// Verify the proxy pool exists and is active
		var poolExists bool
		h.db.QueryRow("SELECT COUNT(*) > 0 FROM proxy_pools WHERE id = ? AND is_active = 1", proxyPoolID).Scan(&poolExists)
		if !poolExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Selected proxy pool not found or inactive"})
			return
		}
		// oc is no-auth; force auth_type to none
		req.AuthType = "none"
		req.APIKey = ""
		// Each additional oc connection is treated as a distinct logical account.
		// Assign a stable account id and label so it can be tracked/exhausted independently.
		if req.ProviderSpecificData["accountId"] == "" {
			req.ProviderSpecificData["accountId"] = "oc-" + uuid.New().String()[:8]
		}
		if req.ProviderSpecificData["accountLabel"] == "" {
			req.ProviderSpecificData["accountLabel"] = req.Name
		}
	}

	// MiMoCode: additional connections must use a proxy pool.
	// The default direct connection is auto-seeded by migration and cannot be added via API.
	if providerID == "mimocode" {
		proxyPoolID := req.ProviderSpecificData["proxyPoolId"]
		if proxyPoolID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MiMoCode connections require a proxy pool. Select one from the proxy pools list."})
			return
		}
		// Verify the proxy pool exists and is active
		var poolExists bool
		h.db.QueryRow("SELECT COUNT(*) > 0 FROM proxy_pools WHERE id = ? AND is_active = 1", proxyPoolID).Scan(&poolExists)
		if !poolExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Selected proxy pool not found or inactive"})
			return
		}
		// mimocode is no-auth; force auth_type to none
		req.AuthType = "none"
		req.APIKey = ""
		// Each additional mimocode connection is treated as a distinct logical account.
		// Assign a stable account id, label, and random fingerprint so it can be tracked/exhausted independently.
		if req.ProviderSpecificData["accountId"] == "" {
			req.ProviderSpecificData["accountId"] = "mimocode-" + uuid.New().String()[:8]
		}
		if req.ProviderSpecificData["accountLabel"] == "" {
			req.ProviderSpecificData["accountLabel"] = req.Name
		}
		if req.ProviderSpecificData["fingerprint"] == "" {
			req.ProviderSpecificData["fingerprint"] = generateFingerprint()
		}
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

	var psdJSON sql.NullString
	if len(req.ProviderSpecificData) > 0 {
		b, err := json.Marshal(req.ProviderSpecificData)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider_specific_data"})
			return
		}
		psdJSON = sql.NullString{String: string(b), Valid: true}
	}

	_, err := h.db.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, priority, provider_specific_data, status, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, connID, providerID, req.Name, req.AuthType, apiKey, req.Priority, psdJSON, initialStatus, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Seed in-memory store so eligibility routing picks up the new connection immediately
	if h.store != nil {
		h.store.SeedConnection(connID, providerID, initialStatus, req.Priority)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"id": connID, "name": req.Name, "status": initialStatus})
}

// BulkAddConnections adds multiple connections at once, processing them in
// batches. Each batch is written as a single transaction. When a WriteQueue is
// wired it runs through the queue (the app's only SQLite writer), so a large
// import never contends with the live gateway for the write lock. Per-row
// failures are reported instead of being silently dropped.
func (h *ProviderHandler) BulkAddConnections(c *gin.Context) {
	providerID := c.Param("id")
	var req struct {
		Connections []struct {
			Name string `json:"name"`
			APIKey string `json:"api_key"`
			Priority int `json:"priority"`
			ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
		} `json:"connections"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// OpenCode Free (oc): bulk add not supported — each connection requires a proxy pool.
	if providerID == "oc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OpenCode Free connections require a proxy pool and cannot be bulk-added. Use the single add connection form."})
		return
	}

	// Validate CF connections require Account ID (fail fast before any inserts).
	if providerID == "cf" {
		for i, conn := range req.Connections {
			accountID := conn.ProviderSpecificData["accountId"]
			if accountID == "" {
				accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
			}
			if accountID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "connection #" + fmt.Sprintf("%d", i+1) + ": Cloudflare Workers AI requires an Account ID"})
				return
			}
		}
	}

	const maxBulkConnections = 5000
	if len(req.Connections) > maxBulkConnections {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "too many connections: maximum " + strconv.Itoa(maxBulkConnections) + " per bulk import"})
		return
	}

	const batchSize = 200
	now := time.Now().Unix()
	var created, failed int
	var errors []string
	var seeded []struct {
		id string
		priority int
	}

	// batchResult is returned from the batch closure; the handler reads it
	// only AFTER WriteQueue.Do returns (the queue's done-channel establishes a
	// happens-before edge, so this is race-free — do NOT mutate handler
	// locals from inside the closure).
	type batchResult struct {
		seeded  []struct{ id string; priority int }
		created int
		fails   []string
		err     error
	}

	runBatch := func(d *sql.DB, conns []struct {
		Name string `json:"name"`
		APIKey string `json:"api_key"`
		Priority int `json:"priority"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
	}) batchResult {
		res := batchResult{}
		tx, err := d.Begin()
		if err != nil {
			res.err = err
			return res
		}
		for _, conn := range conns {
			connID := uuid.New().String()
			apiKey := sql.NullString{String: conn.APIKey, Valid: conn.APIKey != ""}
			var psdJSON sql.NullString
			if len(conn.ProviderSpecificData) > 0 {
				if b, err := json.Marshal(conn.ProviderSpecificData); err == nil {
					psdJSON = sql.NullString{String: string(b), Valid: true}
				}
			}
			if _, err := tx.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, api_key, priority, provider_specific_data, status, is_active, created_at, updated_at) VALUES (?, ?, ?, 'api_key', ?, ?, ?, 'ready', 1, ?, ?)`,
				connID, providerID, conn.Name, apiKey, conn.Priority, psdJSON, now, now); err != nil {
				res.fails = append(res.fails, fmt.Sprintf("connection %q: %s", conn.Name, err.Error()))
				continue
			}
		res.created++
		res.seeded = append(res.seeded, struct{ id string; priority int }{id: connID, priority: conn.Priority})
		}
		if err := tx.Commit(); err != nil {
			res.err = err
			return res
		}
		return res
	}

	for start := 0; start < len(req.Connections); start += batchSize {
		end := start + batchSize
		if end > len(req.Connections) {
			end = len(req.Connections)
		}
		batch := req.Connections[start:end]
		var br batchResult
		var batchErr error
		if h.writeQueue != nil {
			batchErr = h.writeQueue.Do(c.Request.Context(), "bulkAddConnections:batch", func(d *sql.DB) error {
				br = runBatch(d, batch)
				return br.err
			})
		} else {
			br = runBatch(h.db, batch)
		}
		if batchErr != nil {
			// Whole-batch failure: nothing in this batch was persisted.
			for _, conn := range batch {
				failed++
				errors = append(errors, fmt.Sprintf("connection %q: %s", conn.Name, batchErr.Error()))
			}
			continue
		}
	created += br.created
	failed += len(br.fails)
	errors = append(errors, br.fails...)
	seeded = append(seeded, br.seeded...)
	}

	// Seed in-memory store ONLY for committed rows, then recompute eligibility once.
	if h.store != nil {
		for _, s := range seeded {
			h.store.SeedConnection(s.id, providerID, "ready", s.priority)
		}
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"created": created, "total": len(req.Connections), "failed": failed, "errors": errors})
}

// ValidateKey checks if an API key is valid for a provider by attempting a lightweight model list request.
func (h *ProviderHandler) ValidateKey(c *gin.Context) {
	var req struct {
		Provider             string            `json:"provider" binding:"required"`
		APIKey               string            `json:"api_key" binding:"required"`
		ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
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
		testModel = "gpt-4o-mini"
	}

	resp, err := exec.ExecuteStream(ctx, &executor.Request{
		APIKey:               req.APIKey,
		BaseURL:              provider.BaseURL,
		Body:                 buildTestBody(provider.Format, testModel),
		Provider:             req.Provider,
		Model:                testModel,
		ProviderSpecificData: req.ProviderSpecificData,
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

// GetSettings returns the persistent JSON-file settings for a provider.
func (h *ProviderHandler) GetSettings(c *gin.Context) {
	id := c.Param("id")
	s, err := h.providerCfg.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider_id":  id,
		"routing_mode": s.RoutingMode,
	})
}

// UpdateSettings persists per-provider settings to a JSON file.
func (h *ProviderHandler) UpdateSettings(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		RoutingMode providercfg.RoutingMode `json:"routing_mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch req.RoutingMode {
	case providercfg.FirstEligible, providercfg.RoundRobin, providercfg.Random:
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "routing_mode must be one of: first_eligible, round_robin, random",
		})
		return
	}

	if err := h.providerCfg.Save(id, providercfg.ProviderSettings{RoutingMode: req.RoutingMode}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"provider_id":  id,
		"routing_mode": req.RoutingMode,
	})
}
