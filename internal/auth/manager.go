package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ProviderType identifies an auth provider.
type ProviderType string

const (
	ProviderCodex       ProviderType = "cx"
	ProviderAntigravity ProviderType = "ag"
	ProviderKiro        ProviderType = "kiro"
	ProviderGitHub      ProviderType = "copilot"
	ProviderGrokCli     ProviderType = "grok-cli"
	ProviderCodeBuddy   ProviderType = "codebuddy"
ProviderQoder       ProviderType = "qoder"
)

// Credentials holds authentication tokens for a connection.
type Credentials struct {
	APIKey           string            `json:"api_key,omitempty"`
	AccessToken      string            `json:"access_token,omitempty"`
	RefreshToken     string            `json:"refresh_token,omitempty"`
	IDToken          string            `json:"id_token,omitempty"`
	AccountID        string            `json:"account_id,omitempty"`
	Email            string            `json:"email,omitempty"`
	ExpiresAt        time.Time         `json:"expires_at,omitempty"`
	ProviderSpecific map[string]string `json:"provider_specific,omitempty"`
}

// IsExpired checks if the access token is expired.
func (c *Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt.Add(-30 * time.Second)) // 30s buffer
}

// HasValidToken checks if there's a usable access token.
func (c *Credentials) HasValidToken() bool {
	return c.AccessToken != "" && !c.IsExpired()
}

// OAuthService handles OAuth flows for a provider.
type OAuthService interface {
	// GenerateAuthURL creates an authorization URL for the user to visit.
	GenerateAuthURL(ctx context.Context, state string) (authURL string, err error)
	// ExchangeCode exchanges an authorization code for tokens.
	ExchangeCode(ctx context.Context, code string) (*Credentials, error)
	// RefreshToken refreshes an expired access token.
	RefreshToken(ctx context.Context, creds *Credentials) (*Credentials, error)
	// StartLocalServer starts a local callback server and returns the port.
	StartLocalServer(ctx context.Context, state string) (port int, resultChan chan *Credentials, err error)
}

// TokenWriter persists refreshed OAuth tokens to the backing store. It is used
// by Manager.RefreshToken/RefreshTokenForConnection to apply a compare-and-swap
// (CAS) pattern: read the current refresh token, write only if it still matches
// the token we presented to the upstream provider. This prevents concurrent
// refreshes from overwriting a token rotated by another writer.
type TokenWriter interface {
	GetRefreshToken(ctx context.Context, connID string) (string, error)
	SaveTokens(ctx context.Context, connID, accessToken, refreshToken string, expiresAt int64, providerSpecific map[string]string) error
}

// Manager coordinates authentication across providers.
type Manager struct {
	mu           sync.RWMutex
	services     map[ProviderType]OAuthService
	refreshGroup singleflight.Group

	// Rotation-group serializer: prevents concurrent refreshes for providers
	// sharing the same Auth0 client_id from invalidating each other's tokens.
	// Matches OmniRoute refreshSerializer.ts rotation groups.
	groupMu   sync.Mutex
	groupTail map[string]chan struct{} // groupKey → channel used as serialization gate

	// Token rotation map: caches recent rotations so stale callers can be
	// redirected to new tokens without hitting upstream.
	// Matches OmniRoute tokenRefresh.ts:83-141 tokenRotationMap.
	rotationMu  sync.Mutex
	rotationMap map[string]rotationEntry // key → {result, expiresAt}

	// tokenWriter optionally persists refreshed tokens to the database using CAS.
	writerMu    sync.RWMutex
	tokenWriter TokenWriter
}

// rotationEntry caches a recent token rotation.
type rotationEntry struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	expiresAt    time.Time // entry expiry (60s TTL)
}

const rotationMapTTL = 60 * time.Second

// rotationGroup maps providers to their Auth0/OIDC rotation group.
var rotationGroup = map[ProviderType]string{
	ProviderCodex: "openai-auth0",
	ProviderKiro:  "kiro",
}

