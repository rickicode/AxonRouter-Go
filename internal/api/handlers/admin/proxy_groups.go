package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
)

type ProxyGroupHandler struct {
	db       *sql.DB
	resolver *proxypool.Resolver
}

func NewProxyGroupHandler(database *sql.DB, resolver *proxypool.Resolver) *ProxyGroupHandler {
	return &ProxyGroupHandler{db: database, resolver: resolver}
}

func (h *ProxyGroupHandler) List(c *gin.Context) {
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
	if v := c.Query("mode"); v != "" {
		where += " AND mode = ?"
		args = append(args, v)
	}
	var total int
	_ = h.db.QueryRow("SELECT COUNT(*) FROM proxy_groups WHERE "+where, args...).Scan(&total)
	rows, err := h.db.Query(`SELECT id, name, mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active, created_at, updated_at FROM proxy_groups WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []db.ProxyGroup{}
	for rows.Next() {
		if g, ok := scanGroup(rows); ok {
			items = append(items, g)
		}
	}
	pages := total / perPage
	if total%perPage > 0 {
		pages++
	}
	c.JSON(http.StatusOK, db.PaginatedResponse{Data: items, Pagination: db.Pagination{Page: page, PerPage: perPage, Total: total, TotalPages: pages}})
}

func (h *ProxyGroupHandler) Get(c *gin.Context) {
	g, ok := h.get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy group not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *ProxyGroupHandler) Create(c *gin.Context) {
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	name := strings.TrimSpace(asString(req["name"]))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	mode := asString(req["mode"])
	if mode == "" {
		mode = "roundrobin"
	}
	if mode != "roundrobin" && mode != "sticky" && mode != "random" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be roundrobin, sticky or random"})
		return
	}
	ids, err := h.poolIDs(req["proxyPoolIds"])
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stickyLimit := asInt(req["stickyLimit"], 1)
	if stickyLimit < 1 {
		stickyLimit = 1
	}
	strict := asBool(req["strictProxy"])
	active := true
	if _, ok := req["isActive"]; ok {
		active = asBool(req["isActive"])
	}
	rawIDs, _ := json.Marshal(ids)
	now := time.Now().Unix()
	id := uuid.New().String()
	_, err = h.db.Exec(`INSERT INTO proxy_groups (id, name, mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, name, mode, stickyLimit, boolToInt(strict), string(rawIDs), boolToInt(active), now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	g, _ := h.get(id)
	c.JSON(http.StatusCreated, gin.H{"data": g})
}

func (h *ProxyGroupHandler) Update(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy group not found"})
		return
	}
	var req map[string]any
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(asString(req["name"])); v != "" {
		add("name", v)
	}
	if _, ok := req["mode"]; ok {
		mode := asString(req["mode"])
		if mode != "roundrobin" && mode != "sticky" && mode != "random" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be roundrobin, sticky or random"})
			return
		}
		add("mode", mode)
	}
	if _, ok := req["stickyLimit"]; ok {
		n := asInt(req["stickyLimit"], 1)
		if n < 1 {
			n = 1
		}
		add("sticky_limit", n)
	}
	if _, ok := req["strictProxy"]; ok {
		add("strict_proxy", boolToInt(asBool(req["strictProxy"])))
	}
	if _, ok := req["isActive"]; ok {
		add("is_active", boolToInt(asBool(req["isActive"])))
	}
	if _, ok := req["proxyPoolIds"]; ok {
		ids, err := h.poolIDs(req["proxyPoolIds"])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		raw, _ := json.Marshal(ids)
		add("proxy_pool_ids", string(raw))
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().Unix(), id)
	_, err := h.db.Exec("UPDATE proxy_groups SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	g, _ := h.get(id)
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *ProxyGroupHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "proxy group not found"})
		return
	}
	// Cascade: clean references before delete
	h.cleanGroupReferences(id)
	if _, err := h.db.Exec("DELETE FROM proxy_groups WHERE id = ?", id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.resolver != nil {
		h.resolver.Invalidate()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ProxyGroupHandler) get(id string) (db.ProxyGroup, bool) {
	row := h.db.QueryRow(`SELECT id, name, mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active, created_at, updated_at FROM proxy_groups WHERE id = ?`, id)
	return scanGroup(row)
}

func (h *ProxyGroupHandler) poolIDs(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return []string{}, nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(asString(item))
		if id == "" {
			continue
		}
		var exists int
		if err := h.db.QueryRow("SELECT 1 FROM proxy_pools WHERE id = ?", id).Scan(&exists); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func scanGroup(row rowScanner) (db.ProxyGroup, bool) {
	var g db.ProxyGroup
	var strict, active int
	var ids string
	if err := row.Scan(&g.ID, &g.Name, &g.Mode, &g.StickyLimit, &strict, &ids, &active, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return g, false
	}
	g.StrictProxy = strict != 0
	g.IsActive = active != 0
	_ = json.Unmarshal([]byte(ids), &g.ProxyPoolIDs)
	if g.ProxyPoolIDs == nil {
		g.ProxyPoolIDs = []string{}
	}
	return g, true
}

func asInt(v any, fallback int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return fallback
	}
}

// cleanGroupReferences removes all references to a proxy group before deletion.
func (h *ProxyGroupHandler) cleanGroupReferences(groupID string) {
	// 1. Clean connections' provider_specific_data
	rows, err := h.db.Query("SELECT id, provider_specific_data FROM connections WHERE provider_specific_data LIKE ?", "%"+groupID+"%")
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
			if psd["proxyGroupId"] == groupID {
				delete(psd, "proxyGroupId")
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
				if cfg["proxyGroupId"] == groupID {
					delete(cfg, "proxyGroupId")
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
}
