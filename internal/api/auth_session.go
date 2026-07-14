package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionTTL        = 72 * time.Hour
	minPasswordLength = 8
)

const defaultAdminPassword = "12345677"

const (
	firstLoginKey          = "first_login"
	passwordChangedKey     = "admin_password_changed"
	passwordChangeDueAtKey = "password_change_due_at"
)

var jwtSecret []byte

// getSetting reads a setting value from the injected database, returning "" if
// the row is missing or the query fails. Operates on the passed connection so
// it works in tests where the package-global DB is not initialized.
func getSetting(database *sql.DB, key string) string {
	var value string
	err := database.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil || value == "" {
		return ""
	}
	return value
}

// setSetting persists a setting value, creating or replacing the row.
func setSetting(database *sql.DB, key, value string) error {
	_, err := database.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().Unix(),
	)
	return err
}

// InitAuth bootstraps the JWT secret (persisted in settings) and seeds the
// default admin password hash. Call once during router construction, after the
// database is open and settings defaults are seeded.
func InitAuth(database *sql.DB) {
	secret := getSetting(database, "jwt_secret")
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err == nil {
			secret = hex.EncodeToString(b)
			_ = setSetting(database, "jwt_secret", secret)
		}
	}
	jwtSecret = []byte(secret)

	hash := getSetting(database, "admin_password_hash")
	if hash == "" {
		h, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
		if err == nil {
			_ = setSetting(database, "admin_password_hash", string(h))
		}
	}

	if getSetting(database, firstLoginKey) == "" {
		_ = setSetting(database, firstLoginKey, "true")
	}
}

func settingAsInt64(database *sql.DB, key string) int64 {
	s := getSetting(database, key)
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func mustChangePassword(database *sql.DB) bool {
	hash := getSetting(database, "admin_password_hash")
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(defaultAdminPassword)) == nil
}

// issueToken mints a fresh HS256 JWT with sub=admin and exp=now+sessionTTL.
func issueToken() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   "admin",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(sessionTTL)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(jwtSecret)
}

// LoginHandler validates the admin password and returns a JWT. The token is
// returned both as a response header (X-Auth-Token) and in the JSON body.
func LoginHandler(database *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
			return
		}

		hash := getSetting(database, "admin_password_hash")
		if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
			return
		}

		tok, err := issueToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
			return
		}
		c.Header("X-Auth-Token", tok)
		c.JSON(http.StatusOK, gin.H{
			"token":              tok,
			"mustChangePassword": mustChangePassword(database),
		})
	}
}

type changePasswordRequest struct {
	OldPassword     string `json:"old_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

// ChangePasswordHandler updates the admin password after verifying the old one.
func ChangePasswordHandler(database *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req changePasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if req.NewPassword != req.ConfirmPassword {
			c.JSON(http.StatusBadRequest, gin.H{"error": "new passwords do not match"})
			return
		}
		if len(req.NewPassword) < minPasswordLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
			return
		}

		hash := getSetting(database, "admin_password_hash")
		if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)) != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "incorrect current password"})
			return
		}

		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}

		now := time.Now().Unix()
		_ = setSetting(database, "admin_password_hash", string(newHash))
		_ = setSetting(database, firstLoginKey, "false")
		_ = setSetting(database, passwordChangedKey, "true")
		_ = setSetting(database, passwordChangeDueAtKey, "")
		_ = setSetting(database, "admin_password_updated_at", strconv.FormatInt(now, 10))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// DeferPasswordChangeHandler allows an authenticated admin to postpone the
// forced password change for 24 hours. Marking the initial login as done lets
// the deferred window take effect even on a brand-new install.
func DeferPasswordChangeHandler(database *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		dueAt := time.Now().Unix() + 24*3600
		if err := setSetting(database, passwordChangeDueAtKey, strconv.FormatInt(dueAt, 10)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to defer password change"})
			return
		}
		if err := setSetting(database, firstLoginKey, "false"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to defer password change"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"password_change_due_at": dueAt})
	}
}

// SessionAuth enforces a valid admin JWT on /api/admin routes and slides the
// session: each request re-issues a token with a fresh exp=now+72h.
func SessionAuth(database *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Auth-Token")
		if token == "" {
			auth := c.GetHeader("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				token = auth[7:]
			}
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		claims := &jwt.RegisteredClaims{}
		parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !parsed.Valid || claims.Subject != "admin" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		if tok, err := issueToken(); err == nil {
			c.Header("X-Auth-Token", tok)
		}
		c.Next()
	}
}
