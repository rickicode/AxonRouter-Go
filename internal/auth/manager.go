package auth

import (
	"context"
	"crypto/rand"
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
	ProviderCodex       ProviderType = "codex"
	ProviderAntigravity ProviderType = "antigravity"
	ProviderKiro        ProviderType = "kiro"
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

// Manager coordinates authentication across providers.
type Manager struct {
	mu          sync.RWMutex
	services    map[ProviderType]OAuthService
	refreshGroup singleflight.Group
}

// NewManager creates a new auth manager.
func NewManager() *Manager {
	return &Manager{
		services: make(map[ProviderType]OAuthService),
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

// RefreshToken refreshes tokens with singleflight deduplication.
func (m *Manager) RefreshToken(ctx context.Context, provider ProviderType, creds *Credentials) (*Credentials, error) {
	svc, ok := m.GetService(provider)
	if !ok {
		return nil, fmt.Errorf("no auth service for provider: %s", provider)
	}

	// Use singleflight to deduplicate concurrent refresh requests
	key := fmt.Sprintf("%s:%s", provider, creds.RefreshToken)
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
