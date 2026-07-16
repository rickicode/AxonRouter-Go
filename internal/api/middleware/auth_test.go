package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "auth-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestAuth_RequiresBcryptHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openTestDB(t)
	now := time.Now().Unix()

	key := "my-secret-key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 10, ?)`,
		"auth-key-1", "test", string(hash), now,
	); err != nil {
		t.Fatalf("insert hashed key: %v", err)
	}

	// Insert a raw (non-bcrypt) key that must NOT be accepted.
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 10, ?)`,
		"auth-key-2", "raw", "plaintext-key", now,
	); err != nil {
		t.Fatalf("insert raw key: %v", err)
	}

	tests := []struct {
		name       string
		key        string
		wantStatus int
	}{
		{"bcrypt key", key, http.StatusOK},
		{"raw key", "plaintext-key", http.StatusUnauthorized},
		{"missing key", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Auth(database, nil))
			router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.key != "" {
				req.Header.Set("Authorization", "Bearer "+tt.key)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestAuth_SetsRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openTestDB(t)

	key := "rate-limited-key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 42, ?)`,
		"auth-rl-key", "test", string(hash), time.Now().Unix(),
	); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	router := gin.New()
	router.Use(Auth(database, nil))
	var got int
	router.GET("/test", func(c *gin.Context) {
		if v, ok := c.Get("rate_limit"); ok {
			got = v.(int)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got != 42 {
		t.Errorf("rate_limit = %d, want 42", got)
	}
}

func TestAuth_EmptyKeyTable_FailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openTestDB(t)

	router := gin.New()
	router.Use(Auth(database, nil))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	t.Run("no key configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid key", func(t *testing.T) {
		key := "configured-key"
		hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
		if _, err := database.Exec(
			`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 10, ?)`,
			"auth-key", "test", string(hash), time.Now().Unix(),
		); err != nil {
			t.Fatalf("insert key: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestAuthCache_Validate_StoresResult(t *testing.T) {
	database := openTestDB(t)

	key := "cached-key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, max_tokens, created_at) VALUES (?, ?, ?, 1, 10, 100, ?)`,
		"cached-key-id", "test", string(hash), time.Now().Unix(),
	); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	cache := NewAuthCache(30 * time.Second)
	keyID, rateLimit, maxTokens, ok, expired, dbErr := cache.Validate(database, key)
	if dbErr != nil {
		t.Fatalf("Validate returned DB error: %v", dbErr)
	}
	if expired {
		t.Fatalf("Validate returned expired for a non-expiring key")
	}
	if !ok {
		t.Fatalf("Validate returned !ok")
	}
	if keyID != "cached-key-id" || rateLimit != 10 || maxTokens != 100 {
		t.Fatalf("unexpected result: %s, %d, %d", keyID, rateLimit, maxTokens)
	}

	r := cache.Get(key)
	if r == nil {
		t.Fatalf("Validate did not store the result in the cache")
	}
	if r.keyID != keyID || r.rateLimit != rateLimit || r.maxTokens != maxTokens {
		t.Errorf("cached result mismatch: got %s/%d/%d", r.keyID, r.rateLimit, r.maxTokens)
	}
}

func TestAuth_ExpiredKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := openTestDB(t)

	key := "expired-key"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	now := time.Now().Unix()
	expiresAt := now + 1
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at, expires_at) VALUES (?, ?, ?, 1, 10, ?, ?)`,
		"expired-key-id", "test", string(hash), now, expiresAt,
	); err != nil {
		t.Fatalf("insert expiring key: %v", err)
	}

	cache := NewAuthCache(30 * time.Second)
	router := gin.New()
	router.Use(Auth(database, cache))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// First request: key is valid, should be cached.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusOK)
	}

	// Wait for the key to expire, then hit the cache.
	time.Sleep(1100 * time.Millisecond)

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := w.Body.String()
	if !strings.Contains(body, "api key expired") {
		t.Errorf("body = %q, want to contain 'api key expired'", body)
	}

	// is_active must remain unchanged (do not deactivate on expiration check).
	var isActive int
	if err := database.QueryRow(`SELECT is_active FROM api_keys WHERE id = ?`, "expired-key-id").Scan(&isActive); err != nil {
		t.Fatalf("select is_active: %v", err)
	}
	if isActive != 1 {
		t.Errorf("is_active = %d, want 1 (must not be changed on expiration)", isActive)
	}
}

func TestValidateKey_ReturnsDBError(t *testing.T) {
	database := openTestDB(t)
	database.Close()

	_, _, _, _, _, ok, dbErr := validateKey(database, "any-key")
	if dbErr == nil {
		t.Fatalf("expected DB error from validateKey on closed DB")
	}
	if ok {
		t.Fatalf("expected ok=false when DB errors")
	}
}
