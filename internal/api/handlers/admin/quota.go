package admin

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// QuotaHandler handles quota-related API endpoints.
type QuotaHandler struct {
	db *sql.DB
}

// NewQuotaHandler creates a new quota handler.
func NewQuotaHandler(database *sql.DB) *QuotaHandler {
	return &QuotaHandler{db: database}
}

// List returns quota data for all OAuth providers.
// GET /api/admin/quota
func (h *QuotaHandler) List(c *gin.Context) {
	data, err := quota.FetchAllQuota(h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

// Refresh fetches fresh quota for a single connection.
// POST /api/admin/quota/:connId/refresh
func (h *QuotaHandler) Refresh(c *gin.Context) {
	connID := c.Param("connId")
	if connID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection id required"})
		return
	}

	data, err := quota.FetchConnectionQuota(h.db, connID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, data)
}
