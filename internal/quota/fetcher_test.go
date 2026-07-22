package quota

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	_ "modernc.org/sqlite"
)

type mockOAuthService struct {
	refreshed   bool
	returnErr   error
	creds       *auth.Credentials
	lastCreds   *auth.Credentials
}

func (m *mockOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (m *mockOAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (m *mockOAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	m.refreshed = true
	m.lastCreds = creds
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.creds, nil
}

func (m *mockOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func newFetcherTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE connections (
		id TEXT PRIMARY KEY,
		provider_type_id TEXT,
		auth_type TEXT,
		is_active INTEGER,
		name TEXT,
		oauth_token TEXT,
		oauth_refresh_token TEXT,
		oauth_expires_at INTEGER,
		provider_specific_data TEXT,
		status TEXT,
		updated_at INTEGER
	)`)
	return db
}

func TestFetchConnectionQuota_RefreshesTokenViaAuthManager(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-refresh"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	oldExpiry := time.Now().Add(-time.Minute).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldRefresh, oldExpiry, `{"key":"old"}`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{
		creds: &auth.Credentials{
			AccessToken:      "new-access",
			RefreshToken:     "new-refresh",
			ExpiresAt:        time.Now().Add(time.Hour),
			ProviderSpecific: map[string]string{"key": "newval"},
		},
	}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	_, err = FetchConnectionQuota(db, connID)
	if err != nil {
		t.Fatalf("FetchConnectionQuota: %v", err)
	}

	if !mock.refreshed {
		t.Fatal("expected auth manager RefreshToken to be called")
	}

	var gotToken, gotRefresh, gotPSD string
	var gotExpiry int64
	if err := db.QueryRow(`SELECT oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data FROM connections WHERE id = ?`, connID).Scan(&gotToken, &gotRefresh, &gotExpiry, &gotPSD); err != nil {
		t.Fatal(err)
	}
	if gotToken != "new-access" {
		t.Errorf("expected access token new-access, got %s", gotToken)
	}
	if gotRefresh != "new-refresh" {
		t.Errorf("expected refresh token new-refresh, got %s", gotRefresh)
	}
	if gotExpiry <= oldExpiry {
		t.Errorf("expected expiry to increase, got %d", gotExpiry)
	}
	if gotPSD != `{"key":"newval"}` {
		t.Errorf("expected provider_specific_data updated, got %s", gotPSD)
	}
}

func TestFetchConnectionQuota_NoRefreshWhenTokenValid(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-valid"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	oldExpiry := time.Now().Add(time.Hour).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldRefresh, oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{
		creds: &auth.Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(2 * time.Hour),
		},
	}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	_, _ = FetchConnectionQuota(db, connID)

	if mock.refreshed {
		t.Error("expected no refresh when token is still valid")
	}

	var gotToken string
	if err := db.QueryRow(`SELECT oauth_token FROM connections WHERE id = ?`, connID).Scan(&gotToken); err != nil {
		t.Fatal(err)
	}
	if gotToken != oldToken {
		t.Errorf("expected token unchanged, got %s", gotToken)
	}
}

func TestFetchConnectionQuota_SkipsRefreshWithoutRefreshToken(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-norefresh"
	oldToken := "old-access"
	oldExpiry := time.Now().Add(-time.Minute).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{creds: &auth.Credentials{AccessToken: "x"}}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	_, _ = FetchConnectionQuota(db, connID)

	if mock.refreshed {
		t.Error("expected no refresh when refresh token is missing")
	}
}

func TestFetchConnectionQuota_PassesProviderSpecificToAuthManager(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-psd"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	oldExpiry := time.Now().Add(-time.Minute).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldRefresh, oldExpiry, `{"profileArn":"arn:test","ignored":42,"keep":"val"}`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{
		creds: &auth.Credentials{
			AccessToken:      "new-access",
			RefreshToken:     "new-refresh",
			ExpiresAt:        time.Now().Add(time.Hour),
			ProviderSpecific: map[string]string{"profileArn": "arn:new"},
		},
	}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	_, _ = FetchConnectionQuota(db, connID)

	if mock.lastCreds == nil {
		t.Fatal("expected RefreshToken to receive credentials")
	}
	if mock.lastCreds.AccessToken != oldToken {
		t.Errorf("expected access token %q, got %q", oldToken, mock.lastCreds.AccessToken)
	}
	if mock.lastCreds.RefreshToken != oldRefresh {
		t.Errorf("expected refresh token %q, got %q", oldRefresh, mock.lastCreds.RefreshToken)
	}
	gotPSD := mock.lastCreds.ProviderSpecific
	if gotPSD["profileArn"] != "arn:test" {
		t.Errorf("expected profileArn passed to auth manager, got %q", gotPSD["profileArn"])
	}
	if gotPSD["keep"] != "val" {
		t.Errorf("expected keep string passed, got %q", gotPSD["keep"])
	}
	if _, ok := gotPSD["ignored"]; ok {
		t.Errorf("expected non-string provider_specific_data keys to be skipped")
	}
}

func TestFetchConnectionQuota_DeferredRefreshWhenTokenStillValid(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-deferred"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	// Expires in 4 minutes, inside the default 5-minute refresh lead but still valid.
	oldExpiry := time.Now().Add(4 * time.Minute).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldRefresh, oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{returnErr: errors.New("social refresh failed 401: Bad credentials")}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	cq, err := FetchConnectionQuota(db, connID)
	if err != nil {
		t.Fatalf("FetchConnectionQuota: %v", err)
	}

	if !mock.refreshed {
		t.Fatal("expected refresh to be attempted")
	}
	if cq.Error != "" {
		t.Errorf("expected deferred refresh when token still valid, got hard error %q", cq.Error)
	}

	var isActive int
	if err := db.QueryRow(`SELECT is_active FROM connections WHERE id = ?`, connID).Scan(&isActive); err != nil {
		t.Fatal(err)
	}
	if isActive != 1 {
		t.Errorf("expected connection to remain active, got is_active=%d", isActive)
	}
}

func TestForceRefreshOnQuotaAuthError_UpdatesTokenAndProviderSpecific(t *testing.T) {
	db := newFetcherTestDB(t)
	defer db.Close()

	connID := "conn-force-refresh"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	oldExpiry := time.Now().Add(-time.Hour).Unix()

	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, ?, 'ready', ?)`,
		connID, "kiro", oldToken, oldRefresh, oldExpiry, `{"profileArn":"arn:old"}`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockOAuthService{
		creds: &auth.Credentials{
			AccessToken:      "forced-access",
			RefreshToken:     "forced-refresh",
			ExpiresAt:        time.Now().Add(time.Hour),
			ProviderSpecific: map[string]string{"profileArn": "arn:forced"},
		},
	}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("kiro"), mock)
	SetAuthManager(mgr)
	defer SetAuthManager(nil)

	c := connRow{
		ID:                   connID,
		ProviderTypeID:       "kiro",
		Name:                 "Test Conn",
		OAuthToken:           sql.NullString{String: oldToken, Valid: true},
		OAuthRefreshToken:    sql.NullString{String: oldRefresh, Valid: true},
		OAuthExpiresAt:       oldExpiry,
		ProviderSpecificData: sql.NullString{String: `{"profileArn":"arn:old"}`, Valid: true},
	}

	newToken, psd, ok := forceRefreshOnQuotaAuthError(c, oldToken, mapStringToAny(map[string]string{"profileArn": "arn:old"}), "kiro", db)
	if !ok {
		t.Fatal("expected forced refresh to succeed")
	}
	if newToken != "forced-access" {
		t.Errorf("expected forced access token forced-access, got %s", newToken)
	}
	if psd["profileArn"] != "arn:forced" {
		t.Errorf("expected profileArn arn:forced, got %v", psd["profileArn"])
	}

	var gotToken, gotPSD string
	if err := db.QueryRow(`SELECT oauth_token, provider_specific_data FROM connections WHERE id = ?`, connID).Scan(&gotToken, &gotPSD); err != nil {
		t.Fatal(err)
	}
	if gotToken != "forced-access" {
		t.Errorf("expected persisted token forced-access, got %s", gotToken)
	}
	if gotPSD != `{"profileArn":"arn:forced"}` {
		t.Errorf("expected persisted provider_specific_data updated, got %s", gotPSD)
	}
}
