package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// OAuthHandler handles OAuth flow initiation.
type OAuthHandler struct {
	db      *sql.DB
	authMgr *auth.Manager
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(db *sql.DB, authMgr *auth.Manager) *OAuthHandler {
	return &OAuthHandler{db: db, authMgr: authMgr}
}

// InitiateOAuth starts an OAuth flow for a connection's provider.
func (h *OAuthHandler) InitiateOAuth(c *gin.Context) {
	connID := c.Param("id")

	// Get connection to find provider type
	var providerTypeID string
	err := h.db.QueryRow(`SELECT provider_type_id FROM connections WHERE id = ?`, connID).Scan(&providerTypeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}

	providerType := auth.ProviderType(providerTypeID)
	svc, ok := h.authMgr.GetService(providerType)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OAuth not supported for provider: " + providerTypeID})
		return
	}

	ctx := c.Request.Context()
	state := generateOAuthState()
	port, resultChan, err := svc.StartLocalServer(ctx, state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start callback server"})
		return
	}

	stateWithPort := fmt.Sprintf("%s:%d", state, port)
	authURL, err := svc.GenerateAuthURL(ctx, stateWithPort)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate auth URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"auth_url":      authURL,
		"callback_port": port,
	})

	go func() {
		select {
		case creds := <-resultChan:
			if creds != nil {
				log.Printf("OAuth completed for connection %s", connID)
			}
		case <-time.After(5 * time.Minute):
			log.Printf("OAuth timeout for connection %s", connID)
		}
	}()
}

func generateOAuthState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
