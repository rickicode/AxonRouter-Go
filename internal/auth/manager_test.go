package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestProviderGrokCliConstant proves the grok-cli provider type identifier exists
// and matches the expected value used throughout routing and the dashboard.
func TestProviderGrokCliConstant(t *testing.T) {
	if ProviderGrokCli != "grok-cli" {
		t.Fatalf("ProviderGrokCli = %q, want %q", ProviderGrokCli, "grok-cli")
	}
}

type fakeRefreshOAuthService struct {
	called int
	creds  *Credentials
	returnCreds *Credentials
	err    error
}

func (f *fakeRefreshOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (f *fakeRefreshOAuthService) ExchangeCode(ctx context.Context, code string) (*Credentials, error) {
	return nil, nil
}

func (f *fakeRefreshOAuthService) RefreshToken(ctx context.Context, creds *Credentials) (*Credentials, error) {
	f.called++
	f.creds = creds
	if f.err != nil {
		return nil, f.err
	}
	return f.returnCreds, nil
}

func (f *fakeRefreshOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *Credentials, error) {
	return 0, nil, nil
}

type fakeTokenWriter struct {
	currentRefresh string
	getErr         error
	saved          bool
	saveErr        error
	lastConnID     string
	lastAccess     string
	lastRefresh    string
	lastExpiresAt  int64
	lastPSD        map[string]string
}

func (f *fakeTokenWriter) GetRefreshToken(ctx context.Context, connID string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.currentRefresh, nil
}

func (f *fakeTokenWriter) SaveTokens(ctx context.Context, connID, accessToken, refreshToken string, expiresAt int64, providerSpecific map[string]string) error {
	f.saved = true
	f.lastConnID = connID
	f.lastAccess = accessToken
	f.lastRefresh = refreshToken
	f.lastExpiresAt = expiresAt
	f.lastPSD = providerSpecific
	return f.saveErr
}

func TestRefreshTokenForConnection_WritesWhenCurrentTokenMatches(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	writer := &fakeTokenWriter{currentRefresh: "old-refresh"}
	mgr := NewManagerWithWriter(writer)
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	newCreds, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), oldCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Fatalf("access token = %q, want new-access", newCreds.AccessToken)
	}
	if !writer.saved {
		t.Fatal("expected DB write when current refresh token matches presented token")
	}
	if writer.lastConnID != "conn-1" {
		t.Fatalf("connID = %q, want conn-1", writer.lastConnID)
	}
	if writer.lastAccess != "new-access" {
		t.Fatalf("saved access token = %q, want new-access", writer.lastAccess)
	}
	if writer.lastRefresh != "new-refresh" {
		t.Fatalf("saved refresh token = %q, want new-refresh", writer.lastRefresh)
	}
}

func TestRefreshTokenForConnection_SkipsWriteWhenTokenRotatedByOtherWriter(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	writer := &fakeTokenWriter{currentRefresh: "already-rotated"}
	mgr := NewManagerWithWriter(writer)
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	newCreds, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), oldCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Fatalf("access token = %q, want new-access", newCreds.AccessToken)
	}
	if writer.saved {
		t.Fatal("expected DB write to be skipped when another writer already rotated the refresh token")
	}
}

func TestRefreshTokenForConnection_RecordsRotationEvenWhenWriteSkipped(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	writer := &fakeTokenWriter{currentRefresh: "already-rotated"}
	mgr := NewManagerWithWriter(writer)
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	if _, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), oldCreds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A subsequent call with the stale refresh token should be served from the rotation map
	// without invoking the OAuth service again.
	staleCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	svc.called = 0
	rotated, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), staleCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.called != 0 {
		t.Fatalf("expected rotation map to satisfy stale refresh, but OAuth service was called %d times", svc.called)
	}
	if rotated.AccessToken != "new-access" {
		t.Fatalf("rotated access token = %q, want new-access", rotated.AccessToken)
	}
	if rotated.RefreshToken != "new-refresh" {
		t.Fatalf("rotated refresh token = %q, want new-refresh", rotated.RefreshToken)
	}
}

func TestRefreshTokenForConnection_PreservesOldRefreshWhenProviderOmitsIt(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken: "new-access",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	writer := &fakeTokenWriter{currentRefresh: "old-refresh"}
	mgr := NewManagerWithWriter(writer)
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	if _, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), oldCreds); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !writer.saved {
		t.Fatal("expected DB write")
	}
	if writer.lastRefresh != "old-refresh" {
		t.Fatalf("saved refresh token = %q, want old-refresh", writer.lastRefresh)
	}
}

func TestRefreshTokenForConnection_PropagatesSaveError(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	writer := &fakeTokenWriter{currentRefresh: "old-refresh", saveErr: errors.New("db busy")}
	mgr := NewManagerWithWriter(writer)
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	_, err := mgr.RefreshTokenForConnection(ctx, "conn-1", ProviderType("test"), oldCreds)
	if err == nil {
		t.Fatal("expected save error to be returned")
	}
}

func TestRefreshToken_WithoutWriterDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	svc := &fakeRefreshOAuthService{
		returnCreds: &Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	mgr := NewManager()
	mgr.RegisterService(ProviderType("test"), svc)

	oldCreds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	newCreds, err := mgr.RefreshToken(ctx, ProviderType("test"), oldCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Fatalf("access token = %q, want new-access", newCreds.AccessToken)
	}
	if svc.called != 1 {
		t.Fatalf("OAuth service calls = %d, want 1", svc.called)
	}
}
