package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

const (
	bedrockDefaultRegion = "us-west-2"
	bedrockBaseTemplate  = "https://bedrock-mantle.{region}.api.aws/v1"
)

// BedrockExecutor routes OpenAI-compatible requests to Amazon Bedrock Mantle.
// It resolves the region-aware upstream URL and strips the gateway "bedrock/"
// prefix from model IDs so the upstream endpoint receives bare Bedrock model IDs.
type BedrockExecutor struct {
	*OpenAIExecutor
}

// NewBedrockExecutor creates a new Bedrock Mantle executor.
func NewBedrockExecutor(base *BaseExecutor) *BedrockExecutor {
	return &BedrockExecutor{OpenAIExecutor: NewOpenAIExecutor(base)}
}

// Execute performs a non-streaming chat completion through Bedrock Mantle.
func (e *BedrockExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.Execute(ctx, modified)
}

// ExecuteStream performs a streaming chat completion through Bedrock Mantle.
func (e *BedrockExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.ExecuteStream(ctx, modified)
}

// Models returns available Bedrock Mantle models from the configured endpoint.
func (e *BedrockExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.Models(ctx, modified)
}

// Embeddings performs an embedding request through Bedrock Mantle.
func (e *BedrockExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.Embeddings(ctx, modified)
}

// Images performs an image generation request through Bedrock Mantle.
func (e *BedrockExecutor) Images(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.Images(ctx, modified)
}

// prepareRequest resolves the region-aware base URL and applies the provider's
// compatibility config (notably the model prefix strip).
func (e *BedrockExecutor) prepareRequest(req *Request) (*Request, error) {
	baseURL := bedrockBaseURL(req.BaseURL, req.ProviderSpecificData)
	if strings.Contains(baseURL, "{") {
		return nil, fmt.Errorf("bedrock: unresolved base_url placeholders in %q; set region in provider-specific data", baseURL)
	}

	provider := req.Provider
	if provider == "" {
		provider = "bedrock"
	}
	c := providercfg.CompatibilityFor(provider)
	body := sanitizeRequestWithCompatibility(req.Body, c)

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if tools, ok := parsed["tools"].([]any); ok && len(tools) > 0 {
			parsed["tools"] = normalizeBedrockTools(tools)
			if out, err := json.Marshal(parsed); err == nil {
				body = out
			}
		}
	}

	modified := *req
	modified.Provider = provider
	modified.BaseURL = baseURL
	modified.Body = body
	return &modified, nil
}

// bedrockBaseURL resolves the upstream base URL for Bedrock Mantle.
// Region defaults to us-west-2 and can be overridden via provider_specific_data["region"].
// If a custom base_url is provided without a {region} placeholder, it is used as-is.
func bedrockBaseURL(baseURL string, psd map[string]string) string {
	if baseURL == "" {
		baseURL = bedrockBaseTemplate
	}
	region := bedrockDefaultRegion
	if psd != nil && psd["region"] != "" {
		region = psd["region"]
	}
	if strings.Contains(baseURL, "{region}") {
		baseURL = strings.ReplaceAll(baseURL, "{region}", region)
	}
	return strings.TrimRight(baseURL, "/")
}

var unsupportedBedrockSchemaKeywords = []string{
	"additionalProperties",
	"anyOf",
	"oneOf",
	"allOf",
	"not",
	"$schema",
	"$id",
	"$ref",
	"$defs",
	"definitions",
}

// normalizeBedrockToolSchema removes JSON Schema keywords unsupported by the
// Bedrock Converse tool interface and ensures every object schema has a
// consistent `type` and `properties` block. Nested schemas inside properties and
// array items are normalized recursively.
func normalizeBedrockToolSchema(schema map[string]any) map[string]any {
	if schema == nil {
		schema = map[string]any{}
	}

	isObjectSchema := false
	if typ, ok := schema["type"].(string); ok && typ == "object" {
		isObjectSchema = true
	} else if _, ok := schema["properties"]; ok {
		isObjectSchema = true
	} else if typ == "" {
		isObjectSchema = true
	}

	out := make(map[string]any, len(schema))
	for k, v := range schema {
		if isUnsupportedBedrockKeyword(k) {
			continue
		}
		out[k] = v
	}

	if isObjectSchema {
		out["type"] = "object"
		props, _ := out["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			out["properties"] = props
		}

		for pk, pv := range props {
			if child, ok := pv.(map[string]any); ok {
				props[pk] = normalizeBedrockToolSchema(child)
			}
		}

		if req, ok := out["required"].([]any); ok {
			filtered := make([]any, 0, len(req))
			for _, r := range req {
				if name, ok := r.(string); ok && name != "" {
					if _, exists := props[name]; exists {
						filtered = append(filtered, name)
					}
				}
			}
			if len(filtered) > 0 {
				out["required"] = filtered
			} else {
				delete(out, "required")
			}
		}
	}

	if items, ok := out["items"].(map[string]any); ok {
		out["items"] = normalizeBedrockToolSchema(items)
	}

	return out
}

func isUnsupportedBedrockKeyword(k string) bool {
	for _, kw := range unsupportedBedrockSchemaKeywords {
		if k == kw {
			return true
		}
	}
	return false
}

// normalizeBedrockTools normalizes tool schemas inside an OpenAI-compatible
// `tools` array. Each tool's `function.parameters` is passed through
// normalizeBedrockToolSchema; tools without parameters get an empty object
// schema so they are not dropped.
func normalizeBedrockTools(tools []any) []any {
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := toolMap["function"].(map[string]any)
		if !ok {
			continue
		}
		if params, ok := fn["parameters"].(map[string]any); ok {
			fn["parameters"] = normalizeBedrockToolSchema(params)
		} else {
			fn["parameters"] = normalizeBedrockToolSchema(nil)
		}
	}
	return tools
}
