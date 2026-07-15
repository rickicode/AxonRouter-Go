package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	copilotDefaultAuthURL = "https://api.github.com/copilot_internal/v2/token"
	copilotDefaultAPIBase = "https://api.githubcopilot.com"

	copilotUserAgent     = "GitHubCopilotChat/0.26.7"
	copilotEditorVersion = "vscode/1.105.1"
	copilotPluginVersion = "copilot-chat/0.26.7"
	copilotIntegrationID = "vscode-chat"
	copilotOpenAIIntent  = "conversation-edits"

	copilotTokenSkew = 2 * time.Minute
)

// package-level cache for the local Copilot OAuth token read from disk, so
// fallback file reads don't happen on every request with an empty API key.
var copilotOAuthCache = struct {
	mu     sync.RWMutex
	token  string
	loaded bool
}{}

func resetCopilotOAuthTokenCache() {
	copilotOAuthCache.mu.Lock()
	copilotOAuthCache.loaded = false
	copilotOAuthCache.token = ""
	copilotOAuthCache.mu.Unlock()
}

// copilotToken is the short-lived bearer token returned by GitHub's Copilot
// token exchange endpoint.
type copilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// CopilotExecutor routes chat completion requests to GitHub Copilot. It handles
// the OAuth-token → Copilot-token exchange, token caching, and the extra
// headers Copilot's OpenAI-compatible proxy expects.
type CopilotExecutor struct {
	*OpenAIExecutor
	mu     sync.RWMutex
	tokens map[string]*copilotToken
}

// NewCopilotExecutor creates a new GitHub Copilot executor.
func NewCopilotExecutor(base *BaseExecutor) *CopilotExecutor {
	return &CopilotExecutor{
		OpenAIExecutor: NewOpenAIExecutor(base),
		tokens:         make(map[string]*copilotToken),
	}
}

// Execute performs a non-streaming chat completion via GitHub Copilot.
func (e *CopilotExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	body := JSONSet(req.Body, "stream", false)
	body = stripCopilotModelPrefix(body)

	url, err := openAIEndpoint(copilotAPIBase(token), "chat/completions", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, body)
	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
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

// ExecuteStream performs a streaming chat completion via GitHub Copilot.
func (e *CopilotExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	body := JSONSet(req.Body, "stream", true)
	body = stripCopilotModelPrefix(body)

	url, err := openAIEndpoint(copilotAPIBase(token), "chat/completions", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, body)
	return e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, req.StreamConfig)
}

// Embeddings is not supported by GitHub Copilot.
func (e *CopilotExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: embeddings endpoint not supported")
}

// Images is not supported by GitHub Copilot.
func (e *CopilotExecutor) Images(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: images endpoint not supported")
}

// Responses is not supported by GitHub Copilot.
func (e *CopilotExecutor) Responses(ctx context.Context, req *Request) (*Response, error) {
	return nil, errors.New("github copilot: responses endpoint not supported")
}

// ResponsesStream is not supported by GitHub Copilot.
func (e *CopilotExecutor) ResponsesStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, errors.New("github copilot: responses endpoint not supported")
}

// Models fetches the available Copilot models from the active endpoint.
func (e *CopilotExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	token, err := e.ensureToken(req)
	if err != nil {
		return nil, err
	}

	url, err := openAIEndpoint(copilotAPIBase(token), "models", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := e.copilotHeaders(token.Token, nil)
	resp, err := e.DoRequest(ctx, "GET", url, headers, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("copilot models error %d: %s", resp.StatusCode, string(resp.Body))
	}
	return resp, nil
}

// ensureToken returns a cached Copilot token or fetches a new one using the
// request's API key (treated as a GitHub OAuth token). If no API key is
// supplied, it falls back to reading the local Copilot hosts/apps.json file.
func (e *CopilotExecutor) ensureToken(req *Request) (*copilotToken, error) {
	oauth := req.APIKey
	if oauth == "" {
		oauth = loadCopilotOAuthToken()
	}
	if oauth == "" {
		return nil, errors.New("github copilot: missing OAuth token; add it as the connection API key or sign in via a Copilot-enabled editor first")
	}

	e.mu.RLock()
	tok := e.tokens[oauth]
	e.mu.RUnlock()

	now := time.Now().Unix()
	if tok != nil && tok.ExpiresAt > now+int64(copilotTokenSkew.Seconds()) {
		return tok, nil
	}

	return e.fetchToken(oauth)
}

// fetchToken calls GitHub's Copilot token exchange endpoint.
func (e *CopilotExecutor) fetchToken(oauthToken string) (*copilotToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotDefaultAuthURL, nil)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "token "+oauthToken)
	hreq.Header.Set("Accept", "application/json")

	client := http.DefaultClient
	if e.Client != nil {
		client = e.Client
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token exchange returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tok copilotToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("copilot token exchange parse error: %w", err)
	}
	if tok.ExpiresAt == 0 {
		tok.ExpiresAt = time.Now().Unix() + 3600
	}

	e.mu.Lock()
	e.tokens[oauthToken] = &tok
	e.mu.Unlock()

	return &tok, nil
}

