package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// SettingHandler handles settings management.
type SettingHandler struct {
	db *sql.DB
}

// NewSettingHandler creates a new setting handler.
func NewSettingHandler(database *sql.DB) *SettingHandler {
	return &SettingHandler{db: database}
}

// List returns all settings.
func (h *SettingHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var s db.Setting
		rows.Scan(&s.Key, &s.Value, &s.UpdatedAt)
		settings[s.Key] = s.Value
	}
	c.JSON(http.StatusOK, gin.H{"data": settings})
}

// Get returns a single setting.
func (h *SettingHandler) Get(c *gin.Context) {
	key := c.Param("key")
	var value string
	err := h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "setting not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
}

// Set creates or updates a setting.
func (h *SettingHandler) Set(c *gin.Context) {
	key := c.Param("key")
	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Block provider_proxy_defaults containing oc key
	if key == "provider_proxy_defaults" {
		var defaults map[string]map[string]any
		if json.Unmarshal([]byte(req.Value), &defaults) == nil {
			if _, ok := defaults["oc"]; ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "OpenCode Free (oc) cannot be assigned a provider default proxy. Assign a proxy pool per connection instead."})
				return
			}
		}
	}

	now := time.Now().Unix()
	_, err := h.db.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, req.Value, now, req.Value, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a setting.
func (h *SettingHandler) Delete(c *gin.Context) {
	key := c.Param("key")
	h.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DefaultSettings returns the default settings with their values.
var DefaultSettings = map[string]string{
	"quota_check_interval_min":    "30",
	"port":                        "3777",
	"rate_limit_per_min":          "600",
	"log_retention_days":          "30",
	"max_concurrent_requests":     "100",
	"request_timeout_sec":         "120",
	"circuit_breaker_threshold":   "3",
	"circuit_breaker_timeout_sec": "60",
	"failover_max_attempts":       "5",
	"log_format":                  "compact",
	"compression_mode":            "lite",
	"compression_lite_collapse":   "true",
	"compression_lite_image_urls": "true",
	"compression_lite_redundant":  "false",
	"compression_lite_dedup":      "false",
	"combo_strategy":              "priority",
	"combo_strategies":            "{}",
}

// SeedDefaults inserts default settings if they don't exist.
func (h *SettingHandler) SeedDefaults() {
	now := time.Now().Unix()
	for key, value := range DefaultSettings {
		h.db.Exec(`
			INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		`, key, value, now)
	}
}
