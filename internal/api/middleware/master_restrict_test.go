package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMasterRestrict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	okHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}

	fakeMasterAuth := func(masterHeader string) gin.HandlerFunc {
		return func(c *gin.Context) {
			if c.GetHeader("X-Master-Test") == masterHeader {
				c.Set("master_api_auth", true)
			}
			c.Next()
		}
	}

	setupEngine := func() *gin.Engine {
		e := gin.New()
		admin := e.Group("/admin/api/v1")
		admin.Use(fakeMasterAuth("yes"))
		admin.Use(MasterRestrict())
		{
			admin.POST("/change-password", okHandler)
			admin.POST("/restart", okHandler)
			admin.POST("/backup/download", okHandler)
			admin.GET("/developers/master-key", okHandler)
			admin.POST("/developers/master-key/regenerate", okHandler)
			admin.PUT("/tls-config", okHandler)
			admin.GET("/providers", okHandler)
			admin.GET("/providers/:id/connections", okHandler)
		}
		return e
	}

	cases := []struct {
		name        string
		master      bool
		method      string
		path        string
		wantStatus  int
		wantBlocked bool
	}{
		{"blocked change-password with master", true, http.MethodPost, "/admin/api/v1/change-password", http.StatusForbidden, true},
		{"allowed change-password without master", false, http.MethodPost, "/admin/api/v1/change-password", http.StatusOK, false},
		{"blocked restart with master", true, http.MethodPost, "/admin/api/v1/restart", http.StatusForbidden, true},
		{"blocked backup download with master", true, http.MethodPost, "/admin/api/v1/backup/download", http.StatusForbidden, true},
		{"blocked get master key with master", true, http.MethodGet, "/admin/api/v1/developers/master-key", http.StatusForbidden, true},
		{"blocked regenerate master key with master", true, http.MethodPost, "/admin/api/v1/developers/master-key/regenerate", http.StatusForbidden, true},
		{"blocked tls config update with master", true, http.MethodPut, "/admin/api/v1/tls-config", http.StatusForbidden, true},
		{"allowed providers list with master", true, http.MethodGet, "/admin/api/v1/providers", http.StatusOK, false},
		{"allowed connections route with master", true, http.MethodGet, "/admin/api/v1/providers/openai/connections", http.StatusOK, false},
		{"allowed providers list without master", false, http.MethodGet, "/admin/api/v1/providers", http.StatusOK, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := setupEngine()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.master {
				req.Header.Set("X-Master-Test", "yes")
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: got status %d, want %d", tc.name, rec.Code, tc.wantStatus)
			}
			if tc.wantBlocked {
				wantBody := `{"error":{"code":"master_action_forbidden","message":"action not allowed via master api key"}}`
				if rec.Body.String() != wantBody {
					t.Fatalf("%s: got body %q, want %q", tc.name, rec.Body.String(), wantBody)
				}
			}
		})
	}
}
