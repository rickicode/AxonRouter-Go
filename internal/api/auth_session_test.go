package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const testAdminPassword = "testpass1234"

func newAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	InitAuth(database)
	return database
}

func seedAdminPassword(t *testing.T, database *sql.DB, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, "admin_password_hash", string(hash), time.Now().Unix()); err != nil {
		t.Fatalf("seed password hash: %v", err)
	}
}

func loginRequest(t *testing.T, database *sql.DB, password string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"password": password})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	LoginHandler(database)(c)
	return w
}

func authHeaderToken(t *testing.T, database *sql.DB) string {
	t.Helper()
	w := loginRequest(t, database, testAdminPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token              string `json:"token"`
		MustChangePassword bool   `json:"mustChangePassword"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	return resp.Token
}

func TestSessionAuth_DoesNotBlockAPIsOnFirstLogin(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('app_name','x',0) ON CONFLICT(key) DO NOTHING`); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	token := authHeaderToken(t, database)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/settings/app_name", nil)
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected password-change warning not to block APIs, got %d %s", w.Code, w.Body.String())
	}
}

func TestSessionAuth_AllowsPasswordEndpoints_FirstLogin(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")
	token := authHeaderToken(t, database)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{
		"old_password":     testAdminPassword,
		"new_password":     "newsecret123",
		"confirm_password": "newsecret123",
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/change-password", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	if c.IsAborted() {
		t.Fatal("change-password should be allowed before password change")
	}
}

func TestLogin_ReturnsMustChangePassword_WhenDefaultPassword(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, defaultAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")

	w := loginRequest(t, database, defaultAdminPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("expected mustChangePassword=true, got %s", w.Body.String())
	}
}

func TestLogin_MustChangePasswordFalse_AfterChange(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")
	token := authHeaderToken(t, database)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{
		"old_password":     testAdminPassword,
		"new_password":     "newsecret123",
		"confirm_password": "newsecret123",
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/change-password", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	if c.IsAborted() {
		t.Fatal("auth aborted unexpectedly")
	}
	ChangePasswordHandler(database)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("change password failed: %d %s", w.Code, w.Body.String())
	}

	w = loginRequest(t, database, "newsecret123")
	if w.Code != http.StatusOK {
		t.Fatalf("re-login failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mustChangePassword":false`) {
		t.Fatalf("expected mustChangePassword=false, got %s", w.Body.String())
	}
}

func TestChangePassword_RequiresOldPassword(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")
	token := authHeaderToken(t, database)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{
		"old_password":     "wrongpassword",
		"new_password":     "newsecret123",
		"confirm_password": "newsecret123",
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/change-password", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	ChangePasswordHandler(database)(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong old password, got %d %s", w.Code, w.Body.String())
	}
}

func TestChangePassword_RequiresMatchingConfirmation(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	token := authHeaderToken(t, database)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{
		"old_password":     testAdminPassword,
		"new_password":     "newsecret123",
		"confirm_password": "different",
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/change-password", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	ChangePasswordHandler(database)(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched confirmation, got %d %s", w.Code, w.Body.String())
	}
}

func TestDeferPasswordChange_WorksOnFirstLogin(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "true")
	token := authHeaderToken(t, database)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/defer-password-change", nil)
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	DeferPasswordChangeHandler(database)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("defer on first login failed: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/providers", nil)
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected protected endpoint to be allowed after defer, got %d %s", w.Code, w.Body.String())
	}

	w = loginRequest(t, database, testAdminPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("login after defer failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mustChangePassword":false`) {
		t.Fatalf("expected mustChangePassword=false after defer, got %s", w.Body.String())
	}
}

func TestDeferPasswordChange_AndReappearance(t *testing.T) {
	database := newAuthTestDB(t)
	seedAdminPassword(t, database, testAdminPassword)
	_ = setSetting(database, firstLoginKey, "false")
	token := authHeaderToken(t, database)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/defer-password-change", nil)
	c.Request.Header.Set("X-Auth-Token", token)
	SessionAuth(database)(c)
	DeferPasswordChangeHandler(database)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("defer failed: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		PasswordChangeDueAt int64 `json:"password_change_due_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal defer response: %v", err)
	}
	wantMin := time.Now().Unix() + 23*3600
	wantMax := time.Now().Unix() + 25*3600
	if resp.PasswordChangeDueAt < wantMin || resp.PasswordChangeDueAt > wantMax {
		t.Fatalf("due at %d outside expected 24h window", resp.PasswordChangeDueAt)
	}

	w = loginRequest(t, database, testAdminPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("login after defer failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mustChangePassword":false`) {
		t.Fatalf("expected mustChangePassword=false while deferred, got %s", w.Body.String())
	}
}
