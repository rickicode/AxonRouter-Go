package executor

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	vertexDefaultBaseURL  = "https://aiplatform.googleapis.com/v1/projects/{projectId}/locations/{location}/endpoints/openapi"
	vertexTokenScope      = "https://www.googleapis.com/auth/cloud-platform"
	vertexTokenGrantType  = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	vertexTokenSkew       = 2 * time.Minute
)

// vertexServiceAccount is the subset of a GCP service-account JSON key we need.
type vertexServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

// vertexAccessToken caches a Google access token for a service account.
type vertexAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

// VertexExecutor routes OpenAI-compatible requests to Google Vertex AI
// (Gemini) using a service account JSON key for authentication.
type VertexExecutor struct {
	*OpenAIExecutor
	mu     sync.RWMutex
	tokens map[string]*vertexAccessToken
}

// NewVertexExecutor creates a new Vertex AI executor.
func NewVertexExecutor(base *BaseExecutor) *VertexExecutor {
	return &VertexExecutor{
		OpenAIExecutor: NewOpenAIExecutor(base),
		tokens:         make(map[string]*vertexAccessToken),
	}
}

// Execute performs a non-streaming chat completion through Vertex AI.
func (e *VertexExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := e.OpenAIExecutor.Execute(ctx, modified)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
	}
	return resp, nil
}

// ExecuteStream performs a streaming chat completion through Vertex AI.
func (e *VertexExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.ExecuteStream(ctx, modified)
}

// Models returns the available Vertex AI models from the configured endpoint.
func (e *VertexExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	modified, err := e.prepareRequest(req)
	if err != nil {
		return nil, err
	}
	return e.OpenAIExecutor.Models(ctx, modified)
}

// prepareRequest builds a request with a fresh access token, resolved base URL,
// and the provider prefix stripped from the model id.
func (e *VertexExecutor) prepareRequest(req *Request) (*Request, error) {
	saJSON := req.APIKey
	if saJSON == "" {
		return nil, errors.New("vertex ai: missing service account JSON; paste the contents of the service-account key file into the API key / credential field")
	}

	var sa vertexServiceAccount
	if err := json.Unmarshal([]byte(saJSON), &sa); err != nil {
		return nil, fmt.Errorf("vertex ai: invalid service account JSON: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" || sa.TokenURI == "" {
		return nil, errors.New("vertex ai: service account JSON must contain client_email, private_key, and token_uri")
	}

	tok, err := e.accessToken(saJSON, sa)
	if err != nil {
		return nil, err
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = vertexDefaultBaseURL
	}
	baseURL = resolveVertexBaseURL(baseURL, sa.ProjectID, req.ProviderSpecificData)
	if strings.Contains(baseURL, "{") {
		return nil, fmt.Errorf("vertex ai: unresolved base_url placeholders in %q; set projectId (and optionally location) in provider-specific data", baseURL)
	}

	body := stripVertexModelPrefix(req.Body)

	modified := *req
	modified.BaseURL = baseURL
	modified.APIKey = tok.Token
	modified.Body = body
	return &modified, nil
}

// accessToken returns a cached Google access token, refreshing it when expired.
func (e *VertexExecutor) accessToken(saJSON string, sa vertexServiceAccount) (*vertexAccessToken, error) {
	e.mu.RLock()
	cached := e.tokens[saJSON]
	e.mu.RUnlock()

	if cached != nil && time.Until(cached.ExpiresAt) > vertexTokenSkew {
		return cached, nil
	}

	key, err := parseVertexPrivateKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("vertex ai: failed to parse service account private key: %w", err)
	}

	jwt, err := buildVertexJWT(key, sa.ClientEmail, sa.TokenURI)
	if err != nil {
		return nil, fmt.Errorf("vertex ai: failed to build JWT: %w", err)
	}

	token, expiresIn, err := exchangeVertexJWT(ctxOrBackground(), sa.TokenURI, jwt)
	if err != nil {
		return nil, fmt.Errorf("vertex ai: token exchange failed: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	ee := &vertexAccessToken{Token: token, ExpiresAt: expiresAt}
	e.mu.Lock()
	e.tokens[saJSON] = ee
	e.mu.Unlock()
	return ee, nil
}

func ctxOrBackground() context.Context {
	return context.Background()
}

// parseVertexPrivateKey parses a service-account PEM private key (PKCS#1 or PKCS#8).
func parseVertexPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(pemStr, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
}

// buildVertexJWT creates a signed JWT for the Google OAuth service-account grant.
func buildVertexJWT(key *rsa.PrivateKey, clientEmail, tokenURI string) (string, error) {
	now := time.Now().Unix()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   clientEmail,
		"scope": vertexTokenScope,
		"aud":   tokenURI,
		"iat":   now,
		"exp":   now + 3600,
	}

	headerB, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsB, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerB) + "." + base64.RawURLEncoding.EncodeToString(claimsB)
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// exchangeVertexJWT trades a signed JWT for an access token.
func exchangeVertexJWT(ctx context.Context, tokenURI, jwt string) (token string, expiresIn int, err error) {
	body := "grant_type=" + vertexTokenGrantType + "&assertion=" + jwt
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return "", 0, err
	}
	if envelope.AccessToken == "" {
		return "", 0, errors.New("empty access_token in response")
	}
	return envelope.AccessToken, envelope.ExpiresIn, nil
}

// resolveVertexBaseURL substitutes {projectId} and {location} placeholders using
// the service account JSON and provider-specific data.
func resolveVertexBaseURL(baseURL, projectID string, psd map[string]string) string {
	if baseURL == "" {
		baseURL = vertexDefaultBaseURL
	}
	if projectID != "" {
		baseURL = strings.ReplaceAll(baseURL, "{projectId}", projectID)
	}
	if psd != nil {
		if v := psd["projectId"]; v != "" {
			baseURL = strings.ReplaceAll(baseURL, "{projectId}", v)
		}
		if v := psd["location"]; v != "" {
			baseURL = strings.ReplaceAll(baseURL, "{location}", v)
		}
	}
	baseURL = strings.ReplaceAll(baseURL, "{location}", "global")
	return baseURL
}

// stripVertexModelPrefix removes the "vertex/" prefix from model IDs so the
// upstream Vertex OpenAI-compatible endpoint receives bare model names.
func stripVertexModelPrefix(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		return body
	}
	clean := strings.TrimPrefix(model, "vertex/")
	if clean == model {
		return body
	}
	out, err := sjson.SetBytes(body, "model", clean)
	if err != nil {
		log.Printf("WARN: failed to rewrite vertex model id: %v", err)
		return body
	}
	return out
}
