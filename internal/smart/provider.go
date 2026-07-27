package smart

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// VirtualModelHandler exposes CRUD/lifecycle endpoints for virtual models.
type VirtualModelHandler struct {
	registry *Registry
}

// NewVirtualModelHandler creates a new admin handler.
func NewVirtualModelHandler(registry *Registry) *VirtualModelHandler {
	return &VirtualModelHandler{registry: registry}
}

// List returns all configured virtual models.
func (h *VirtualModelHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.registry.List()})
}

// Get returns a single virtual model.
func (h *VirtualModelHandler) Get(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	vm, ok := h.registry.Get(VirtualModelID(id))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "virtual model not found"})
		return
	}
	c.JSON(http.StatusOK, vm)
}

// Update updates candidates and enable toggle for a virtual model.
func (h *VirtualModelHandler) Update(c *gin.Context) {
	id := VirtualModelID(c.Param("id"))
	existing, ok := h.registry.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "virtual model not found"})
		return
	}
	var req struct {
		Name       string   `json:"name"`
		Enabled    *bool    `json:"enabled"`
		Candidates []string `json:"candidates"`
		Strategy   string   `json:"strategy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	update := &VirtualModel{
		ID:         existing.ID,
		Name:       existing.Name,
		Enabled:    existing.Enabled,
		Candidates: existing.Candidates,
		Strategy:   existing.Strategy,
		CreatedAt:  existing.CreatedAt,
		UpdatedAt:  db.UnixNow(),
	}
	if req.Name != "" {
		update.Name = req.Name
	}
	if req.Enabled != nil {
		update.Enabled = *req.Enabled
	}
	if req.Candidates != nil {
		update.Candidates = req.Candidates
	}
	if req.Strategy != "" {
		update.Strategy = Strategy(req.Strategy)
	}
	if _, err := h.registry.Upsert(update); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, update)
}
