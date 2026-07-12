package admin

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/adminapi"
)

// DevelopersHandler returns master-key metadata for the Developers dashboard page
// and the programmatic admin API.
type DevelopersHandler struct {
	db   *sql.DB
	km   *adminapi.KeyManager
	port string
}

// NewDevelopersHandler creates a master-key metadata handler.
func NewDevelopersHandler(db *sql.DB, km *adminapi.KeyManager, port string) *DevelopersHandler {
	return &DevelopersHandler{db: db, km: km, port: port}
}

// GetMasterKey returns the current master key, its prefix, the admin base URL,
// and when the key was created.
func (h *DevelopersHandler) GetMasterKey(c *gin.Context) {
	key := h.km.Current()
	prefix := ""
	if len(key) >= 12 {
		prefix = key[:12]
	}
	createdAt := h.keyUpdatedAt()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"key":        key,
		"prefix":     prefix,
		"base_url":   "http://localhost:" + h.port + "/admin/api/v1",
		"created_at": createdAt,
	}})
}

// RegenerateMasterKey rotates the master key and returns the new metadata.
func (h *DevelopersHandler) RegenerateMasterKey(c *gin.Context) {
	if _, err := h.km.Regenerate(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": "master_key_regenerate_failed"}})
		return
	}
	h.GetMasterKey(c)
}

func (h *DevelopersHandler) keyUpdatedAt() int64 {
	var updatedAt int64
	if err := h.db.QueryRow(`SELECT updated_at FROM settings WHERE key = ?`, "admin_api_key").Scan(&updatedAt); err != nil {
		return 0
	}
	return updatedAt
}
