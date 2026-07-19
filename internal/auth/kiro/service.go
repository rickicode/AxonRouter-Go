package kiro

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

const (
	authServiceBase    = "https://prod.us-east-1.auth.desktop.kiro.dev"
	defaultRegion      = "us-east-1"
	clientName         = "kiro-oauth-client"
	clientType         = "public"
	builderIDStartURL  = "https://view.awsapps.com/start"
	builderIDIssuerURL = "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6"
	pollInterval       = 5 * time.Second
	pollTimeout        = 5 * time.Minute
	socialRedirectURI  = "kiro://kiro.kiroAgent/authenticate-success"
)

var (
	scopes = []string{
		"codewhisperer:completions",
		"codewhisperer:analysis",
		"codewhisperer:conversations",
	}
	grantTypes = []string{
		"urn:ietf:params:oauth:grant-type:device_code",
		"refresh_token",
	}
)

// DeviceCodeResult holds device code metadata for the handler to read.
type DeviceCodeResult struct {
	VerificationURI         string
	VerificationURIComplete string
	UserCode                string
}

// ExternalIDPRequest holds imported External IdP credentials.
type ExternalIDPRequest struct {
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	TokenEndpoint string `json:"token_endpoint"`
	IssuerURL     string `json:"issuer_url"`
	ClientID      string `json:"client_id"`
	Scope         string `json:"scope"`
	ProfileArn    string `json:"profile_arn"`
	Region        string `json:"region"`
}

// KiroAuthService dispatches all Kiro authentication methods.
type KiroAuthService struct {
	httpClient     *http.Client
	pending        sync.Map // state -> *DeviceCodeResult
	mu             sync.Mutex
	socialSessions map[string]*socialSession
}

type socialSession struct {
	provider string
	verifier string
}

// NewAuthService creates a multi-method Kiro auth service.
func NewAuthService(httpClient *http.Client) *KiroAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &KiroAuthService{
		httpClient:     httpClient,
		socialSessions: make(map[string]*socialSession),
	}
}

// StartLocalServer begins the default AWS Builder ID device-code flow.
// It satisfies the auth.OAuthService interface used by the generic admin OAuth handler.
func (s *KiroAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return s.StartDeviceFlow(ctx, state, defaultRegion, builderIDStartURL, builderIDIssuerURL, "builder-id")
}

// GenerateAuthURL returns the verification URI stored by StartLocalServer.
// Prefer verification_uri_complete so the browser can pre-fill the user code.
func (s *KiroAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	bare := bareState(state)
	val, ok := s.pending.Load(bare)
	if !ok {
		return "", fmt.Errorf("no pending device code for state")
	}
	res := val.(*DeviceCodeResult)
	if res.VerificationURIComplete != "" {
		return res.VerificationURIComplete, nil
	}
	return res.VerificationURI, nil
}

// GetUserCode returns the user code for the dashboard.
func (s *KiroAuthService) GetUserCode(state string) string {
	bare := bareState(state)
	val, ok := s.pending.LoadAndDelete(bare)
	if !ok {
		return ""
	}
	return val.(*DeviceCodeResult).UserCode
}

// ExchangeCode is not used by Kiro's device-code flow.
func (s *KiroAuthService) ExchangeCode(_ context.Context, _ string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("kiro uses method-specific auth flows; use the /oauth/kiro endpoints")
}

// RefreshToken refreshes credentials using the method stored in ProviderSpecific["authMethod"].
func (s *KiroAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	am := strings.ToLower(strings.TrimSpace(creds.ProviderSpecific["authMethod"]))
	switch am {
	case "external_idp":
		return s.refreshExternalIDPFromCreds(ctx, creds)
	case "google", "github", "import":
		return s.refreshSocialToken(ctx, creds)
	case "builder-id", "idc":
		return s.refreshOIDCWithRetry(ctx, creds)
	case "api_key":
		return nil, fmt.Errorf("api_key credentials cannot be refreshed")
	default:
		if s.hasExternalIDPData(creds) {
			return s.refreshExternalIDPFromCreds(ctx, creds)
		}
		if creds.RefreshToken != "" {
			return s.refreshSocialToken(ctx, creds)
		}
		if s.hasOIDCData(creds) {
			return s.refreshOIDCWithRetry(ctx, creds)
		}
		return nil, fmt.Errorf("no refresh strategy available for Kiro credentials")
	}
}

