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
	ProviderCodex       ProviderType = "cx"
	ProviderAntigravity ProviderType = "ag"
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
	mu           sync.RWMutex
	services     map[ProviderType]OAuthService
	refreshGroup singleflight.Group

	// Rotation-group serializer: prevents concurrent refreshes for providers
	// sharing the same Auth0 client_id from invalidating each other's tokens.
	// Matches OmniRoute refreshSerializer.ts rotation groups.
	groupMu   sync.Mutex
	groupTail map[string]chan struct{} // groupKey → channel used as serialization gate
}

// rotationGroup maps providers to their Auth0/OIDC rotation group.
// Providers sharing a group MUST NOT refresh concurrently.
var rotationGroup = map[ProviderType]string{
	ProviderCodex: "openai-auth0", // Codex/Auth0: rotating refresh tokens
	ProviderKiro:  "kiro",         // Kiro: AWS SSO OIDC one-time-use refresh tokens
}

// ProviderAntigravity (Google) is NOT in rotation groups — Google refresh tokens are permanent/non-rotating.

const refreshSpacingMs = 2000 // 2s settle gap between sibling refreshes

// NewManager creates a new auth manager.
func NewManager() *Manager {
	return &Manager{
		services:  make(map[ProviderType]OAuthService),
		groupTail: make(map[string]chan struct{}),
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
func (m *Manager) RefreshToken(ctx context.Context, provider ProviderType, creds *Credentials) (*Credentials, error) {
	svc, ok := m.GetService(provider)
	if !ok {
		return nil, fmt.Errorf("no auth service for provider: %s", provider)
	}

	// Rotation-group serialization: if this provider is in a rotation group,
	// wait for any sibling refresh to complete + settle gap before proceeding.
	if group, ok := rotationGroup[provider]; ok {
		m.groupMu.Lock()
		tail, exists := m.groupTail[group]
		if !exists {
			tail = make(chan struct{}, 1)
			tail <- struct{}{} // initial unlock
			m.groupTail[group] = tail
		}
		m.groupMu.Unlock()

		// Wait for our turn in the group
		select {
		case <-tail:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		// Release the gate after this refresh completes + settle gap
		defer func() {
			go func() {
				time.Sleep(time.Duration(refreshSpacingMs) * time.Millisecond)
				tail <- struct{}{}
			}()
		}()
	}

	// Singleflight dedup: if the same refresh_token is already being refreshed,
	// wait for that result instead of starting a new request.
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
