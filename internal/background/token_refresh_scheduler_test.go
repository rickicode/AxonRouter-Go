package background

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	queuedb "github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

type mockTokenRefreshService struct {
	refreshed bool
	lastCreds *auth.Credentials
	returnErr error
	creds     *auth.Credentials
}

func (m *mockTokenRefreshService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (m *mockTokenRefreshService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (m *mockTokenRefreshService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	m.refreshed = true
	m.lastCreds = creds
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.creds, nil
}

func (m *mockTokenRefreshService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func newTokenRefreshTestDB(t *testing.T) *sql.DB {
	database, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS connections (
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
	)`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestTokenRefreshScheduler_RefreshesNearExpiryToken(t *testing.T) {
	database := newTokenRefreshTestDB(t)
	defer database.Close()

	connID := "conn-tr"
	oldToken := "old-access"
	oldRefresh := "old-refresh"
	// Expires in 4 minutes, inside the 5-minute lead.
	oldExpiry := time.Now().Add(4 * time.Minute).Unix()

	_, err := database.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", oldToken, oldRefresh, oldExpiry, `{"profileArn":"arn:old"}`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockTokenRefreshService{
		creds: &auth.Credentials{
			AccessToken:      "new-access",
			RefreshToken:     "new-refresh",
			ExpiresAt:        time.Now().Add(time.Hour),
			ProviderSpecific: map[string]string{"profileArn": "arn:new"},
		},
	}
	mgr := auth.NewManagerWithWriter(queuedb.NewOAuthTokenWriter(database, nil))
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)

	s := NewTokenRefreshScheduler(database, nil, store, elig, mgr, 1)
	// Directly invoke the check for deterministic testing.
	s.check()

	// Wait for the async refresh goroutine to finish.
	time.Sleep(200 * time.Millisecond)

	if !mock.refreshed {
		t.Fatal("expected RefreshToken to be called")
	}
	if mock.lastCreds == nil {
		t.Fatal("expected credentials to be passed to RefreshToken")
	}
	if mock.lastCreds.ProviderSpecific["profileArn"] != "arn:old" {
		t.Errorf("expected profileArn forwarded, got %q", mock.lastCreds.ProviderSpecific["profileArn"])
	}

	var gotToken, gotRefresh, gotPSD string
	var gotExpiry int64
	if err := database.QueryRow(`SELECT oauth_token, oauth_refresh_token, oauth_expires_at, provider_specific_data FROM connections WHERE id = ?`, connID).Scan(&gotToken, &gotRefresh, &gotExpiry, &gotPSD); err != nil {
		t.Fatal(err)
	}
	if gotToken != "new-access" {
		t.Errorf("expected token new-access, got %s", gotToken)
	}
	if gotRefresh != "new-refresh" {
		t.Errorf("expected refresh new-refresh, got %s", gotRefresh)
	}
	if gotPSD != `{"profileArn":"arn:new"}` {
		t.Errorf("expected provider_specific_data updated, got %s", gotPSD)
	}
	if gotExpiry <= oldExpiry {
		t.Errorf("expected expiry increased, got %d", gotExpiry)
	}
}

func TestTokenRefreshScheduler_SkipsTokenOutsideLeadWindow(t *testing.T) {
	database := newTokenRefreshTestDB(t)
	defer database.Close()

	connID := "conn-skip"
	oldExpiry := time.Now().Add(30 * time.Minute).Unix()

	_, err := database.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", "old-access", "old-refresh", oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockTokenRefreshService{creds: &auth.Credentials{AccessToken: "x"}}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)

	s := NewTokenRefreshScheduler(database, nil, nil, nil, mgr, 1)
	s.check()

	if mock.refreshed {
		t.Error("expected no refresh for token outside lead window")
	}
}

func TestTokenRefreshScheduler_MarksAuthFailedOnUnrecoverableError(t *testing.T) {
	database := newTokenRefreshTestDB(t)
	defer database.Close()

	connID := "conn-fail"
	oldExpiry := time.Now().Add(-time.Minute).Unix()

	_, err := database.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", "old-access", "old-refresh", oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockTokenRefreshService{returnErr: errors.New("invalid_grant: refresh token is invalid")}
	mgr := auth.NewManager()
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)

	s := NewTokenRefreshScheduler(database, nil, store, elig, mgr, 1)
	s.check()
	time.Sleep(200 * time.Millisecond)

	if !mock.refreshed {
		t.Fatal("expected refresh attempt")
	}

	var isActive int
	var status string
	if err := database.QueryRow(`SELECT is_active, status FROM connections WHERE id = ?`, connID).Scan(&isActive, &status); err != nil {
		t.Fatal(err)
	}
	if isActive != 0 {
		t.Errorf("expected connection disabled, got is_active=%d", isActive)
	}
	if status != "auth_failed" {
		t.Errorf("expected status auth_failed, got %q", status)
	}
}

func TestTokenRefreshScheduler_UsesWriteQueueWhenProvided(t *testing.T) {
	database := newTokenRefreshTestDB(t)
	defer database.Close()

	connID := "conn-wq"
	oldExpiry := time.Now().Add(-time.Minute).Unix()

	_, err := database.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, name, oauth_token, oauth_refresh_token, oauth_expires_at, status, updated_at)
		VALUES (?, ?, 'oauth', 1, 'Test Conn', ?, ?, ?, 'ready', ?)`,
		connID, "testprovider", "old-access", "old-refresh", oldExpiry, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockTokenRefreshService{
		creds: &auth.Credentials{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	wq := queuedb.NewWriteQueue(database)
	defer wq.Stop()

	mgr := auth.NewManagerWithWriter(queuedb.NewOAuthTokenWriter(database, wq))
	mgr.RegisterService(auth.ProviderType("testprovider"), mock)

	s := NewTokenRefreshScheduler(database, wq, nil, nil, mgr, 1)
	s.check()

	// Wait for the async refresh to enqueue the write, then drain the queue.
	time.Sleep(200 * time.Millisecond)
	wq.FlushIdle(2 * time.Second)

	var gotToken string
	if err := database.QueryRow(`SELECT oauth_token FROM connections WHERE id = ?`, connID).Scan(&gotToken); err != nil {
		t.Fatal(err)
	}
	if gotToken != "new-access" {
		t.Errorf("expected token new-access via write queue, got %s", gotToken)
	}
}
