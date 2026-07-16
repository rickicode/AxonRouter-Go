package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
)

type ProxyPoolHandler struct {
	db *sql.DB
	health *proxypool.HealthChecker
	resolver *proxypool.Resolver
	testProxy func(proxyURL, typ, auth string) proxypool.TestResult
	writeQueue *db.WriteQueue
}

func NewProxyPoolHandler(database *sql.DB, health *proxypool.HealthChecker, resolver *proxypool.Resolver, writeQueue *db.WriteQueue) *ProxyPoolHandler {
	return &ProxyPoolHandler{db: database, health: health, resolver: resolver, testProxy: proxypool.TestProxy, writeQueue: writeQueue}
}

func (h *ProxyPoolHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	where := "1=1"
	args := []any{}
	if v := c.Query("is_active"); v != "" {
		where += " AND is_active = ?"
		args = append(args, boolQuery(v))
	}
	if v := c.Query("test_status"); v != "" {
		where += " AND test_status = ?"
		args = append(args, v)
	}
	if v := c.Query("type"); v != "" {
		where += " AND type = ?"
		args = append(args, v)
	}
	if v := c.Query("q"); v != "" {
		term := likeEscape(v)
		where += " AND (name LIKE ? ESCAPE '\\' OR proxy_url LIKE ? ESCAPE '\\')"
		args = append(args, "%"+term+"%", "%"+term+"%")
	}

	var total int
	_ = h.db.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE "+where, args...).Scan(&total)
	rows, err := h.db.Query(`SELECT id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, last_tested_at, last_error, response_time_ms, proxy_ip, proxy_country, proxy_city, proxy_org, created_at, updated_at FROM proxy_pools WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		if p, ok := scanPool(rows); ok {
			items = append(items, poolJSON(p))
		}
	}
	pages := total / perPage
	if total%perPage > 0 {
		pages++
	}
	c.JSON(http.StatusOK, db.PaginatedResponse{Data: items, Pagination: db.Pagination{Page: page, PerPage: perPage, Total: total, TotalPages: pages}})
}

func (h *ProxyPoolHandler) Get(c *gin.Context) {
	p, ok := h.get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy pool not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": poolJSON(p)})
}

func (h *ProxyPoolHandler) Create(c *gin.Context) {
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	name := strings.TrimSpace(asString(req["name"]))
	proxyURL := strings.TrimSpace(asString(req["proxyUrl"]))
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(asString(req["proxy_url"]))
	}
	if name == "" || proxyURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and proxyUrl are required"})
		return
	}
	typ := proxypool.NormalizeType(asString(req["type"]), proxyURL)
	noProxy := asString(req["noProxy"])
	relayAuth := asString(req["relayAuth"])
	if proxypool.IsRelayType(typ) && relayAuth == "" {
		relayAuth = proxypool.GenerateRelayAuth()
	}
	// Check for duplicate proxy URL
	var existingCount int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?`, proxyURL).Scan(&existingCount); err == nil && existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "proxy URL already exists", "existing_count": existingCount})
		return
	}

	// Mandatory health check before insert.
	res := h.testProxy(proxyURL, typ, relayAuth)
	const defaultMaxResponseTimeMs = 8000
	if !proxypool.Healthy(res, defaultMaxResponseTimeMs) {
		reason := res.Error
		if reason == "" {
			reason = "proxy check failed"
		}
		if res.OK && res.ElapsedMs > defaultMaxResponseTimeMs {
			reason = "proxy too slow"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": reason, "elapsedMs": res.ElapsedMs})
		return
	}

	active := true
	if v, ok := req["isActive"]; ok {
		active = asBool(v)
	}
	now := time.Now().Unix()
	id := uuid.New().String()
	testedAt := time.Now().Format(time.RFC3339)
	_, err := h.db.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, last_tested_at, last_error, response_time_ms, proxy_ip, proxy_country, proxy_city, proxy_org, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, name, typ, proxyURL, noProxy, relayAuth, boolToInt(active), testedAt, nil, res.ElapsedMs, res.IP, res.Country, res.City, res.Org, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	p, _ := h.get(id)
	c.JSON(http.StatusCreated, gin.H{"data": poolJSON(p)})
}

