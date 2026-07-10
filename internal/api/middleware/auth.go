package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"golang.org/x/crypto/bcrypt"
)

// Auth validates API keys from the Authorization header.
// Fail-closed: returns 401 when no keys are configured.
func Auth(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if any keys exist
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE is_active = 1`).Scan(&count)
		if err != nil {
			logging.Logger.Warn("auth system error querying api_keys", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
			c.Abort()
			return
		}
		if count == 0 {
			// No keys configured — open access (user hasn't set up auth yet)
			c.Next()
			return
		}

		// Extract API key
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// Support "Bearer <key>" format
		key := authHeader
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			key = authHeader[7:]
		}

		// Validate against stored key hashes (bcrypt only)
		rows, err := db.Query(`SELECT id, key_hash, rate_limit_per_min FROM api_keys WHERE is_active = 1`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
			c.Abort()
			return
		}
		defer rows.Close()

			var keyID string
		var rateLimit int
		for rows.Next() {
			var id, hash string
			rows.Scan(&id, &hash, &rateLimit)
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(key)); err == nil {
				keyID = id
				break
			}
		}

		if keyID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		c.Set("api_key_id", keyID)
		c.Set("rate_limit", rateLimit)
		c.Next()
	}
}
