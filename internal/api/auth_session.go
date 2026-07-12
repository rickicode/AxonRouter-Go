package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 72 * time.Hour
const defaultAdminPassword = "12345677"

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
		c.JSON(http.StatusOK, gin.H{"token": tok})
	}
}

// SessionAuth enforces a valid admin JWT on /api/admin routes and slides the
// session: each request re-issues a token with a fresh exp=now+72h.
func SessionAuth() gin.HandlerFunc {
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
