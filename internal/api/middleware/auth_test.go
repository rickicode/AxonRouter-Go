package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
			router.Use(Auth(database))
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
	router.Use(Auth(database))
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
