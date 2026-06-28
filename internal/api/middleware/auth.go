package middleware

import (
	"crypto/subtle"
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
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
			log.Printf("WARN: auth system error querying api_keys: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
			c.Abort()
			return
		}
		if count == 0 {
			// No keys configured — fail closed
			log.Printf("WARN: no active API keys configured — denying request (fail-closed)")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no API keys configured"})
			c.Abort()
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

		// Validate against stored key hashes
		// ponytail: simple hash comparison — upgrade to bcrypt for production
		rows, err := db.Query(`SELECT key_hash, rate_limit_per_min FROM api_keys WHERE is_active = 1`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
			c.Abort()
			return
		}
		defer rows.Close()

		valid := false
		var rateLimit int
		for rows.Next() {
			var hash string
			rows.Scan(&hash, &rateLimit)
			// Try bcrypt first (hashed keys)
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(key)); err == nil {
				valid = true
				break
			}
			// ponytail: backward compat — raw key comparison during migration window
			if subtle.ConstantTimeCompare([]byte(key), []byte(hash)) == 1 {
				valid = true
				break
			}
		}

		if !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		c.Set("rate_limit", rateLimit)
		c.Next()
	}
}
