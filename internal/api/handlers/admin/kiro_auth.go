package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/auth/kiro"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

const kiroSessionTimeout = 5 * time.Minute

// kiroAuthService is the subset of auth methods the dashboard needs.
type kiroAuthService interface {
	StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error)
	GenerateAuthURL(ctx context.Context, state string) (string, error)
	GetUserCode(state string) string
	StartDeviceFlow(ctx context.Context, state, region, startURL, issuerURL, authMethod string) (int, chan *auth.Credentials, error)
	StartSocial(provider string) (string, string, string, error)
	ExchangeSocialCode(ctx context.Context, sessionID, code string) (*auth.Credentials, error)
	ImportToken(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error)
	ValidateAPIKey(ctx context.Context, apiKey, region string) (*auth.Credentials, error)
	ImportExternalIDP(ctx context.Context, req kiro.ExternalIDPRequest) (*auth.Credentials, error)
	AutoImport(ctx context.Context) (*kiro.AutoImportResult, error)
}

// KiroAuthHandler exposes Kiro-specific auth endpoints.
type KiroAuthHandler struct {
	db       *sql.DB
	svc      kiroAuthService
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	sessions sync.Map // sessionID -> *kiroAuthSession
}

type kiroAuthSession struct {
	method string
	status string
	name   string
	connID string
	err    string
	doneAt time.Time
	creds  *auth.Credentials
}

// NewKiroAuthHandler creates a new Kiro auth handler.
func NewKiroAuthHandler(db *sql.DB, svc kiroAuthService, store *connstate.Store, elig *connstate.EligibilityManager) *KiroAuthHandler {
	return &KiroAuthHandler{db: db, svc: svc, store: store, elig: elig}
}

func (h *KiroAuthHandler) storeSession(id string, s *kiroAuthSession) { h.sessions.Store(id, s) }
func (h *KiroAuthHandler) loadSession(id string) *kiroAuthSession {
	v, ok := h.sessions.Load(id)
	if !ok {
		return nil
	}
	return v.(*kiroAuthSession)
}

// StartBuilderID starts the AWS Builder ID device-code flow.
func (h *KiroAuthHandler) StartBuilderID(c *gin.Context) {
	h.startDeviceFlow(c, "builder-id", "us-east-1", "https://view.awsapps.com/start", "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6")
}

// StartIDC starts an IAM Identity Center device-code flow.
func (h *KiroAuthHandler) StartIDC(c *gin.Context) {
	var req struct {
		StartURL  string `json:"start_url" binding:"required"`
		IssuerURL string `json:"issuer_url"`
		Region    string `json:"region"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.startDeviceFlow(c, "idc", req.Region, req.StartURL, req.IssuerURL)
}

func (h *KiroAuthHandler) startDeviceFlow(c *gin.Context, method, region, startURL, issuerURL string) {
	sessionID := generateKiroState()
	ctx, cancel := context.WithCancel(context.Background())
	port, resultChan, err := h.svc.StartDeviceFlow(ctx, sessionID, region, startURL, issuerURL, method)
	if err != nil {
		cancel()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	authURL, err := h.svc.GenerateAuthURL(ctx, sessionID+":"+strconv.Itoa(port))
	if err != nil {
		cancel()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	session := &kiroAuthSession{method: method, status: "pending"}
	h.storeSession(sessionID, session)

	resp := gin.H{
		"auth_url":   authURL,
		"session_id": sessionID,
		"port":       port,
	}
	if code := h.svc.GetUserCode(sessionID); code != "" {
		resp["user_code"] = code
	}
	c.JSON(http.StatusOK, resp)

	go h.watchResult(ctx, cancel, sessionID, session, resultChan)
}

// GoogleStart starts a Google social login flow.
func (h *KiroAuthHandler) GoogleStart(c *gin.Context) { h.startSocial(c, "google") }

// GitHubStart starts a GitHub social login flow.
func (h *KiroAuthHandler) GitHubStart(c *gin.Context) { h.startSocial(c, "github") }

func (h *KiroAuthHandler) startSocial(c *gin.Context, provider string) {
	authURL, sessionID, _, err := h.svc.StartSocial(provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.storeSession(sessionID, &kiroAuthSession{method: provider, status: "pending"})
	c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "session_id": sessionID})
}

// GoogleCallback exchanges a Google authorization code.
func (h *KiroAuthHandler) GoogleCallback(c *gin.Context) { h.socialCallback(c, "google") }

// GitHubCallback exchanges a GitHub authorization code.
func (h *KiroAuthHandler) GitHubCallback(c *gin.Context) { h.socialCallback(c, "github") }

func (h *KiroAuthHandler) socialCallback(c *gin.Context, provider string) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		Code      string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session := h.loadSession(req.SessionID)
	if session == nil || session.method != provider {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	creds, err := h.svc.ExchangeSocialCode(c.Request.Context(), req.SessionID, req.Code)
	if err != nil {
		session.status = "failed"
		session.err = err.Error()
		session.doneAt = time.Now()
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	connID, name, err := h.persistConnection(creds, provider)
	if err != nil {
		session.status = "failed"
		session.err = err.Error()
		session.doneAt = time.Now()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	session.status = "connected"
	session.name = name
	session.connID = connID
	session.creds = creds
	session.doneAt = time.Now()
	c.JSON(http.StatusCreated, gin.H{"connection_id": connID, "name": name, "status": "ready"})
}

// ImportKiroToken imports an AWS refresh token.
func (h *KiroAuthHandler) ImportKiroToken(c *gin.Context) {
	var req kiro.ImportTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	creds, err := h.svc.ImportToken(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	connID, name, err := h.persistConnection(creds, "import")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"connection_id": connID, "name": name, "status": "ready"})
}

// APIKey validates and stores a Kiro API key.
func (h *KiroAuthHandler) APIKey(c *gin.Context) {
	var req struct {
		APIKey string `json:"api_key" binding:"required"`
		Region string `json:"region"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	creds, err := h.svc.ValidateAPIKey(c.Request.Context(), req.APIKey, req.Region)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	connID, name, err := h.persistConnection(creds, "api_key")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"connection_id": connID, "name": name, "status": "ready"})
}