// copilotHeaders builds the request headers Copilot's OpenAI-compatible proxy
// expects.
func (e *CopilotExecutor) copilotHeaders(token string, body []byte) map[string]string {
	initiator := "user"
	if len(body) > 0 {
		msgs := gjson.GetBytes(body, "messages")
		if msgs.IsArray() {
			arr := msgs.Array()
			if len(arr) > 0 {
				if r := arr[len(arr)-1].Get("role").String(); r == "assistant" {
					initiator = "agent"
				}
			}
		}
	}

	return map[string]string{
		"Content-Type":           "application/json",
		"Authorization":          "Bearer " + token,
		"User-Agent":             copilotUserAgent,
		"Editor-Version":         copilotEditorVersion,
		"Editor-Plugin-Version":  copilotPluginVersion,
		"Copilot-Integration-Id": copilotIntegrationID,
		"Openai-Intent":          copilotOpenAIIntent,
		"X-Initiator":            initiator,
	}
}

// copilotAPIBase returns the upstream API host from the token, or the public
// GitHub Copilot default if missing.
func copilotAPIBase(tok *copilotToken) string {
	if tok != nil && tok.Endpoints.API != "" {
		return tok.Endpoints.API
	}
	return copilotDefaultAPIBase
}

// stripCopilotModelPrefix removes the "copilot/" prefix from model IDs so the
// upstream Copilot proxy receives bare model names like "gpt-4o".
func stripCopilotModelPrefix(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		return body
	}
	clean := strings.TrimPrefix(model, "copilot/")
	if clean == model {
		return body
	}
	out, err := sjson.SetBytes(body, "model", clean)
	if err != nil {
		log.Printf("WARN: failed to rewrite copilot model id: %v", err)
		return body
	}
	return out
}

// loadCopilotOAuthToken tries to read the locally stored GitHub OAuth token
// from the files written by the official Copilot editor extensions.
func loadCopilotOAuthToken() string {
	copilotOAuthCache.mu.RLock()
	if copilotOAuthCache.loaded {
		tok := copilotOAuthCache.token
		copilotOAuthCache.mu.RUnlock()
		return tok
	}
	copilotOAuthCache.mu.RUnlock()

	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		if runtime.GOOS == "windows" {
			localAppData := os.Getenv("LOCALAPPDATA")
			if localAppData == "" {
				configDir = filepath.Join(home, ".config")
			} else {
				configDir = localAppData
			}
		} else {
			configDir = filepath.Join(home, ".config")
		}
	}

	tok := ""
	dir := filepath.Join(configDir, "github-copilot")
	for _, name := range []string{"hosts.json", "apps.json"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		tok = extractGitHubOAuthToken(data)
		if tok != "" {
			break
		}
	}

	copilotOAuthCache.mu.Lock()
	copilotOAuthCache.token = tok
	copilotOAuthCache.loaded = true
	copilotOAuthCache.mu.Unlock()
	return tok
}

// extractGitHubOAuthToken walks the "github.com" entry in hosts.json or
// apps.json and returns the oauth_token value.
func extractGitHubOAuthToken(data []byte) string {
	for _, key := range []string{`github\.com`, `github\.localhost`} {
		oauth := gjson.GetBytes(data, key+".oauth_token")
		if oauth.Exists() && oauth.String() != "" {
			return oauth.String()
		}
	}
	return ""
}
