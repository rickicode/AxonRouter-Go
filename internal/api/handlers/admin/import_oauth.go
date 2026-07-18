package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// ImportToken creates a ready OAuth connection from manually supplied tokens.
// Only built-in OAuth providers with a registered auth.OAuthService are accepted.
func (h *OAuthHandler) ImportToken(c *gin.Context) {
	var req struct {
		Provider             string            `json:"provider" binding:"required"`
		AccessToken          string            `json:"access_token" binding:"required"`
		RefreshToken         string            `json:"refresh_token" binding:"required"`
		ExpiresAt            int64             `json:"expires_at"`
		Email                string            `json:"email"`
		ProviderSpecificData map[string]string `json:"provider_specific_data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify the provider exists.
	var exists bool
	if err := h.db.QueryRow(`SELECT COUNT(*) > 0 FROM provider_types WHERE id = ?`, req.Provider).Scan(&exists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Only allow import for built-in OAuth providers registered with the auth manager.
	providerType := auth.ProviderType(req.Provider)
	if _, ok := h.authMgr.GetService(providerType); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OAuth import not supported for provider: " + req.Provider})
		return
	}

	connName := "OAuth " + req.Provider
	if req.Email != "" {
		connName = req.Email
	}

	connID := uuid.New().String()
	now := time.Now().Unix()

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
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, is_active, created_at, updated_at)
		VALUES (?, ?, ?, 'oauth', ?, ?, ?, ?, 'ready', 1, ?, ?)
	`, connID, req.Provider, connName, req.AccessToken, req.RefreshToken, req.ExpiresAt, psdJSON, now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sync in-memory state so the connection is immediately eligible for routing.
	if h.store != nil {
		h.store.UpdateStatus(connID, connstate.StatusReady)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     connID,
		"name":   connName,
		"status": "ready",
	})
}
