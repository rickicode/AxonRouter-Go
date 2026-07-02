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
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// oauthSession tracks an in-flight OAuth attempt before any connection exists in the DB.
type oauthSession struct {
	provider     string
	providerName string
	status       string // "pending", "connected", "failed"
	name         string // email from OAuth
	connID       string // created connection ID (only after success)
	err          string
}

// OAuthHandler manages OAuth flows for providers.
type OAuthHandler struct {
	db       *sql.DB
	authMgr  *auth.Manager
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	sessions sync.Map // sessionID -> *oauthSession
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(db *sql.DB, authMgr *auth.Manager, store *connstate.Store, elig *connstate.EligibilityManager) *OAuthHandler {
	return &OAuthHandler{db: db, authMgr: authMgr, store: store, elig: elig}
}

// StartOAuth begins an OAuth flow WITHOUT creating a connection.
// Returns auth_url + session_id. Connection is only created when OAuth succeeds.
// This matches OmniRoute behavior: no orphaned connections on failed OAuth.
func (h *OAuthHandler) StartOAuth(c *gin.Context) {
	var req struct {
		Provider     string `json:"provider" binding:"required"`
		ProviderName string `json:"provider_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	providerType := auth.ProviderType(req.Provider)
	svc, ok := h.authMgr.GetService(providerType)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OAuth not supported for provider: " + req.Provider})
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

	stateWithPort := state + ":" + strconv.Itoa(port)
	authURL, err := svc.GenerateAuthURL(ctx, stateWithPort)
	if err != nil {
		cancel()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate auth URL"})
		return
	}

	sessionID := generateOAuthState() // reuse random hex generator
	session := &oauthSession{
		provider:     req.Provider,
		providerName: req.ProviderName,
		status:       "pending",
	}
	h.sessions.Store(sessionID, session)

	resp := gin.H{
		"auth_url":   authURL,
		"session_id": sessionID,
		"port":       port,
	}
	// Device code flow: include user_code so frontend can display it
	type userCoder interface{ GetUserCode(string) string }
	if uc, ok := svc.(userCoder); ok {
		if code := uc.GetUserCode(state); code != "" {
			resp["user_code"] = code
		}
	}
	c.JSON(http.StatusOK, resp)

	go func() {
		defer cancel()
		defer h.sessions.Delete(sessionID)
		select {
		case creds := <-resultChan:
			if creds == nil {
				session.status = "failed"
				session.err = "nil credentials"
				log.Printf("OAuth nil credentials for session %s", sessionID)
				return
			}

			// Build connection name from email or provider name
			connName := "OAuth " + req.Provider
			if creds.Email != "" {
				connName = creds.Email
			} else if req.ProviderName != "" {
				connName = "OAuth " + req.ProviderName
			}

			// Create connection ONLY on success
			connID := uuid.New().String()
			now := time.Now().Unix()
			providerSpecific := sql.NullString{}
			if len(creds.ProviderSpecific) > 0 {
				if b, err := json.Marshal(creds.ProviderSpecific); err == nil {
					providerSpecific = sql.NullString{String: string(b), Valid: true}
				}
			}

			_, err := h.db.Exec(`
				INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, is_active, created_at, updated_at)
				VALUES (?, ?, ?, 'oauth', ?, ?, ?, ?, 'ready', 1, ?, ?)
			`, connID, req.Provider, connName, creds.AccessToken, creds.RefreshToken, creds.ExpiresAt.Unix(), providerSpecific, now, now)
			if err != nil {
				session.status = "failed"
				session.err = "failed to create connection: " + err.Error()
				log.Printf("OAuth create connection failed for session %s: %v", sessionID, err)
				return
			}

			session.status = "connected"
			session.name = connName
			session.connID = connID
			log.Printf("OAuth connection created: %s (%s) for session %s", connID, connName, sessionID)

			// Sync in-memory state
			if h.store != nil {
				h.store.UpdateStatus(connID, connstate.StatusReady)
				if h.elig != nil {
					h.elig.Update(h.store)
				}
			}

		case <-time.After(5 * time.Minute):
			session.status = "failed"
			session.err = "OAuth timeout after 5 minutes"
			log.Printf("OAuth timeout for session %s", sessionID)
		}
	}()
}

// PollOAuth checks the status of an OAuth session.
func (h *OAuthHandler) PollOAuth(c *gin.Context) {
	sessionID := c.Param("sessionId")
	val, ok := h.sessions.Load(sessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found or expired"})
		return
	}
	session := val.(*oauthSession)
	c.JSON(http.StatusOK, gin.H{
		"status":        session.status,
		"name":          session.name,
		"connection_id": session.connID,
		"error":         session.err,
	})
}

// SubmitOAuthCallback lets remote dashboard users paste the localhost callback URL.
func (h *OAuthHandler) SubmitOAuthCallback(c *gin.Context) {
	var req struct {
		RedirectURL string `json:"redirect_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	u, err := url.Parse(req.RedirectURL)
	if err != nil || u.Scheme != "http" || (u.Path != "/auth/callback" && u.Path != "/callback") {
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
