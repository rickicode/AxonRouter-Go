package smart

import (
	"fmt"

	"github.com/tidwall/sjson"
)

// RewriteVirtualModel rewrites a request whose model is a smart virtual model.
// It returns the concrete model id, the updated body, and an error if routing fails.
// If router is nil the original model and body are returned unchanged.
func RewriteVirtualModel(model string, body []byte, router *Router) (string, []byte, error) {
	if !IsVirtualModel(model) {
		return model, body, nil
	}
	if router == nil {
		return model, body, nil
	}
	res, err := router.Resolve(model, body)
	if err != nil {
		return "", nil, fmt.Errorf("smart routing failed for %s: %w", model, err)
	}
	concrete := res.Provider + "/" + res.ModelID
	updated := EnsureBodyModel(body, concrete)
	return concrete, updated, nil
}

// EnsureBodyModel updates the JSON body model field to concreteModel.
func EnsureBodyModel(body []byte, concreteModel string) []byte {
	if len(body) == 0 {
		return body
	}
	updated, err := sjson.SetBytes(body, "model", concreteModel)
	if err != nil {
		return body
	}
	return updated
}
