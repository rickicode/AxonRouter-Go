package admin

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

// OptimizationHandler handles compression and cache admin endpoints.
type OptimizationHandler struct {
	db    *sql.DB
	cache cache.CacheStorage
}

// NewOptimizationHandler creates a new optimization handler.
func NewOptimizationHandler(database *sql.DB, c cache.CacheStorage) *OptimizationHandler {
	return &OptimizationHandler{db: database, cache: c}
}

func (h *OptimizationHandler) getSetting(key, def string) string {
	var value string
	err := h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil || value == "" {
		return def
	}
	return value
}

func (h *OptimizationHandler) setSetting(key, value string) error {
	now := time.Now().Unix()
	_, err := h.db.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now, value, now)
	return err
}

// GetCompressionSettings returns current compression configuration.
func (h *OptimizationHandler) GetCompressionSettings(c *gin.Context) {
	mode := h.getSetting("compression_mode", "lite")
	c.JSON(http.StatusOK, h.compressionSettingsMap(mode))
}

// UpdateCompressionSettings persists compression configuration.
func (h *OptimizationHandler) UpdateCompressionSettings(c *gin.Context) {
	var req struct {
		Mode string `json:"mode"`
		Lite struct {
			CollapseWhitespace     bool `json:"collapse_whitespace"`
			ReplaceImageUrls       bool `json:"replace_image_urls"`
			RemoveRedundantContent bool `json:"remove_redundant_content"`
			DedupSystemPrompt      bool `json:"dedup_system_prompt"`
		} `json:"lite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "lite"
	}

	_ = h.setSetting("compression_mode", mode)
	_ = h.setSetting("compression_lite_collapse", boolStr(req.Lite.CollapseWhitespace))
	_ = h.setSetting("compression_lite_image_urls", boolStr(req.Lite.ReplaceImageUrls))
	_ = h.setSetting("compression_lite_dedup", boolStr(req.Lite.DedupSystemPrompt))
	_ = h.setSetting("compression_lite_redundant", boolStr(req.Lite.RemoveRedundantContent))

	c.JSON(http.StatusOK, h.compressionSettingsMap(mode))
}

// GetCacheStats returns current cache statistics.
func (h *OptimizationHandler) GetCacheStats(c *gin.Context) {
	stats := h.cache.Stats()
	c.JSON(http.StatusOK, gin.H{
		"hits":     stats.Hits,
		"misses":   stats.Misses,
		"size":     stats.Size,
		"hit_rate": stats.HitRate(),
	})
}

// FlushCache clears all cached responses.
func (h *OptimizationHandler) FlushCache(c *gin.Context) {
	h.cache.Flush()
	c.JSON(http.StatusOK, gin.H{"flushed": true})
}

// PreviewCompression runs compression on a sample body and returns stats.
func (h *OptimizationHandler) PreviewCompression(c *gin.Context) {
	var req struct {
		Body string `json:"body" binding:"required"`
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mode := compression.CompressionMode(req.Mode)
	if mode == "" {
		mode = compression.ModeStandard
	}

	cfg := compression.Strategy{
		Mode: mode,
		Lite: h.liteConfigFromDB(),
	}

	compressed, stats, _ := compression.Apply(cfg, []byte(req.Body))

	c.JSON(http.StatusOK, gin.H{
		"compressed":        string(compressed),
		"original_tokens":   stats.OriginalTokens,
		"compressed_tokens": stats.CompressedTokens,
		"savings_percent":   stats.SavingsPercent,
		"techniques_used":   stats.TechniquesUsed,
	})
}

func (h *OptimizationHandler) compressionSettingsMap(mode string) gin.H {
	return gin.H{
		"mode": mode,
		"lite": gin.H{
			"collapse_whitespace":        parseBool(h.getSetting("compression_lite_collapse", "true")),
			"replace_image_urls":           parseBool(h.getSetting("compression_lite_image_urls", "true")),
			"remove_redundant_content":     parseBool(h.getSetting("compression_lite_redundant", "false")),
			"dedup_system_prompt":          parseBool(h.getSetting("compression_lite_dedup", "false")),
		},
	}
}

func (h *OptimizationHandler) liteConfigFromDB() compression.LiteConfig {
	return compression.LiteConfig{
		CollapseWhitespace:     parseBool(h.getSetting("compression_lite_collapse", "true")),
		ReplaceImageUrls:       parseBool(h.getSetting("compression_lite_image_urls", "true")),
		RemoveRedundantContent: parseBool(h.getSetting("compression_lite_redundant", "false")),
		DedupSystemPrompt:      parseBool(h.getSetting("compression_lite_dedup", "false")),
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func parseBool(s string) bool {
	return s == "true"
}
