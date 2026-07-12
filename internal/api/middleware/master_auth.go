package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/adminapi"
)

// MasterAuth validates the master admin API key for programmatic endpoints.
func MasterAuth(km *adminapi.KeyManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := km.Current()
		if expected == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid master api key", "code": "invalid_master_key"}})
			c.Abort()
			return
		}

		header := c.GetHeader("Authorization")
		presented := header
		if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
			presented = header[7:]
		}

		if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid master api key", "code": "invalid_master_key"}})
			c.Abort()
			return
		}

		c.Set("master_api_auth", true)
		c.Next()
	}
}
