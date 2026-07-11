package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// ModelPricingHandler serves the model_pricing table — the single source of
// truth for per-model cost rates used by EstimateCost and combo scoring.
type ModelPricingHandler struct{}

// NewModelPricingHandler creates a new model pricing handler.
func NewModelPricingHandler() *ModelPricingHandler {
	return &ModelPricingHandler{}
}

// List returns all model pricing rows.
func (h *ModelPricingHandler) List(c *gin.Context) {
	rows := usage.ListPricing()
	if rows == nil {
		rows = []usage.ModelPricingRow{}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// Create inserts or replaces a model pricing row.
func (h *ModelPricingHandler) Create(c *gin.Context) {
	var req usage.ModelPricingRow
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id is required"})
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	req.UpdatedAt = time.Now().Unix()
	if err := usage.UpsertPricing(req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, req)
}

// Update patches a model pricing row (keyed by :id).
func (h *ModelPricingHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req usage.ModelPricingRow
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ModelID = id
	if req.Currency == "" {
		req.Currency = "USD"
	}
	req.UpdatedAt = time.Now().Unix()
	if err := usage.UpsertPricing(req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, req)
}

// Delete removes a model pricing row.
func (h *ModelPricingHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := usage.DeletePricing(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
