package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// TokenStore manages persistent token storage for Codex.
type TokenStore struct {
	mu       sync.RWMutex
	filePath string
	tokens   map[string]*auth.Credentials // keyed by account_id
	oauth    *OAuthService
}

// NewTokenStore creates a new Codex token store.
func NewTokenStore(storageDir string, oauth *OAuthService) *TokenStore {
	return &TokenStore{
		filePath: filepath.Join(storageDir, "codex_tokens.json"),
		tokens:   make(map[string]*auth.Credentials),
		oauth:    oauth,
	}
}

// Load reads tokens from disk.
func (ts *TokenStore) Load() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read token file: %w", err)
	}

	var tokens map[string]*auth.Credentials
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parse token file: %w", err)
	}

	ts.tokens = tokens
	return nil
}

// Save writes tokens to disk.
func (ts *TokenStore) Save() error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(ts.filePath), 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	data, err := json.MarshalIndent(ts.tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	return os.WriteFile(ts.filePath, data, 0600)
}

// Get returns credentials for an account, refreshing if needed.
func (ts *TokenStore) Get(ctx context.Context, accountID string) (*auth.Credentials, error) {
	ts.mu.RLock()
	creds, ok := ts.tokens[accountID]
	ts.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no credentials for account: %s", accountID)
	}

	if creds.HasValidToken() {
		return creds, nil
	}

	// Need refresh
	newCreds, err := ts.oauth.RefreshToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	ts.mu.Lock()
	ts.tokens[accountID] = newCreds
	ts.mu.Unlock()

	if err := ts.Save(); err != nil {
		// Log but don't fail — token is still in memory
		fmt.Printf("warning: failed to save tokens: %v\n", err)
	}

	return newCreds, nil
}

// Store saves credentials for an account.
func (ts *TokenStore) Store(accountID string, creds *auth.Credentials) {
	ts.mu.Lock()
	ts.tokens[accountID] = creds
	ts.mu.Unlock()
	ts.Save()
}

// List returns all stored account IDs.
func (ts *TokenStore) List() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	ids := make([]string, 0, len(ts.tokens))
	for id := range ts.tokens {
		ids = append(ids, id)
	}
	return ids
}

// Remove deletes credentials for an account.
func (ts *TokenStore) Remove(accountID string) {
	ts.mu.Lock()
	delete(ts.tokens, accountID)
	ts.mu.Unlock()
	ts.Save()
}

// RefreshAll refreshes all expired tokens.
func (ts *TokenStore) RefreshAll(ctx context.Context) error {
	ts.mu.RLock()
	accounts := make([]string, 0, len(ts.tokens))
	for id, creds := range ts.tokens {
		if creds.IsExpired() && creds.RefreshToken != "" {
			accounts = append(accounts, id)
		}
	}
	ts.mu.RUnlock()

	for _, id := range accounts {
		if _, err := ts.Get(ctx, id); err != nil {
			fmt.Printf("warning: refresh failed for %s: %v\n", id, err)
		}
	}
	return nil
}

// StartRefreshLoop starts a background goroutine that refreshes tokens.
func (ts *TokenStore) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts.RefreshAll(ctx)
			}
		}
	}()
}
