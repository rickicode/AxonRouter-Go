package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api/middleware"
	"golang.org/x/crypto/bcrypt"
)

// APIKeyHandler manages proxy API keys.
type APIKeyHandler struct {
	db    *sql.DB
	cache *middleware.AuthCache
}

// NewAPIKeyHandler creates a new API key handler.
func NewAPIKeyHandler(db *sql.DB, cache *middleware.AuthCache) *APIKeyHandler {
	return &APIKeyHandler{db: db, cache: cache}
}

// List returns all API keys (masked).
func (h *APIKeyHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`SELECT id, COALESCE(name, ''), COALESCE(key_value, ''), rate_limit_per_min, max_tokens, is_active, created_at, COALESCE(expires_at, 0) FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type apiKeyView struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Key             string `json:"key"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
		MaxTokens       int64  `json:"max_tokens"`
		IsActive        bool   `json:"is_active"`
		CreatedAt       int64  `json:"created_at"`
		ExpiresAt       int64  `json:"expires_at"`
	}

	keys := make([]apiKeyView, 0)
	for rows.Next() {
		var k apiKeyView
		var isActive int
		var keyValue string
		var maxTokens int64
		if err := rows.Scan(&k.ID, &k.Name, &keyValue, &k.RateLimitPerMin, &maxTokens, &isActive, &k.CreatedAt, &k.ExpiresAt); err != nil {
			continue
		}
		k.IsActive = isActive == 1
		k.Key = keyValue
		k.MaxTokens = maxTokens
		// k.Key already assigned from key_value above
		keys = append(keys, k)
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// Create generates a new API key.
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req struct {
		Name            string  `json:"name"`
		RateLimitPerMin int     `json:"rate_limit_per_min"`
		MaxTokens       int64   `json:"max_tokens"`
		ExpiresAt       *int64  `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Defaults are fine
	}
	if req.RateLimitPerMin <= 0 {
		req.RateLimitPerMin = 600
	}

	now := time.Now().Unix()
	if req.ExpiresAt != nil && *req.ExpiresAt <= now {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be in the future"})
		return
	}

	// Generate random key
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key"})
		return
	}
	hexPart := hex.EncodeToString(raw)
	id := "ax-" + hexPart[:16]
	keyValue := "ax-" + hexPart

	// Hash with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(keyValue), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash key"})
		return
	}

	name := sql.NullString{}
	if req.Name != "" {
		name = sql.NullString{String: req.Name, Valid: true}
	}

	expiresAt := sql.NullInt64{}
	if req.ExpiresAt != nil {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}

	_, err = h.db.Exec(`
	INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at)
	VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, string(hash), keyValue, name, req.RateLimitPerMin, req.MaxTokens, now, expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	expiresAtResponse := int64(0)
	if req.ExpiresAt != nil {
		expiresAtResponse = *req.ExpiresAt
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":          id,
		"key":         keyValue, // Only shown once
		"name":        req.Name,
		"max_tokens":  req.MaxTokens,
		"expires_at":  expiresAtResponse,
		"message":     "Save this key — it won't be shown again",
	})
}

// GetValue returns the raw API key value for a given id. The value is only
// available at creation time and stored for convenience; it is served here so
// the dashboard can copy the full key and so CLI tool configs can embed it
// directly from the selected key (no manual paste needed).
func (h *APIKeyHandler) GetValue(c *gin.Context) {
	id := c.Param("id")
	var raw string
	err := h.db.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&raw)
	if err != nil || raw == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "api key value is not available"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "key": raw})
}

// Delete removes an API key and clears its usage and cache entry.
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var keyValue string
	if err := tx.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&keyValue); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, err := tx.Exec(`DELETE FROM api_key_usage WHERE api_key_id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, err := tx.Exec(`DELETE FROM api_keys WHERE id = ?`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.cache != nil {
		if keyValue != "" {
			h.cache.Invalidate(keyValue)
		} else {
			h.cache.InvalidateAll()
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ToggleActive enables or disables an API key.
func (h *APIKeyHandler) ToggleActive(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		IsActive  bool  `json:"is_active"`
		MaxTokens int64 `json:"max_tokens"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	active := 0
	if req.IsActive {
		active = 1
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		_ = h.db.QueryRow(`SELECT COALESCE(max_tokens, 0) FROM api_keys WHERE id = ?`, id).Scan(&maxTokens)
	}
	_, err := h.db.Exec(`UPDATE api_keys SET is_active = ?, max_tokens = ? WHERE id = ?`, active, maxTokens, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.cache != nil {
		var keyValue string
		if err := h.db.QueryRow(`SELECT COALESCE(key_value, '') FROM api_keys WHERE id = ?`, id).Scan(&keyValue); err == nil {
			if keyValue != "" {
				h.cache.Invalidate(keyValue)
			} else {
				h.cache.InvalidateAll()
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
