package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
)

type ProxyPoolHandler struct {
	db       *sql.DB
	health   *proxypool.HealthChecker
	resolver *proxypool.Resolver
}

func NewProxyPoolHandler(database *sql.DB, health *proxypool.HealthChecker, resolver *proxypool.Resolver) *ProxyPoolHandler {
	return &ProxyPoolHandler{db: database, health: health, resolver: resolver}
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

	var total int
	_ = h.db.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE "+where, args...).Scan(&total)
	rows, err := h.db.Query(`SELECT id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, last_tested_at, last_error, response_time_ms, created_at, updated_at FROM proxy_pools WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, perPage, (page-1)*perPage)...)
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
	active := true
	if v, ok := req["isActive"]; ok {
		active = asBool(v)
	}
	now := time.Now().Unix()
	id := uuid.New().String()
	_, err := h.db.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'untested', ?, ?)`, id, name, typ, proxyURL, noProxy, relayAuth, boolToInt(active), now, now)
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
// allowDuplicate is false and returns the new pool id or an error reason.
func (h *ProxyPoolHandler) insertPoolRow(name, proxyURL, typ, noProxy, relayAuth string, active bool, allowDuplicate bool) (string, string) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return "", "proxyUrl is required"
	}
	if u, err := url.Parse(proxyURL); err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
		return "", "invalid proxy URL"
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
		if err := h.db.QueryRow(`SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?`, proxyURL).Scan(&existingCount); err == nil && existingCount > 0 {
			return "", "duplicate"
		}
	}
	now := time.Now().Unix()
	id := uuid.New().String()
	_, err := h.db.Exec(
		`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'untested', ?, ?)`,
		id, name, typ, proxyURL, noProxy, relayAuth, boolToInt(active), now, now,
	)
	if err != nil {
		return "", "insert failed: " + err.Error()
	}
	return id, ""
}

func (h *ProxyPoolHandler) BulkCreate(c *gin.Context) {
	var req struct {
		Items       []any  `json:"items"`
		NamePrefix  string `json:"namePrefix"`
		DefaultType string `json:"defaultType"`
		NoProxy     string `json:"noProxy"`
		IsActive    *bool  `json:"isActive"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	prefix := strings.TrimSpace(req.NamePrefix)
	if prefix == "" {
		prefix = "bulk"
	}

	created := 0
	skipped := 0
	errors := 0
	details := []gin.H{}

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

		if item.Name == "" {
			item.Name = fmt.Sprintf("%s-%d", prefix, i+1)
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

		id, reason := h.insertPoolRow(item.Name, item.ProxyURL, item.Type, noProxy, "", active, false)
		if reason == "duplicate" {
			skipped++
			details = append(details, gin.H{"index": i, "url": item.ProxyURL, "status": "skipped", "reason": reason})
		} else if reason != "" {
			errors++
			details = append(details, gin.H{"index": i, "url": item.ProxyURL, "status": "error", "reason": reason})
		} else {
			created++
			details = append(details, gin.H{"index": i, "url": item.ProxyURL, "id": id, "status": "created"})
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
	// Cascade: clean references before delete
	h.cleanPoolReferences(id)
	if _, err := h.db.Exec("DELETE FROM proxy_pools WHERE id = ?", id); err != nil {
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
	row := h.db.QueryRow(`SELECT id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, last_tested_at, last_error, response_time_ms, created_at, updated_at FROM proxy_pools WHERE id = ?`, id)
	p, ok := scanPool(row)
	return p, ok
}

type rowScanner interface{ Scan(dest ...any) error }

func scanPool(row rowScanner) (db.ProxyPool, bool) {
	var p db.ProxyPool
	var active int
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.ProxyURL, &p.NoProxy, &p.RelayAuth, &active, &p.TestStatus, &p.LastTestedAt, &p.LastError, &p.ResponseTimeMs, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, false
	}
	p.IsActive = active != 0
	return p, true
}

func poolJSON(p db.ProxyPool) gin.H {
	return gin.H{"id": p.ID, "name": p.Name, "type": p.Type, "proxyUrl": p.ProxyURL, "noProxy": p.NoProxy, "relayAuth": p.RelayAuth, "isActive": p.IsActive, "testStatus": p.TestStatus, "lastTestedAt": nullString(p.LastTestedAt), "lastError": nullString(p.LastError), "responseTimeMs": nullInt(p.ResponseTimeMs), "createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt}
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

// cleanPoolReferences removes all references to a proxy pool before deletion.
func (h *ProxyPoolHandler) cleanPoolReferences(poolID string) {
	// 1. Clean connections' provider_specific_data
	rows, err := h.db.Query("SELECT id, provider_specific_data FROM connections WHERE provider_specific_data LIKE ?", "%"+poolID+"%")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, raw string
			if rows.Scan(&id, &raw) != nil || raw == "" {
				continue
			}
			var psd map[string]any
			if json.Unmarshal([]byte(raw), &psd) != nil {
				continue
			}
			changed := false
			if psd["proxyPoolId"] == poolID {
				delete(psd, "proxyPoolId")
				changed = true
			}
			if changed {
				if out, err := json.Marshal(psd); err == nil {
					h.db.Exec("UPDATE connections SET provider_specific_data = ?, updated_at = ? WHERE id = ?", string(out), time.Now().Unix(), id)
				}
			}
		}
	}

	// 2. Clean provider_proxy_defaults in settings
	var raw string
	if h.db.QueryRow("SELECT value FROM settings WHERE key = 'provider_proxy_defaults'").Scan(&raw) == nil && raw != "" {
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
				if out, err := json.Marshal(defaults); err == nil {
					h.db.Exec("UPDATE settings SET value = ?, updated_at = ? WHERE key = 'provider_proxy_defaults'", string(out), time.Now().Unix())
				}
			}
		}
	}

	// 3. Remove pool ID from proxy groups' proxy_pool_ids
	grows, err := h.db.Query("SELECT id, proxy_pool_ids FROM proxy_groups WHERE proxy_pool_ids LIKE ?", "%"+poolID+"%")
	if err == nil {
		defer grows.Close()
		for grows.Next() {
			var id, rawIDs string
			if grows.Scan(&id, &rawIDs) != nil || rawIDs == "" {
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
			if len(newIDs) != len(ids) {
				if out, err := json.Marshal(newIDs); err == nil {
					h.db.Exec("UPDATE proxy_groups SET proxy_pool_ids = ?, updated_at = ? WHERE id = ?", string(out), time.Now().Unix(), id)
				}
			}
		}
	}
}