const refreshSpacingMs = 2000

// NewManager creates a new auth manager without persistence.
func NewManager() *Manager {
	return NewManagerWithWriter(nil)
}

// NewManagerWithWriter creates a new auth manager that persists refreshed
// OAuth tokens via the supplied TokenWriter using a CAS pattern.
func NewManagerWithWriter(w TokenWriter) *Manager {
	return &Manager{
		services:    make(map[ProviderType]OAuthService),
		groupTail:   make(map[string]chan struct{}),
		rotationMap: make(map[string]rotationEntry),
		tokenWriter: w,
	}
}

// SetTokenWriter configures the writer used for CAS token persistence.
func (m *Manager) SetTokenWriter(w TokenWriter) {
	m.writerMu.Lock()
	defer m.writerMu.Unlock()
	m.tokenWriter = w
}

// rotationCacheKey creates a cache key for the rotation map.
// Matches OmniRoute: provider:sha256(oldRefreshToken)
func rotationCacheKey(provider ProviderType, refreshToken string) string {
	h := sha256.Sum256([]byte(refreshToken))
	return string(provider) + ":" + hex.EncodeToString(h[:])
}

// lookupRotation checks if a stale refresh token was recently rotated.
func (m *Manager) lookupRotation(provider ProviderType, refreshToken string) *rotationEntry {
	if refreshToken == "" {
		return nil
	}
	m.rotationMu.Lock()
	defer m.rotationMu.Unlock()
	key := rotationCacheKey(provider, refreshToken)
	entry, ok := m.rotationMap[key]
	if !ok {
		return nil
	}
	if time.Now().After(entry.expiresAt) {
		delete(m.rotationMap, key)
		return nil
	}
	return &entry
}

// recordRotation caches a token rotation for stale caller redirect.
func (m *Manager) recordRotation(provider ProviderType, oldRefreshToken string, newCreds *Credentials) {
	if oldRefreshToken == "" || newCreds.RefreshToken == "" || oldRefreshToken == newCreds.RefreshToken {
		return
	}
	m.rotationMu.Lock()
	defer m.rotationMu.Unlock()
	key := rotationCacheKey(provider, oldRefreshToken)
	m.rotationMap[key] = rotationEntry{
		AccessToken:  newCreds.AccessToken,
		RefreshToken: newCreds.RefreshToken,
		ExpiresAt:    newCreds.ExpiresAt,
		expiresAt:    time.Now().Add(rotationMapTTL),
	}
}

// RegisterService registers an OAuth service for a provider.
func (m *Manager) RegisterService(provider ProviderType, svc OAuthService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[provider] = svc
}

// GetService returns the OAuth service for a provider.
func (m *Manager) GetService(provider ProviderType) (OAuthService, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.services[provider]
	return svc, ok
}

// RefreshToken refreshes tokens with singleflight deduplication AND rotation-group serialization.
// Providers in the same rotation group (e.g. codex+openai → openai-auth0) are serialized
// to prevent Auth0 family revocation. Matches OmniRoute refreshSerializer.ts.
// This method does not persist refreshed tokens; use RefreshTokenForConnection when you
// need CAS database persistence.
func (m *Manager) RefreshToken(ctx context.Context, provider ProviderType, creds *Credentials) (*Credentials, error) {
	return m.refreshToken(ctx, "", provider, creds)
}

// RefreshTokenForConnection refreshes tokens with the same deduplication/serialization
// guarantees as RefreshToken, and additionally persists refreshed tokens to the
// database using a compare-and-swap pattern keyed by connection ID. If the current
// DB refresh token differs from the token we presented, another writer already
// rotated it and our DB write is skipped, but the in-memory rotation map is still
// updated so stale callers can be redirected to the latest token.
func (m *Manager) RefreshTokenForConnection(ctx context.Context, connID string, provider ProviderType, creds *Credentials) (*Credentials, error) {
	return m.refreshToken(ctx, connID, provider, creds)
}

