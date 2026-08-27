package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/db"
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

	connID, _, err := db.UpsertOAuthConnection(context.Background(), h.db, req.Provider, req.Email, connName, req.AccessToken, req.RefreshToken, req.ExpiresAt, psdJSON, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sync in-memory state so the connection is immediately eligible for routing.
	if h.store != nil {
		h.store.SeedConnection(connID, req.Provider, "ready", 0)
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

// BulkImportFreebuff imports multiple Freebuff accounts at once from a JSON array.
// Each entry requires access_token and refresh_token; email, expires_at, and
// device_id are optional. Deduplicates by email (oauth_email) — existing accounts
// are updated with new tokens.
func (h *OAuthHandler) BulkImportFreebuff(c *gin.Context) {
	var req struct {
		Accounts []struct {
			AccessToken  string            `json:"access_token" binding:"required"`
			RefreshToken string            `json:"refresh_token" binding:"required"`
			ExpiresAt    int64             `json:"expires_at"`
			Email        string            `json:"email"`
			DeviceID     string            `json:"device_id"`
			Name         string            `json:"name"`
		} `json:"accounts" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	const maxBulk = 5000
	if len(req.Accounts) > maxBulk {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "too many accounts: maximum 5000 per import"})
		return
	}

	providerID := "freebuff"

	// Verify the provider exists.
	var exists bool
	if err := h.db.QueryRow(`SELECT COUNT(*) > 0 FROM provider_types WHERE id = ?`, providerID).Scan(&exists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "freebuff provider not found"})
		return
	}

	now := time.Now().Unix()

	type importResult struct {
		Index  int    `json:"index"`
		Status string `json:"status"` // "created", "updated", "error"
		ID     string `json:"id,omitempty"`
		Error  string `json:"error,omitempty"`
	}

	results := make([]importResult, 0, len(req.Accounts))
	created := 0
	updated := 0
	failed := 0

	for i, acc := range req.Accounts {
		result := importResult{Index: i}

		// Build connection name.
		connName := acc.Name
		if connName == "" {
			connName = acc.Email
		}
		if connName == "" {
			connName = "Freebuff " + acc.AccessToken[:min(8, len(acc.AccessToken))] + "..."
		}

		// Build provider_specific_data with device_id if provided.
		var psdJSON sql.NullString
		psd := map[string]string{}
		if acc.DeviceID != "" {
			psd["deviceId"] = acc.DeviceID
		}
		if len(psd) > 0 {
			b, _ := json.Marshal(psd)
			psdJSON = sql.NullString{String: string(b), Valid: true}
		}

		connID, isNew, err := db.UpsertOAuthConnection(
			context.Background(), h.db, providerID, acc.Email, connName,
			acc.AccessToken, acc.RefreshToken, acc.ExpiresAt, psdJSON, now,
		)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			failed++
			results = append(results, result)
			continue
		}

		result.ID = connID
		if isNew {
			result.Status = "created"
			created++
		} else {
			result.Status = "updated"
			updated++
		}

		// Sync in-memory state so the connection is immediately eligible.
		if h.store != nil {
			h.store.SeedConnection(connID, providerID, "ready", 0)
		}

		results = append(results, result)
	}

	// Update eligibility snapshot once after all connections are seeded.
	if h.elig != nil {
		h.elig.Update(h.store)
	}

	c.JSON(http.StatusOK, gin.H{
		"total":   len(req.Accounts),
		"created": created,
		"updated": updated,
		"failed":  failed,
		"results": results,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
