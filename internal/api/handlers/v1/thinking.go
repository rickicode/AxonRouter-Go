package v1

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/thinking"
)

// thinkingBudgetKey is the context key used to propagate a parsed thinking
// budget from the endpoint that reads the request body down to the executor.
type thinkingBudgetKey struct{}

// parseThinkingSuffixFromBody strips any thinking-budget suffix from the model
// name in the request body, stores the normalized budget on the request context,
// and returns the updated body and base model name.
func (h *Handler) parseThinkingSuffixFromBody(c *gin.Context, body []byte) ([]byte, string, bool) {
	model := executor.JSONGet(body, "model")
	base, raw, ok := thinking.ParseThinkingSuffix(model)
	if !ok {
		return body, model, false
	}

	budget, ok := thinking.BudgetFromString(raw)
	if !ok {
		return body, model, false
	}

	// Provider/model resolution later in the request path uses the stripped name.
	body = executor.JSONSet(body, "model", base)
	// Keep the budget available for executeProviderCall and direct-mode handlers.
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), thinkingBudgetKey{}, budget))
	return body, base, true
}

// applyThinkingOverrideFromContext reads a parsed thinking budget from ctx (if
// present) and injects it into the provider-format request body.
func (h *Handler) applyThinkingOverrideFromContext(ctx context.Context, body []byte, providerFormat string) []byte {
	budget, ok := ctx.Value(thinkingBudgetKey{}).(int)
	if !ok {
		return body
	}
	return thinking.ApplyThinkingOverride(body, budget, providerFormat)
}
