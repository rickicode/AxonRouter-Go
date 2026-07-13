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
	rows, err := h.db.Query(`SELECT id, COALESCE(name, ''), COALESCE(key_value, ''), rate_limit_per_min, max_tokens, is_active, created_at FROM api_keys ORDER BY created_at DESC`)
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
	}

	keys := make([]apiKeyView, 0)
	for rows.Next() {
		var k apiKeyView
		var isActive int
		var keyValue string
		var maxTokens int64
		if err := rows.Scan(&k.ID, &k.Name, &keyValue, &k.RateLimitPerMin, &maxTokens, &isActive, &k.CreatedAt); err != nil {
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
		Name            string `json:"name"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
		MaxTokens       int64  `json:"max_tokens"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Defaults are fine
	}
	if req.RateLimitPerMin <= 0 {
		req.RateLimitPerMin = 600
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

	now := time.Now().Unix()

	name := sql.NullString{}
	if req.Name != "" {
		name = sql.NullString{String: req.Name, Valid: true}
	}

	_, err = h.db.Exec(`
		INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?)
	`, id, string(hash), keyValue, name, req.RateLimitPerMin, req.MaxTokens, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         id,
		"key":        keyValue, // Only shown once
		"name":       req.Name,
		"max_tokens": req.MaxTokens,
		"message":    "Save this key — it won't be shown again",
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
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
