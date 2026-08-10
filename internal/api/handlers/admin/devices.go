package admin

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// DevicesHandler returns the distinct devices tracked for each API key.
type DevicesHandler struct {
	db      *sql.DB
	tracker *usage.DeviceTracker
}

// NewDevicesHandler creates a handler that reads device state from the tracker
// and key metadata from the DB.
func NewDevicesHandler(db *sql.DB, tracker *usage.DeviceTracker) *DevicesHandler {
	return &DevicesHandler{db: db, tracker: tracker}
}

// deviceView is the JSON shape exposed for a single tracked device.
type deviceView struct {
	Fingerprint string `json:"fingerprint"`
	IP          string `json:"ip"`
	UserAgent   string `json:"userAgent"`
	LastSeen    int64  `json:"lastSeen"`
}

// GetDevices returns the key name and masked device list for the given API key.
func (h *DevicesHandler) GetDevices(c *gin.Context) {
	id := c.Param("id")

	var name string
	err := h.db.QueryRow(`SELECT COALESCE(name, '') FROM api_keys WHERE id = ?`, id).Scan(&name)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	details := h.tracker.GetDeviceDetails(id)
	devices := make([]deviceView, 0, len(details))
	for _, d := range details {
		devices = append(devices, deviceView{
			Fingerprint: d.Fingerprint,
			IP:          d.IP,
			UserAgent:   d.UserAgent,
			LastSeen:    d.LastSeen.Unix(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"keyId":   id,
		"name":    name,
		"count":   len(devices),
		"devices": devices,
	})
}