// insertPoolRow creates a single proxy pool row. It skips duplicates when
// allowDuplicate is false and returns the new pool id, relay auth, or an error reason.
func (h *ProxyPoolHandler) insertPoolRow(tx *sql.Tx, name, proxyURL, typ, noProxy, relayAuth string, active bool, allowDuplicate bool) (string, string, string) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return "", "", "proxyUrl is required"
	}
	if u, err := url.Parse(proxyURL); err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
		return "", "", "invalid proxy URL"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "pool-" + time.Now().Format("060102150405")
	}
	typ = proxypool.NormalizeType(typ, proxyURL)
	if proxypool.IsRelayType(typ) && relayAuth == "" {
		relayAuth = proxypool.GenerateRelayAuth()
	}
	if !allowDuplicate {
		var existingCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?`, proxyURL).Scan(&existingCount); err == nil && existingCount > 0 {
			return "", "", "duplicate"
		}
	}
	now := time.Now().Unix()
	id := uuid.New().String()
	_, err := tx.Exec(
		`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'untested', ?, ?)`,
		id, name, typ, proxyURL, noProxy, relayAuth, boolToInt(active), now, now,
	)
	if err != nil {
		return "", "", "insert failed: " + err.Error()
	}
	return id, relayAuth, ""
}

func (h *ProxyPoolHandler) BulkCreate(c *gin.Context) {
	var req struct {
		Items             []any  `json:"items"`
		NamePrefix        string `json:"namePrefix"`
		DefaultType       string `json:"defaultType"`
		NoProxy           string `json:"noProxy"`
		IsActive          *bool  `json:"isActive"`
		RequireHealthy    bool   `json:"requireHealthy"`
		MaxResponseTimeMs int64  `json:"maxResponseTimeMs"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	// Hard limit to keep a single bulk request (and its per-item response
	// payload) bounded for very large imports.
	const maxBulkItems = 1000
	if len(req.Items) > maxBulkItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many items: maximum " + strconv.Itoa(maxBulkItems) + " per bulk import"})
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	// Health checks are mandatory for bulk imports. Default response-time ceiling
	// matches the single-add check.
	if req.MaxResponseTimeMs == 0 {
		req.MaxResponseTimeMs = 8000
	}
	// RequireHealthy is deprecated; it now behaves as always true.
	req.RequireHealthy = true

	created := 0
	skipped := 0
	errors := 0
	details := []gin.H{}
	// Tracks names used within this batch so auto-generated random names stay
	// unique for the proxies being imported together.
	usedNames := map[string]bool{}

	type normalizedItem struct {
		index     int
		name      string
		proxyURL  string
		typ       string
		noProxy   string
		relayAuth string
		dup       bool
		needsName bool
	}


	normalized := make([]normalizedItem, 0, len(req.Items))

	// Phase 1: parse and normalize all items sequentially. Duplicate checks and
	// name generation happen here so the subsequent test phase never wastes
	// network calls on entries that will be skipped anyway.
	for i, raw := range req.Items {
		var item struct {
			Name     string `json:"name"`
			ProxyURL string `json:"proxyUrl"`
			Type     string `json:"type"`
			NoProxy  string `json:"noProxy"`
		}
		switch v := raw.(type) {
		case string:
			line := strings.TrimSpace(v)
			if line == "" {
				continue
			}
			// Support "name|url" or just "url".
			if idx := strings.Index(line, "|"); idx > 0 {
				item.Name = strings.TrimSpace(line[:idx])
				item.ProxyURL = strings.TrimSpace(line[idx+1:])
			} else {
				item.ProxyURL = line
			}
		case map[string]any:
			item.Name = strings.TrimSpace(asString(v["name"]))
			item.ProxyURL = strings.TrimSpace(asString(v["proxyUrl"]))
			if item.ProxyURL == "" {
				item.ProxyURL = strings.TrimSpace(asString(v["proxy_url"]))
			}
			item.Type = asString(v["type"])
			item.NoProxy = asString(v["noProxy"])
			if item.NoProxy == "" {
				item.NoProxy = asString(v["no_proxy"])
			}
		default:
			errors++
			details = append(details, gin.H{"index": i, "status": "error", "reason": "unsupported item type"})
			continue
		}
	needsName := false
	if item.Name == "" {
		// Defer random geo name generation until after the health check so the
		// suffix can use country and ISP from the test result.
		needsName = true
	} else {
		// Reserve explicit names now so generated names never collide with them.
		usedNames[item.Name] = true
	}
	if item.Type == "" {
		item.Type = req.DefaultType
	}
	// If type is still http, try auto-detecting relay hosts (vercel/deno/cloudflare)
	// so bulk imports of relay URLs get the correct type by default.
	if item.Type == "" || item.Type == proxypool.TypeHTTP {
		if detected := proxypool.DetectRelayType(item.ProxyURL); detected != "" {
			item.Type = detected
		} else if item.Type == "" {
			item.Type = proxypool.TypeHTTP
		}
	}
	noProxy := item.NoProxy
	if noProxy == "" {
		noProxy = req.NoProxy
	}
	// Generate relay auth before testing so relay health checks and the
	// eventual insert use the exact same credentials.
	relayAuth := ""
	if proxypool.IsRelayType(item.Type) {
		relayAuth = proxypool.GenerateRelayAuth()
	}

	var existingCount int
	dup := h.db.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?", item.ProxyURL).Scan(&existingCount) == nil && existingCount > 0

	normalized = append(normalized, normalizedItem{
		index:     i,
		name:      item.Name,
		proxyURL:  item.ProxyURL,
		typ:       item.Type,
		noProxy:   noProxy,
		relayAuth: relayAuth,
		dup:       dup,
		needsName: needsName,
	})
}


	// Phase 2: test non-duplicate items concurrently with a bounded worker pool.
	testResults := make([]proxypool.TestResult, len(normalized))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, it := range normalized {
		if it.dup {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it normalizedItem) {
			defer wg.Done()
			defer func() { <-sem }()
			testResults[idx] = h.testProxy(it.proxyURL, it.typ, it.relayAuth)
		}(i, it)
	}
	wg.Wait()

	// Phase 2b: build final names for items that need auto-generated geo names.
	for i := range normalized {
		if normalized[i].dup {
			continue
		}
		if !normalized[i].needsName {
			continue
		}
		res := testResults[i]
		normalized[i].name = randomGeoProxyName(res.Country, res.Org, func(candidate string) bool {
			if usedNames[candidate] {
				return true
			}
			var cnt int
			if err := h.db.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE name = ?", candidate).Scan(&cnt); err == nil && cnt > 0 {
				return true
			}
			return false
		})
		usedNames[normalized[i].name] = true
	}

	// Phase 3: insert passing items and update their test metadata. Each write
	// goes through WriteQueue so bulk imports never contend on the SQLite
	// writer (the root cause of intermittent "database is locked" failures).
	for i, it := range normalized {
		if it.dup {
			skipped++
			details = append(details, gin.H{"index": it.index, "url": it.proxyURL, "status": "skipped", "reason": "duplicate"})
			continue
		}

	res := testResults[i]
	if !proxypool.Healthy(res, req.MaxResponseTimeMs) {
		reason := res.Error
		if reason == "" {
			reason = "proxy too slow"
		}
		skipped++
		details = append(details, gin.H{"index": it.index, "url": it.proxyURL, "status": "skipped", "reason": reason})
		continue
	}

		// Capture loop vars for the closure (Do blocks until the worker
		// finishes, so mutating captured counters here is race-free).
		idx := i
		item := it
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
			id, _, reason := h.insertPoolRow(tx, item.name, item.proxyURL, item.typ, item.noProxy, item.relayAuth, active, false)
			if reason != "" {
				// Per-item failure: record and let the batch continue.
				errors++
				details = append(details, gin.H{"index": item.index, "url": item.proxyURL, "status": "error", "reason": reason})
				return nil
			}
		created++
		details = append(details, gin.H{"index": item.index, "url": item.proxyURL, "id": id, "status": "created"})
		res := testResults[idx]
		status := "active"
		var lastErr any = nil
		if !res.OK {
			status = "error"
			lastErr = res.Error
		}
		testedAt := time.Now().Format(time.RFC3339)
		if _, e = tx.Exec("UPDATE proxy_pools SET test_status = ?, last_tested_at = ?, last_error = ?, response_time_ms = ?, proxy_ip = ?, proxy_country = ?, proxy_city = ?, proxy_org = ?, updated_at = ? WHERE id = ?", status, testedAt, lastErr, res.ElapsedMs, res.IP, res.Country, res.City, res.Org, time.Now().Unix(), id); e != nil {
			return e
		}
		return tx.Commit()
		}
		var doErr error
		if h.writeQueue == nil {
			doErr = run(h.db)
		} else {
			doErr = h.writeQueue.Do(c.Request.Context(), "bulk-create-proxy", run)
		}
		if doErr != nil {
			errors++
			details = append(details, gin.H{"index": item.index, "url": item.proxyURL, "status": "error", "reason": doErr.Error()})
			c.JSON(http.StatusInternalServerError, gin.H{"error": doErr.Error(), "created": created, "skipped": skipped, "errors": errors, "details": details})
			return
		}
	}

	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	c.JSON(http.StatusCreated, gin.H{
		"created": created,
		"skipped": skipped,
		"errors":  errors,
		"details": details,
	})
}