// StartSocial begins a Google/GitHub social login flow and returns the URL the user must visit.
func (s *KiroAuthService) StartSocial(provider string) (string, string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "google" && provider != "github" {
		return "", "", "", fmt.Errorf("unsupported social provider: %s", provider)
	}
	verifier := generateCodeVerifier()
	challenge := codeChallengeS256(verifier)
	sessionID := generateKiroState()

	s.mu.Lock()
	s.socialSessions[sessionID] = &socialSession{provider: provider, verifier: verifier}
	s.mu.Unlock()

	authURL := buildSocialLoginURL(provider, challenge, sessionID)
	return authURL, sessionID, verifier, nil
}

// ExchangeSocialCode exchanges an authorization code from a social login for tokens.
func (s *KiroAuthService) ExchangeSocialCode(ctx context.Context, sessionID, code string) (*auth.Credentials, error) {
	s.mu.Lock()
	sess := s.socialSessions[sessionID]
	delete(s.socialSessions, sessionID)
	s.mu.Unlock()
	if sess == nil {
		return nil, fmt.Errorf("invalid or expired social session")
	}
	if code == "" {
		return nil, fmt.Errorf("authorization code is required")
	}

	accessToken, refreshToken, profileArn, expiresIn, err := exchangeSocialCode(ctx, s.httpClient, code, sess.verifier)
	if err != nil {
		return nil, err
	}

	creds := &auth.Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		ProviderSpecific: map[string]string{
			"authMethod":  sess.provider,
			"profileArn":  profileArn,
			"redirectUri": socialRedirectURI,
		},
	}
	if email := extractEmailFromJWT(accessToken); email != "" {
		creds.Email = email
	}
	return creds, nil
}

// ImportToken validates and imports a manually supplied AWS refresh token.
func (s *KiroAuthService) ImportToken(_ context.Context, refreshToken string) (*auth.Credentials, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}
	if !isAWSRefreshToken(refreshToken) {
		return nil, fmt.Errorf("invalid token format: AWS refresh tokens start with aorAAAAAG...")
	}
	return &auth.Credentials{
		RefreshToken: refreshToken,
		ProviderSpecific: map[string]string{
			"authMethod": "import",
		},
	}, nil
}

// ValidateAPIKey validates a long-lived Kiro API key by listing available profiles.
func (s *KiroAuthService) ValidateAPIKey(ctx context.Context, apiKey, region string) (*auth.Credentials, error) {
	return validateAPIKey(ctx, s.httpClient, apiKey, region)
}

// ImportExternalIDP imports enterprise SSO credentials with an SSRF-guarded token endpoint.
func (s *KiroAuthService) ImportExternalIDP(_ context.Context, req ExternalIDPRequest) (*auth.Credentials, error) {
	region := strings.TrimSpace(req.Region)
	if region == "" {
		region = defaultRegion
	}
	tokenEndpoint, err := validateExternalIdpTokenEndpoint(req.TokenEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid external IdP token endpoint: %w", err)
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		return nil, fmt.Errorf("client_id is required for external_idp")
	}
	scope := normalizeScope(req.Scope)
	if scope == "" {
		return nil, fmt.Errorf("scope is required for external_idp")
	}

	email := ""
	if req.AccessToken != "" {
		email = emailFromExternalIdpToken(req.AccessToken)
	}

	psd := map[string]string{
		"authMethod":    "external_idp",
		"clientId":      clientID,
		"tokenEndpoint": tokenEndpoint,
		"scope":         scope,
		"issuerUrl":     strings.TrimSpace(req.IssuerURL),
		"profileArn":    strings.TrimSpace(req.ProfileArn),
		"region":        region,
		"tokenType":     "EXTERNAL_IDP",
	}
	return &auth.Credentials{
		AccessToken:      req.AccessToken,
		RefreshToken:     req.RefreshToken,
		Email:            email,
		ProviderSpecific: psd,
	}, nil
}

// --- small helpers ---

func bareState(state string) string {
	if idx := strings.LastIndex(state, ":"); idx > 0 {
		return state[:idx]
	}
	return state
}

func generateKiroState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateCodeVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func codeChallengeS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// flexibleToken tries both camelCase and snake_case fields returned by Kiro/AWS OIDC endpoints.
type flexibleToken struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ProfileArn   string
	ExpiresIn    int
}

func parseFlexibleToken(body []byte) flexibleToken {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	t := flexibleToken{}
	t.AccessToken = firstString(m, "accessToken", "access_token")
	t.RefreshToken = firstString(m, "refreshToken", "refresh_token")
	t.IDToken = firstString(m, "idToken", "id_token")
	t.ProfileArn = firstString(m, "profileArn", "profile_arn", "arn")
	if v, ok := getAny(m, "expiresIn", "expires_in", "expires_in").(float64); ok {
		t.ExpiresIn = int(v)
	}
	return t
}