func (m *Manager) refreshToken(ctx context.Context, connID string, provider ProviderType, creds *Credentials) (*Credentials, error) {
	svc, ok := m.GetService(provider)
	if !ok {
		return nil, fmt.Errorf("no auth service for provider: %s", provider)
	}

	m.writerMu.RLock()
	writer := m.tokenWriter
	m.writerMu.RUnlock()

	// Check rotation map: if this refresh_token was recently rotated, return cached result
	if entry := m.lookupRotation(provider, creds.RefreshToken); entry != nil {
		return &Credentials{
			AccessToken:  entry.AccessToken,
			RefreshToken: entry.RefreshToken,
			ExpiresAt:    entry.ExpiresAt,
		}, nil
	}

	// Rotation-group serialization
	if group, ok := rotationGroup[provider]; ok {
		m.groupMu.Lock()
		tail, exists := m.groupTail[group]
		if !exists {
			tail = make(chan struct{}, 1)
			tail <- struct{}{}
			m.groupTail[group] = tail
		}
		m.groupMu.Unlock()

		select {
		case <-tail:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		defer func() {
			go func() {
				time.Sleep(time.Duration(refreshSpacingMs) * time.Millisecond)
				tail <- struct{}{}
			}()
		}()
	}

	// Singleflight dedup
	oldRefreshToken := creds.RefreshToken
	key := fmt.Sprintf("%s:%s", provider, oldRefreshToken)
	result, err, _ := m.refreshGroup.Do(key, func() (any, error) {
		return svc.RefreshToken(ctx, creds)
	})
	if err != nil {
		return nil, err
	}

	newCreds, ok := result.(*Credentials)
	if !ok {
		return nil, fmt.Errorf("unexpected refresh result type")
	}

	// Preserve old refresh token when provider omits it (Google/Antigravity often omit)
	if newCreds.RefreshToken == "" {
		newCreds.RefreshToken = oldRefreshToken
	}

	// CAS persistence: read current refresh token from DB and write only if it
	// matches the token we just presented. Skip writes when another writer has
	// already rotated the refresh token, but still update the rotation map below.
	if writer != nil && connID != "" {
		currentRefresh, readErr := writer.GetRefreshToken(ctx, connID)
		if readErr == nil {
			if currentRefresh == oldRefreshToken {
				expiresAt := newCreds.ExpiresAt.Unix()
				if err := writer.SaveTokens(ctx, connID, newCreds.AccessToken, newCreds.RefreshToken, expiresAt, newCreds.ProviderSpecific); err != nil {
					return nil, err
				}
			}
		}
		// If the read fails we cannot safely CAS; skip the write but do not fail
		// the refresh because the new tokens are still usable in memory.
	}

	// Record rotation for stale caller redirect
	m.recordRotation(provider, oldRefreshToken, newCreds)

	return newCreds, nil
}

// StartAuth starts an OAuth flow for a provider.
func (m *Manager) StartAuth(ctx context.Context, provider ProviderType) (authURL string, resultChan chan *Credentials, err error) {
	svc, ok := m.GetService(provider)
	if !ok {
		return "", nil, fmt.Errorf("no auth service for provider: %s", provider)
	}

	state := generateState()

	port, resultChan, err := svc.StartLocalServer(ctx, state)
	if err != nil {
		return "", nil, fmt.Errorf("start local server: %w", err)
	}

	authURL, err = svc.GenerateAuthURL(ctx, fmt.Sprintf("%s:%d", state, port))
	if err != nil {
		return "", nil, fmt.Errorf("generate auth URL: %w", err)
	}

	return authURL, resultChan, nil
}

// generateState creates a cryptographically random state parameter.
func generateState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// ponytail: fallback to time-based if crypto/rand fails (should never happen)
		log.Printf("WARN: crypto/rand failed, falling back to time: %v", err)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
