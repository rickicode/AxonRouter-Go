package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

type OAuthHandler struct {
	db      *sql.DB
	authMgr *auth.Manager
	store   *connstate.Store
	elig    *connstate.EligibilityManager
}

func NewOAuthHandler(db *sql.DB, authMgr *auth.Manager, store *connstate.Store, elig *connstate.EligibilityManager) *OAuthHandler {
	return &OAuthHandler{db: db, authMgr: authMgr, store: store, elig: elig}
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

	ctx, cancel := context.WithCancel(context.Background())
	state := generateOAuthState()
	port, resultChan, err := svc.StartLocalServer(ctx, state)
	if err != nil {
		cancel()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start callback server"})
		return
	}

	stateWithPort := fmt.Sprintf("%s:%d", state, port)
	authURL, err := svc.GenerateAuthURL(ctx, stateWithPort)
	if err != nil {
		cancel()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate auth URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"auth_url":      authURL,
		"callback_port": port,
	})

	go func() {
		defer cancel() // shuts down the local callback server
		select {
		case creds := <-resultChan:
			if creds == nil {
				log.Printf("OAuth nil credentials for connection %s", connID)
				return
			}
			now := time.Now().Unix()
			providerSpecific := sql.NullString{}
			if len(creds.ProviderSpecific) > 0 {
				if b, err := json.Marshal(creds.ProviderSpecific); err == nil {
					providerSpecific = sql.NullString{String: string(b), Valid: true}
				}
			}
			_, err := h.db.Exec(`
				UPDATE connections SET
					oauth_token = ?, oauth_refresh_token = ?, oauth_expires_at = ?,
					provider_specific_data = ?, status = 'ready', updated_at = ?
				WHERE id = ?
			`, creds.AccessToken, creds.RefreshToken, creds.ExpiresAt.Unix(), providerSpecific, now, connID)
			if err != nil {
				log.Printf("OAuth save tokens failed for connection %s: %v", connID, err)
			} else {
				log.Printf("OAuth tokens saved for connection %s", connID)
				// Sync in-memory state so routing picks up the new connection
				if h.store != nil {
					h.store.UpdateStatus(connID, connstate.StatusReady)
					if h.elig != nil {
						h.elig.Update(h.store)
					}
				}
			}
		case <-time.After(5 * time.Minute):
			log.Printf("OAuth timeout for connection %s", connID)
		}
	}()
}

// OAuthStatus checks if an OAuth connection has received its tokens.
func (h *OAuthHandler) OAuthStatus(c *gin.Context) {
	connID := c.Param("id")
	var oauthToken sql.NullString
	err := h.db.QueryRow(`SELECT oauth_token FROM connections WHERE id = ?`, connID).Scan(&oauthToken)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	connected := oauthToken.Valid && oauthToken.String != ""
	c.JSON(http.StatusOK, gin.H{"connected": connected})
}

func generateOAuthState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
