package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
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
			// Update name with email if available (like OmniRoute auto-naming)
			if creds.Email != "" {
				h.db.Exec(`UPDATE connections SET name = ? WHERE id = ? AND name LIKE 'OAuth %'`, creds.Email, connID)
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
			log.Printf("OAuth timeout for connection %s, removing connection", connID)
			h.db.Exec(`DELETE FROM connections WHERE id = ? AND status = 'auth_failed'`, connID)
		}
	}()
}

// OAuthStatus checks if an OAuth connection has received its tokens.
func (h *OAuthHandler) OAuthStatus(c *gin.Context) {
	connID := c.Param("id")
	var oauthToken sql.NullString
	var name string
	err := h.db.QueryRow(`SELECT COALESCE(oauth_token, ''), name FROM connections WHERE id = ?`, connID).Scan(&oauthToken, &name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	connected := oauthToken.Valid && oauthToken.String != ""
	c.JSON(http.StatusOK, gin.H{"connected": connected, "name": name})
}

// SubmitOAuthCallback lets remote dashboard users paste the localhost callback URL.
// The backend forwards it to the local callback server started by InitiateOAuth,
// preserving provider-specific PKCE verifier and redirect_uri handling.
func (h *OAuthHandler) SubmitOAuthCallback(c *gin.Context) {
	var req struct {
		RedirectURL string `json:"redirect_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	u, err := url.Parse(req.RedirectURL)
	if err != nil || u.Scheme != "http" || u.Path != "/auth/callback" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid callback URL"})
		return
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "callback URL must point to localhost"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(req.RedirectURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "callback submit failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("callback rejected %d: %s", resp.StatusCode, string(body))})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func generateOAuthState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