func (h *ProxyPoolHandler) BulkDelete(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids"`
		Status string   `json:"status"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	ids := map[string]struct{}{}
	for _, id := range req.IDs {
		ids[id] = struct{}{}
	}
	if req.Status != "" {
		rows, err := h.db.Query("SELECT id FROM proxy_pools WHERE test_status = ?", req.Status)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ids[id] = struct{}{}
				}
			}
		}
	}

	deleted := 0
	skipped := 0
	for id := range ids {
		if _, ok := h.get(id); !ok {
			skipped++
			continue
		}
		if err := h.deletePoolCascade(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": deleted, "skipped": skipped})
}

func (h *ProxyPoolHandler) Update(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy pool not found"})
		return
	}
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
	if v := strings.TrimSpace(asString(req["name"])); v != "" {
		add("name", v)
	}
	if _, ok := req["proxyUrl"]; ok {
		add("proxy_url", strings.TrimSpace(asString(req["proxyUrl"])))
	}
	if _, ok := req["noProxy"]; ok {
		add("no_proxy", asString(req["noProxy"]))
	}
	if _, ok := req["relayAuth"]; ok {
		add("relay_auth", asString(req["relayAuth"]))
	}
	if _, ok := req["isActive"]; ok {
		add("is_active", boolToInt(asBool(req["isActive"])))
	}
	if _, ok := req["testStatus"]; ok {
		add("test_status", asString(req["testStatus"]))
	}
	if _, ok := req["type"]; ok {
		add("type", proxypool.NormalizeType(asString(req["type"]), asString(req["proxyUrl"])))
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().Unix(), id)
	_, err := h.db.Exec("UPDATE proxy_pools SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	p, _ := h.get(id)
	c.JSON(http.StatusOK, gin.H{"data": poolJSON(p)})
}

func (h *ProxyPoolHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy pool not found"})
		return
	}
	// Cascade: soft-delete referencing connections, detach settings, then delete.
	if err := h.deletePoolCascade(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ProxyPoolHandler) Test(c *gin.Context) {
	res, err := proxypool.TestPool(h.db, c.Param("id"))
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy pool not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	c.JSON(http.StatusOK, res)
}

func (h *ProxyPoolHandler) HealthGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "lastHealthCheckAt": h.health.Last()})
}

func (h *ProxyPoolHandler) HealthRun(c *gin.Context) {
	results, skipped := h.health.RunNow()
	c.JSON(http.StatusOK, gin.H{"ok": true, "checkedAt": h.health.Last(), "results": results, "skipped": skipped})
}

func (h *ProxyPoolHandler) get(id string) (db.ProxyPool, bool) {
	row := h.db.QueryRow(`SELECT id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, last_tested_at, last_error, response_time_ms, proxy_ip, proxy_country, proxy_city, proxy_org, created_at, updated_at FROM proxy_pools WHERE id = ?`, id)
	p, ok := scanPool(row)
	return p, ok
}

type rowScanner interface{ Scan(dest ...any) error }

func scanPool(row rowScanner) (db.ProxyPool, bool) {
	var p db.ProxyPool
	var active int
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.ProxyURL, &p.NoProxy, &p.RelayAuth, &active, &p.TestStatus, &p.LastTestedAt, &p.LastError, &p.ResponseTimeMs, &p.ProxyIP, &p.ProxyCountry, &p.ProxyCity, &p.ProxyOrg, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, false
	}
	p.IsActive = active != 0
	return p, true
}

func poolJSON(p db.ProxyPool) gin.H {
	return gin.H{"id": p.ID, "name": p.Name, "type": p.Type, "proxyUrl": p.ProxyURL, "noProxy": p.NoProxy, "relayAuth": p.RelayAuth, "isActive": p.IsActive, "testStatus": p.TestStatus, "lastTestedAt": nullString(p.LastTestedAt), "lastError": nullString(p.LastError), "responseTimeMs": nullInt(p.ResponseTimeMs), "proxyIp": p.ProxyIP, "proxyCountry": p.ProxyCountry, "proxyCity": p.ProxyCity, "proxyOrg": p.ProxyOrg, "createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt}
}

func nullString(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}
func nullInt(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func asBool(v any) bool { b, ok := v.(bool); return ok && b }
func boolQuery(v string) int {
	if v == "true" || v == "1" {
		return 1
	}
	return 0
}

// randomGeoProxyName generates a pronounceable base name and appends a geo/ISP
// suffix. The final name is collision-checked against the current batch and DB.
func randomGeoProxyName(country, org string, exists func(string) bool) string {
	for attempt := 0; attempt < 50; attempt++ {
		base := pronounceableName(5 + rand.Intn(3)) // 5..7 characters
		name := geoProxyName(base, country, org)
		if !exists(name) {
			return name
		}
	}
	// Fallback: extremely unlikely collision storm.
	return geoProxyName(pronounceableName(5), country, org)
}

// geoProxyName builds a proxy name from the base and geo metadata.
// Precedence: base-isp-country, base-country, base-unknown.
func geoProxyName(base, country, org string) string {
	isp := sanitizeISP(org)
	cc := sanitizeISP(country)
	if cc == "" {
		cc = "unknown"
	}
	if isp != "" {
		return base + "-" + isp + "-" + cc
	}
	return base + "-" + cc
}

// sanitizeISP lowercases a raw ISP/org/country string and strips it down to
// alphanumeric segments separated by single dashes. Empty input returns empty.
func sanitizeISP(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := true
	written := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			written++
			if written >= 20 {
				break
			}
		} else if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := b.String()
	out = strings.TrimSuffix(out, "-")
	if out == "" {
		return ""
	}
	return out
}

// pronounceableName builds a consonant-vowel alternating string of the given
// length, which reads like a short made-up word (e.g. "bizot", "kavup", "milez").
func pronounceableName(length int) string {
	consonants := []rune("bcdfghklmnprstvz")
	vowels := []rune("aeiou")
	var b strings.Builder
	b.Grow(length)
	consonant := true
	for i := 0; i < length; i++ {
		if consonant {
			b.WriteRune(consonants[rand.Intn(len(consonants))])
		} else {
			b.WriteRune(vowels[rand.Intn(len(vowels))])
		}
		consonant = !consonant
	}
	return b.String()
}

// likeEscape escapes SQL LIKE wildcard characters so an ID containing '%' or
// '_' is matched literally rather than as a pattern. Used with ESCAPE '\'.
func likeEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// deletePoolCascade removes a proxy pool and cascades the deletion: any
// connection (any provider) that references the pool directly via proxyPoolId,
// or via a proxy group that contains the pool, is soft-deleted (is_active = 0).
// Group references in settings are detached, and groups left empty by the
// removal are deleted. All steps run in a single transaction so a failure
// cannot leave dangling references behind.
// deletePoolCascadeTx performs the cascade delete within an existing tx.
func (h *ProxyPoolHandler) deletePoolCascadeTx(tx *sql.Tx, poolID string) error {
	now := time.Now().Unix()
	connIDs := map[string]struct{}{}

	// 1a. Direct references: connections with proxyPoolId == poolID.
	rows, qerr := tx.Query("SELECT id, provider_specific_data FROM connections WHERE provider_specific_data LIKE ? ESCAPE '\\'", "%"+likeEscape(poolID)+"%")
	if qerr == nil {
		for rows.Next() {
			var id, raw string
			if rows.Scan(&id, &raw) != nil || raw == "" {
				continue
			}
			var psd map[string]any
			if json.Unmarshal([]byte(raw), &psd) != nil {
				continue
			}
			if psd["proxyPoolId"] == poolID {
				connIDs[id] = struct{}{}
			}
		}
		rows.Close()
	}

	// 1b. Group references: groups containing poolID, then connections using them.
	groupRows, gerr := tx.Query("SELECT id, proxy_pool_ids FROM proxy_groups WHERE proxy_pool_ids LIKE ? ESCAPE '\\'", "%"+likeEscape(poolID)+"%")
	groupIDs := []string{}
	if gerr == nil {
		for groupRows.Next() {
			var gid, rawIDs string
			if groupRows.Scan(&gid, &rawIDs) != nil || rawIDs == "" {
				continue
			}
			var ids []string
			if json.Unmarshal([]byte(rawIDs), &ids) != nil {
				continue
			}
			has := false
			for _, pid := range ids {
				if pid == poolID {
					has = true
					break
				}
			}
			if !has {
				continue
			}
			groupIDs = append(groupIDs, gid)
			crows, cerr := tx.Query("SELECT id, provider_specific_data FROM connections WHERE provider_specific_data LIKE ? ESCAPE '\\'", "%"+likeEscape(gid)+"%")
			if cerr == nil {
				for crows.Next() {
					var id, raw string
					if crows.Scan(&id, &raw) != nil || raw == "" {
						continue
					}
					var psd map[string]any
					if json.Unmarshal([]byte(raw), &psd) != nil {
						continue
					}
					if psd["proxyGroupId"] == gid {
						connIDs[id] = struct{}{}
					}
				}
				crows.Close()
			}
		}
		groupRows.Close()
	}

	// 2. Soft-delete the collected connections (skip the default direct oc connection).
	for id := range connIDs {
		var psd string
		if tx.QueryRow("SELECT COALESCE(provider_specific_data,'') FROM connections WHERE id = ?", id).Scan(&psd) == nil {
			if strings.Contains(psd, `"direct":"true"`) {
				continue
			}
		}
		if _, e := tx.Exec("UPDATE connections SET is_active = 0, updated_at = ? WHERE id = ?", now, id); e != nil {
			return e
		}
	}

	// 3. Detach pool from provider_proxy_defaults in settings.
	var raw string
	if tx.QueryRow("SELECT value FROM settings WHERE key = 'provider_proxy_defaults'").Scan(&raw) == nil && raw != "" {
		var defaults map[string]map[string]any
		if json.Unmarshal([]byte(raw), &defaults) == nil {
			changed := false
			for _, cfg := range defaults {
				if cfg["proxyPoolId"] == poolID {
					delete(cfg, "proxyPoolId")
					changed = true
				}
			}
			if changed {
				if out, merr := json.Marshal(defaults); merr == nil {
					if _, e := tx.Exec("UPDATE settings SET value = ?, updated_at = ? WHERE key = 'provider_proxy_defaults'", string(out), now); e != nil {
						return e
					}
				}
			}
		}
	}

	// 4. Remove pool from groups; delete groups left empty.
	for _, gid := range groupIDs {
		var rawIDs string
		if tx.QueryRow("SELECT proxy_pool_ids FROM proxy_groups WHERE id = ?", gid).Scan(&rawIDs) != nil {
			continue
		}
		var ids []string
		if json.Unmarshal([]byte(rawIDs), &ids) != nil {
			continue
		}
		newIDs := make([]string, 0, len(ids))
		for _, pid := range ids {
			if pid != poolID {
				newIDs = append(newIDs, pid)
			}
		}
		if len(newIDs) == 0 {
			if _, e := tx.Exec("DELETE FROM proxy_groups WHERE id = ?", gid); e != nil {
				return e
			}
		} else if out, merr := json.Marshal(newIDs); merr == nil {
			if _, e := tx.Exec("UPDATE proxy_groups SET proxy_pool_ids = ?, updated_at = ? WHERE id = ?", string(out), now, gid); e != nil {
				return e
			}
		}
	}

	// 5. Delete the pool itself.
	if _, e := tx.Exec("DELETE FROM proxy_pools WHERE id = ?", poolID); e != nil {
		return e
	}
	return nil
}

// deletePoolCascade removes a proxy pool and cascades the deletion. It routes
// the write through WriteQueue when available so bulk deletes never contend on
// the SQLite writer; when writeQueue is nil it falls back to a direct tx.
func (h *ProxyPoolHandler) deletePoolCascade(ctx context.Context, poolID string) error {
	run := func(d *sql.DB) error {
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()
		if err = h.deletePoolCascadeTx(tx, poolID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if h.writeQueue == nil {
		return run(h.db)
	}
	return h.writeQueue.Do(ctx, "delete-pool-cascade", run)
}
