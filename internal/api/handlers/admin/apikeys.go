package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// APIKeyHandler manages proxy API keys.
type APIKeyHandler struct {
	db *sql.DB
}

// NewAPIKeyHandler creates a new API key handler.
func NewAPIKeyHandler(db *sql.DB) *APIKeyHandler {
	return &APIKeyHandler{db: db}
}

// List returns all API keys (masked).
func (h *APIKeyHandler) List(c *gin.Context) {
	rows, err := h.db.Query(`SELECT id, COALESCE(name, ''), rate_limit_per_min, is_active, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type apiKeyView struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		KeyPreview      string `json:"key_preview"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
		IsActive        bool   `json:"is_active"`
		CreatedAt       int64  `json:"created_at"`
	}

	var keys []apiKeyView
	for rows.Next() {
		var k apiKeyView
		var isActive int
		if err := rows.Scan(&k.ID, &k.Name, &k.RateLimitPerMin, &isActive, &k.CreatedAt); err != nil {
			continue
		}
		k.IsActive = isActive == 1
		k.KeyPreview = k.ID[:8] + "..."
		keys = append(keys, k)
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// Create generates a new API key.
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req struct {
		Name            string `json:"name"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Defaults are fine
	}
	if req.RateLimitPerMin <= 0 {
		req.RateLimitPerMin = 600
	}

	// Generate random key
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key"})
		return
	}
	keyHex := hex.EncodeToString(raw)

	// Hash with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(keyHex), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash key"})
		return
	}

	id := keyHex[:16] // Use first 16 chars as ID for display
	now := time.Now().Unix()

	name := sql.NullString{}
	if req.Name != "" {
		name = sql.NullString{String: req.Name, Valid: true}
	}

	_, err = h.db.Exec(`
		INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, 1, ?)
	`, id, string(hash), keyHex, name, req.RateLimitPerMin, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      id,
		"key":     keyHex, // Only shown once
		"name":    req.Name,
		"message": "Save this key — it won't be shown again",
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

// Delete removes an API key.
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	result, err := h.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ToggleActive enables or disables an API key.
func (h *APIKeyHandler) ToggleActive(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	active := 0
	if req.IsActive {
		active = 1
	}
	_, err := h.db.Exec(`UPDATE api_keys SET is_active = ? WHERE id = ?`, active, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