func firstString(m map[string]any, keys ...string) string {
	v := getAny(m, keys...)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func getAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func extractEmailFromJWT(accessToken string) string {
	claims := decodeJWTPayload(accessToken)
	if claims == nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "upn", "sub"} {
		if v, ok := claims[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (s *KiroAuthService) hasExternalIDPData(creds *auth.Credentials) bool {
	psd := creds.ProviderSpecific
	return psd != nil && psd["tokenEndpoint"] != "" && psd["clientId"] != "" && strings.EqualFold(psd["authMethod"], "external_idp")
}

func (s *KiroAuthService) hasOIDCData(creds *auth.Credentials) bool {
	psd := creds.ProviderSpecific
	return psd != nil && psd["clientId"] != "" && psd["clientSecret"] != ""
}

func (s *KiroAuthService) refreshExternalIDPFromCreds(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	access, refresh, expiresAt, err := refreshExternalIdpToken(ctx, s.httpClient, creds.RefreshToken, creds.ProviderSpecific)
	if err != nil {
		return nil, err
	}
	newCreds := *creds
	newCreds.AccessToken = access
	if refresh != "" {
		newCreds.RefreshToken = refresh
	}
	if !expiresAt.IsZero() {
		newCreds.ExpiresAt = expiresAt
	}
	return &newCreds, nil
}

func (s *KiroAuthService) refreshSocialToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	return refreshSocial(ctx, s.httpClient, creds)
}

// StartDeviceFlow begins an AWS SSO OIDC device-code flow for builder-id or IDC.
func (s *KiroAuthService) StartDeviceFlow(ctx context.Context, state, region, startURL, issuerURL, authMethod string) (int, chan *auth.Credentials, error) {
	if region == "" {
		region = defaultRegion
	}
	if startURL == "" {
		startURL = builderIDStartURL
	}
	if issuerURL == "" {
		issuerURL = builderIDIssuerURL
	}

	clientID, clientSecret, err := s.registerClient(ctx, region, issuerURL)
	if err != nil {
		return 0, nil, fmt.Errorf("register client: %w", err)
	}

	verificationURI, verificationURIComplete, userCode, deviceCode, err := s.startDeviceAuthorization(ctx, region, clientID, clientSecret, startURL)
	if err != nil {
		return 0, nil, fmt.Errorf("start device auth: %w", err)
	}

	bare := bareState(state)
	s.pending.Store(bare, &DeviceCodeResult{
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURIComplete,
		UserCode:                userCode,
	})

	resultChan := make(chan *auth.Credentials, 1)
	go func() {
		defer close(resultChan)
		creds, err := s.pollDeviceTokenLoop(ctx, region, clientID, clientSecret, deviceCode, authMethod, startURL)
		if err != nil {
			resultChan <- &auth.Credentials{
				ProviderSpecific: map[string]string{"__oauth_error__": err.Error()},
			}
			return
		}
		resultChan <- creds
	}()

	return 0, resultChan, nil
}

func (s *KiroAuthService) registerClient(ctx context.Context, region, issuerURL string) (string, string, error) {
	reqBody := map[string]any{
		"clientName": clientName,
		"clientType": clientType,
		"scopes":     scopes,
		"grantTypes": grantTypes,
		"issuerUrl":  issuerURL,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	endpoint := fmt.Sprintf("https://oidc.%s.amazonaws.com/client/register", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("register failed %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", err
	}
	if result.ClientID == "" {
		return "", "", fmt.Errorf("empty clientId")
	}
	return result.ClientID, result.ClientSecret, nil
}

func (s *KiroAuthService) startDeviceAuthorization(ctx context.Context, region, clientID, clientSecret, startURL string) (string, string, string, string, error) {
	reqBody := map[string]any{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"startUrl":     startURL,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	endpoint := fmt.Sprintf("https://oidc.%s.amazonaws.com/device_authorization", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", "", fmt.Errorf("device auth failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		DeviceCode              string `json:"deviceCode"`
		UserCode                string `json:"userCode"`
		VerificationURI         string `json:"verificationUri"`
		VerificationURIComplete string `json:"verificationUriComplete"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", "", err
	}
	if result.VerificationURI == "" || result.DeviceCode == "" {
		return "", "", "", "", fmt.Errorf("missing verification_uri or device_code")
	}
	return result.VerificationURI, result.VerificationURIComplete, result.UserCode, result.DeviceCode, nil
}

func (s *KiroAuthService) pollDeviceTokenLoop(ctx context.Context, region, clientID, clientSecret, deviceCode, authMethod, startURL string) (*auth.Credentials, error) {
	deadline := time.Now().Add(pollTimeout)
	interval := pollInterval

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		creds, pending, err := s.pollDeviceToken(ctx, region, clientID, clientSecret, deviceCode)
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "slow_down") {
				interval += time.Second
				continue
			}
			if strings.Contains(msg, "authorization_pending") {
				continue
			}
			return nil, err
		}
		if pending {
			continue
		}
		if creds != nil {
			if creds.ProviderSpecific == nil {
				creds.ProviderSpecific = map[string]string{}
			}
			creds.ProviderSpecific["authMethod"] = authMethod
			creds.ProviderSpecific["clientId"] = clientID
			creds.ProviderSpecific["clientSecret"] = clientSecret
			creds.ProviderSpecific["region"] = region
			creds.ProviderSpecific["startUrl"] = startURL
			return creds, nil
		}
	}
	return nil, fmt.Errorf("device authentication timed out")
}

func (s *KiroAuthService) pollDeviceToken(ctx context.Context, region, clientID, clientSecret, deviceCode string) (*auth.Credentials, bool, error) {
	reqBody := map[string]any{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"deviceCode":   deviceCode,
		"grantType":    "urn:ietf:params:oauth:grant-type:device_code",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	endpoint := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errResp)
		if errResp.Error != "" {
			msg := strings.ToLower(errResp.Error)
			if strings.Contains(msg, "authorization_pending") || strings.Contains(msg, "slow_down") {
				return nil, true, nil
			}
			return nil, false, fmt.Errorf("%s", errResp.Error)
		}
		return nil, false, fmt.Errorf("authorization pending")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("poll failed %d: %s", resp.StatusCode, string(respBody))
	}

	tok := parseFlexibleToken(respBody)
	if tok.AccessToken == "" {
		return nil, false, fmt.Errorf("empty access token")
	}
	expiresAt := time.Time{}
	if tok.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	creds := &auth.Credentials{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
		ExpiresAt:    expiresAt,
		ProviderSpecific: map[string]string{
			"profileArn": strings.TrimSpace(tok.ProfileArn),
		},
	}
	if email := extractEmailFromJWT(tok.AccessToken); email != "" {
		creds.Email = email
	} else if email := extractEmailFromJWT(tok.IDToken); email != "" {
		creds.Email = email
	}
	return creds, false, nil
}

func (s *KiroAuthService) refreshOIDCWithRetry(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	psd := creds.ProviderSpecific
	region := strings.TrimSpace(psd["region"])
	if region == "" {
		region = defaultRegion
	}
	clientID := psd["clientId"]
	clientSecret := psd["clientSecret"]

	newCreds, err := s.refreshOIDC(ctx, region, clientID, clientSecret, creds)
	if err != nil && psd["startUrl"] != "" {
		var regErr error
		clientID, clientSecret, regErr = s.registerClient(ctx, region, builderIDIssuerURL)
		if regErr == nil {
			newCreds, err = s.refreshOIDC(ctx, region, clientID, clientSecret, creds)
		}
	}
	if err != nil {
		return nil, err
	}
	if newCreds.ProviderSpecific == nil {
		newCreds.ProviderSpecific = map[string]string{}
	}
	newCreds.ProviderSpecific["clientId"] = clientID
	newCreds.ProviderSpecific["clientSecret"] = clientSecret
	return newCreds, nil
}

func (s *KiroAuthService) refreshOIDC(ctx context.Context, region, clientID, clientSecret string, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	endpoint := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
	body := map[string]any{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": creds.RefreshToken,
		"grantType":    "refresh_token",
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc refresh failed %d: %s", resp.StatusCode, string(respBody))
	}

	tok := parseFlexibleToken(respBody)
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	newCreds := *creds
	newCreds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		newCreds.RefreshToken = tok.RefreshToken
	}
	if tok.IDToken != "" {
		newCreds.IDToken = tok.IDToken
	}
	if tok.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	if email := extractEmailFromJWT(tok.AccessToken); email != "" {
		newCreds.Email = email
	} else if email := extractEmailFromJWT(tok.IDToken); email != "" {
		newCreds.Email = email
	}
	return &newCreds, nil
}
