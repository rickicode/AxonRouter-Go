package codex

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	dbpkg "github.com/rickicode/AxonRouter-Go/internal/db"
)

func mustOpenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := dbpkg.RunMigrations(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func insertCodexConnection(t *testing.T, db *sql.DB, accessToken, refreshToken string, expiresAt int64) string {
	t.Helper()
	id := "cx-" + fmt.Sprintf("%d", time.Now().UnixNano())
	var refresh sql.NullString
	if refreshToken != "" {
		refresh = sql.NullString{String: refreshToken, Valid: true}
	}
	now := time.Now().Unix()
	_, err := db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, status, is_active, created_at, updated_at)
		VALUES (?, 'cx', 'Test', 'oauth', ?, ?, ?, 'ready', 1, ?, ?)`,
		id, accessToken, refresh, expiresAt, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testIDToken(accountID, email string) string {
	claims, _ := json.Marshal(map[string]string{"sub": accountID, "email": email})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	return "header." + payload + ".sig"
}

func tokenResponse(access, refresh, idToken string, expiresIn int) string {
	b, _ := json.Marshal(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"id_token":      idToken,
		"expires_in":    expiresIn,
		"token_type":    "Bearer",
	})
	return string(b)
}

func TestRefreshToken_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(tokenResponse("new-access", "new-refresh", testIDToken("acc1", "a@b"), 3600)))
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.tokenURL = ts.URL + "/token"
	creds := &auth.Credentials{RefreshToken: "old-refresh"}
	newCreds, err := svc.RefreshToken(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Errorf("access token = %q, want new-access", newCreds.AccessToken)
	}
	if newCreds.RefreshToken != "new-refresh" {
		t.Errorf("refresh token = %q, want new-refresh", newCreds.RefreshToken)
	}
	if newCreds.AccountID != "acc1" {
		t.Errorf("account id = %q, want acc1", newCreds.AccountID)
	}
}

func TestRefreshToken_FailsFastOnAuthError(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.tokenURL = ts.URL + "/token"
	_, err := svc.RefreshTokenWithRetry(context.Background(), &auth.Credentials{RefreshToken: "rt"})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (fail fast on 401)", calls)
	}
}

func TestRefreshTokenWithRetry_SucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(tokenResponse("ok-access", "ok-refresh", "", 3600)))
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.tokenURL = ts.URL + "/token"
	creds, err := svc.RefreshTokenWithRetry(context.Background(), &auth.Credentials{RefreshToken: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "ok-access" {
		t.Errorf("access token = %q, want ok-access", creds.AccessToken)
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
}

func TestTokenStore_GetReturnsValidTokenWithoutRefresh(t *testing.T) {
	db := mustOpenTestDB(t)
	id := insertCodexConnection(t, db, "valid-token", "refresh", time.Now().Add(time.Hour).Unix())
	store := NewTokenStore(db, "", NewOAuthService(nil))
	creds, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "valid-token" {
		t.Errorf("access token = %q", creds.AccessToken)
	}
}

func TestTokenStore_GetRefreshesExpiredTokenAndPersists(t *testing.T) {
	db := mustOpenTestDB(t)
	id := insertCodexConnection(t, db, "expired", "refresh", time.Now().Add(-time.Hour).Unix())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(tokenResponse("refreshed", "new-refresh", testIDToken("acc", ""), 3600)))
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.tokenURL = ts.URL + "/token"
	store := NewTokenStore(db, "", svc)

	creds, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "refreshed" {
		t.Errorf("access token = %q, want refreshed", creds.AccessToken)
	}

	var persisted string
	if err := db.QueryRow("SELECT oauth_token FROM connections WHERE id = ?", id).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != "refreshed" {
		t.Errorf("persisted token = %q, want refreshed", persisted)
	}
}

func TestTokenStore_ListAndRemove(t *testing.T) {
	db := mustOpenTestDB(t)
	insertCodexConnection(t, db, "a", "", time.Now().Add(time.Hour).Unix())
	insertCodexConnection(t, db, "b", "", time.Now().Add(time.Hour).Unix())
	store := NewTokenStore(db, "", NewOAuthService(nil))
	ids, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("listed %d ids, want 2", len(ids))
	}
	if err := store.Remove(context.Background(), ids[0]); err != nil {
		t.Fatal(err)
	}
	ids, _ = store.List(context.Background())
	if len(ids) != 1 {
		t.Errorf("after remove listed %d ids, want 1", len(ids))
	}
}

func TestImportCredentials_BareAccessToken(t *testing.T) {
	db := mustOpenTestDB(t)
	id, err := ImportCredentials(context.Background(), db, []byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected connection id")
	}
	var tok string
	if err := db.QueryRow("SELECT oauth_token FROM connections WHERE id = ?", id).Scan(&tok); err != nil {
		t.Fatal(err)
	}
	if tok != "at" {
		t.Errorf("token = %q", tok)
	}
}

func TestImportCredentials_CodexCLIAuthJSON(t *testing.T) {
	db := mustOpenTestDB(t)
	raw := []byte(`{"token":"cli-token","refreshToken":"cli-refresh","expiresAt":1893456000}`)
	id, err := ImportCredentials(context.Background(), db, raw)
	if err != nil {
		t.Fatal(err)
	}
	var tok string
	db.QueryRow("SELECT oauth_token FROM connections WHERE id = ?", id).Scan(&tok)
	if tok != "cli-token" {
		t.Errorf("token = %q, want cli-token", tok)
	}
}

func TestImportCredentials_ExtractsAccountIDFromIDToken(t *testing.T) {
	db := mustOpenTestDB(t)
	idToken := testIDToken("imported-acc", "import@example.com")
	raw := []byte(fmt.Sprintf(`{"access_token":"at","id_token":"%s"}`, idToken))
	connID, err := ImportCredentials(context.Background(), db, raw)
	if err != nil {
		t.Fatal(err)
	}
	var psd string
	db.QueryRow("SELECT provider_specific_data FROM connections WHERE id = ?", connID).Scan(&psd)
	if !strings.Contains(psd, "imported-acc") {
		t.Errorf("psd should contain account id, got %q", psd)
	}
	if !strings.Contains(psd, "import@example.com") {
		t.Errorf("psd should contain email, got %q", psd)
	}
}

func TestDeviceFlow_Start(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"device_auth_id":"daid","user_code":"UC-1234","verification_uri":"http://verify","interval":5}`))
	}))
	defer ts.Close()
	svc := NewOAuthService(ts.Client())
	svc.deviceUserCodeURL = ts.URL + "/usercode"
	resp, err := svc.RequestDeviceUserCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceAuthID != "daid" || resp.UserCode != "UC-1234" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestDeviceFlow_PollSucceeds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"authorization_code":"ac123","code_verifier":"cv","code_challenge":"cc"}`))
	}))
	defer ts.Close()
	svc := NewOAuthService(ts.Client())
	svc.deviceTokenURL = ts.URL + "/token"
	resp, err := svc.PollDeviceToken(context.Background(), "daid", "UC-1234", time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.AuthorizationCode != "ac123" {
		t.Errorf("auth code = %q", resp.AuthorizationCode)
	}
}

func TestExtractAccountIDFromPSD(t *testing.T) {
	tests := []struct {
		name string
		psd  string
		want string
	}{
		{
			name: "account_id field",
			psd:  `{"account_id":"acc-123"}`,
			want: "acc-123",
		},
		{
			name: "chatgpt_account_id field",
			psd:  `{"chatgpt_account_id":"acc-456"}`,
			want: "acc-456",
		},
		{
			name: "workspaceId field",
			psd:  `{"workspaceId":"ws-789"}`,
			want: "ws-789",
		},
		{
			name: "id_token JWT with chatgpt_account_id",
			psd:  fmt.Sprintf(`{"id_token":"%s"}`, testIDTokenWithAccountID("acc-jwt")),
			want: "acc-jwt",
		},
		{
			name: "empty JSON",
			psd:  `{}`,
			want: "",
		},
		{
			name: "account_id takes precedence over workspaceId",
			psd:  `{"account_id":"acc-xyz","workspaceId":"ws-abc"}`,
			want: "acc-xyz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractAccountIDFromPSD(tt.psd); got != tt.want {
				t.Errorf("extractAccountIDFromPSD(%q) = %q, want %q", tt.psd, got, tt.want)
			}
		})
	}
}

func testIDTokenWithAccountID(accountID string) string {
	claims, _ := json.Marshal(map[string]any{
		"sub": accountID,
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": accountID},
	})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	return "header." + payload + ".sig"
}

func TestDeviceFlow_PollExpires(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"authorization_pending"}`))
	}))
	defer ts.Close()
	svc := NewOAuthService(ts.Client())
	svc.deviceTokenURL = ts.URL + "/token"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := svc.PollDeviceToken(ctx, "daid", "UC-1234", time.Millisecond, time.Second)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if calls == 0 {
		t.Error("server was never called")
	}
}

