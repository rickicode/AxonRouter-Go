package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// restrictedMasterActions lists admin routes that should not be callable via
// the programmatic master API key, even though they are part of the shared
// admin route table. This keeps the master key useful for automation while
// preventing break-glass / security-recovery actions if the key is leaked.
//
// Keys are stored as "METHOD:path" where path is the route's FullPath
// (including the /admin/api/v1 prefix).
var restrictedMasterActions = map[string]struct{}{
	"POST:/admin/api/v1/change-password":                    {},
	"POST:/admin/api/v1/defer-password-change":              {},
	"GET:/admin/api/v1/developers/master-key":               {},
	"POST:/admin/api/v1/developers/master-key/regenerate":   {},
	"POST:/admin/api/v1/upgrade":                            {},
	"POST:/admin/api/v1/restart":                            {},
	"POST:/admin/api/v1/backup/download":                    {},
	"POST:/admin/api/v1/backup/restore":                     {},
	"PUT:/admin/api/v1/tls-config":                          {},
}

// MasterRestrict blocks break-glass admin actions when the request is
// authenticated with the master API key. Session-authenticated dashboard
// requests are not affected.
func MasterRestrict() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMasterAuth(c) {
			c.Next()
			return
		}

		key := c.Request.Method + ":" + c.FullPath()
		if _, blocked := restrictedMasterActions[key]; blocked {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"message": "action not allowed via master api key",
					"code":    "master_action_forbidden",
				},
			})
			return
		}

		c.Next()
	}
}

func isMasterAuth(c *gin.Context) bool {
	v, exists := c.Get("master_api_auth")
	if !exists {
		return false
	}
	ok, _ := v.(bool)
	return ok
}
