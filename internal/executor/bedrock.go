package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

const (
	bedrockDefaultRegion = "us-west-2"
	bedrockBaseTemplate = "https://bedrock-mantle.{region}.api.aws/v1"
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
