package smart

import (
	"github.com/gin-gonic/gin"
)

// RegisterAdminRoutes wires virtual model CRUD endpoints under /api/admin.
func RegisterAdminRoutes(g *gin.RouterGroup, registry *Registry) {
	h := NewVirtualModelHandler(registry)
	g.GET("/virtual-models", h.List)
	g.GET("/virtual-models/:id", h.Get)
	g.PATCH("/virtual-models/:id", h.Update)
}