// AutoImport discovers credentials from kiro-cli SQLite or AWS SSO cache.
func (h *KiroAuthHandler) AutoImport(c *gin.Context) {
	res, err := h.svc.AutoImport(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !res.Found {
		c.JSON(http.StatusNotFound, gin.H{"error": res.Error, "tried_paths": res.TriedPaths})
		return
	}
	if err := kiro.ValidateDiscoveredCredential(res); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "result": res})
		return
	}
	c.JSON(http.StatusOK, res)
}

// ExternalIDP imports an enterprise SSO credential.
func (h *KiroAuthHandler) ExternalIDP(c *gin.Context) {
	var req kiro.ExternalIDPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	creds, err := h.svc.ImportExternalIDP(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	connID, name, err := h.persistConnection(creds, "external_idp")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"connection_id": connID, "name": name, "status": "ready"})
}

// Poll checks the status of any Kiro auth session.
func (h *KiroAuthHandler) Poll(c *gin.Context) {
	h.sweepSessions()
	sessionID := c.Param("sessionId")
	session := h.loadSession(sessionID)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":        session.status,
		"method":        session.method,
		"name":          session.name,
		"connection_id": session.connID,
		"error":         session.err,
	})
}

func (h *KiroAuthHandler) watchResult(ctx context.Context, cancel context.CancelFunc, sessionID string, session *kiroAuthSession, resultChan chan *auth.Credentials) {
	defer cancel()
	select {
	case creds := <-resultChan:
		if creds == nil {
			session.status = "failed"
			session.err = "OAuth session ended without credentials"
			session.doneAt = time.Now()
			return
		}
		if creds.ProviderSpecific["__oauth_error__"] != "" {
			session.status = "failed"
			session.err = creds.ProviderSpecific["__oauth_error__"]
			session.doneAt = time.Now()
			return
		}
		if creds.AccessToken == "" {
			session.status = "failed"
			session.err = "OAuth succeeded but access token is empty"
			session.doneAt = time.Now()
			return
		}
		connID, name, err := h.persistConnection(creds, session.method)
		if err != nil {
			session.status = "failed"
			session.err = err.Error()
			session.doneAt = time.Now()
			return
		}
		session.status = "connected"
		session.name = name
		session.connID = connID
		session.creds = creds
		session.doneAt = time.Now()
	case <-time.After(kiroSessionTimeout):
		session.status = "failed"
		session.err = "OAuth timeout after 5 minutes"
		session.doneAt = time.Now()
	}
}

func (h *KiroAuthHandler) persistConnection(creds *auth.Credentials, method string) (string, string, error) {
	connName := creds.Email
	if connName == "" {
		fallback, err := nextKiroFallbackName(h.db)
		if err != nil {
			fallback = "Kiro-1"
		}
		connName = fallback
	}
	connID := uuid.New().String()
	now := time.Now().Unix()

	psd := creds.ProviderSpecific
	if psd == nil {
		psd = map[string]string{"authMethod": method}
	} else if psd["authMethod"] == "" {
		psd["authMethod"] = method
	}
	psdJSON, _ := json.Marshal(psd)
	expiresAt := int64(0)
	if !creds.ExpiresAt.IsZero() {
		expiresAt = creds.ExpiresAt.Unix()
	}

	_, err := h.db.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, is_active, created_at, updated_at)
		VALUES (?, 'kiro', ?, 'oauth', ?, ?, ?, ?, 'ready', 1, ?, ?)
	`, connID, connName, creds.AccessToken, creds.RefreshToken, expiresAt, psdJSON, now, now)
	if err != nil {
		return "", "", fmt.Errorf("failed to create connection: %w", err)
	}
	if h.store != nil {
		h.store.SeedConnection(connID, "kiro", "ready", 0)
		if h.elig != nil {
			h.elig.Update(h.store)
		}
	}
	return connID, connName, nil
}

func (h *KiroAuthHandler) sweepSessions() {
	h.sessions.Range(func(key, value any) bool {
		s := value.(*kiroAuthSession)
		if !s.doneAt.IsZero() && time.Since(s.doneAt) > 30*time.Second {
			h.sessions.Delete(key)
		}
		return true
	})
}

func generateKiroState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
