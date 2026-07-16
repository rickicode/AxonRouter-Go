package executor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

const (
	mimocodeChatSuffix       = "/chat"
	mimocodeBootstrapSuffix  = "/api/free-ai/bootstrap"
	mimocodeSource           = "mimocode-cli-free"
	mimocodeSystemMarker     = "You are MiMoCode, an interactive CLI tool that helps users with software engineering tasks."
	mimocodeJWTBuffer        = 5 * time.Minute
	mimocodeBootstrapTimeout = 15 * time.Second
)

var mimocodeUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
}

// bootstrapURLFromBase derives the MiMoCode bootstrap URL.
// For the canonical base path (.../api/free-ai/openai) it returns .../api/free-ai/bootstrap.
// For a shorter base ending in .../openai it appends the full bootstrap path.
func bootstrapURLFromBase(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/openai") {
		base = strings.TrimSuffix(base, "/openai")
	}
	if strings.HasSuffix(base, "/api/free-ai") {
		return base + "/bootstrap"
	}
	return base + mimocodeBootstrapSuffix
}

// chatURLFromBase derives the MiMoCode chat URL.
func chatURLFromBase(base string) string {
	return strings.TrimRight(base, "/") + mimocodeChatSuffix
}

// MimocodeExecutor routes free MiMoCode requests through per-fingerprint JWT bootstrap.
type MimocodeExecutor struct {
	*BaseExecutor
	mu     sync.Mutex
	tokens map[string]*mimocodeToken
}

type mimocodeToken struct {
	token     string
	expiresAt time.Time
}

// NewMimocodeExecutor creates a new MiMoCode executor.
func NewMimocodeExecutor(base *BaseExecutor) *MimocodeExecutor {
	return &MimocodeExecutor{
		BaseExecutor: base,
		tokens:       make(map[string]*mimocodeToken),
	}
}

// Execute performs a non-streaming chat completion.
func (e *MimocodeExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	baseURL, fingerprint := e.resolveBaseAndFingerprint(req)
	token, err := e.accessToken(ctx, baseURL, fingerprint)
	if err != nil {
		return nil, err
	}

	body := e.prepareBody(req)
	body = JSONSet(body, "stream", false)

	url := chatURLFromBase(baseURL)
	headers := e.buildHeaders(false)
	headers["Authorization"] = "Bearer " + token

	resp, err := e.DoRequest(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		e.invalidate(fingerprint)
		token, err = e.accessToken(ctx, baseURL, fingerprint)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + token
		resp, err = e.DoRequest(ctx, http.MethodPost, url, headers, body)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
	}
	return resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *MimocodeExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	baseURL, fingerprint := e.resolveBaseAndFingerprint(req)
	token, err := e.accessToken(ctx, baseURL, fingerprint)
	if err != nil {
		return nil, err
	}

	body := e.prepareBody(req)
	body = JSONSet(body, "stream", true)

	url := chatURLFromBase(baseURL)
	headers := e.buildHeaders(true)
	headers["Authorization"] = "Bearer " + token

	result, err := e.DoStreamRequestWithConfig(ContextWithProvider(ctx, req.Provider), http.MethodPost, url, headers, body, req.StreamConfig)
	var ue *UpstreamError
	if errors.As(err, &ue) && (ue.StatusCode == http.StatusUnauthorized || ue.StatusCode == http.StatusForbidden) {
		e.invalidate(fingerprint)
		token, err = e.accessToken(ctx, baseURL, fingerprint)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + token
		result, err = e.DoStreamRequestWithConfig(ContextWithProvider(ctx, req.Provider), http.MethodPost, url, headers, body, req.StreamConfig)
	}
	return result, err
}

func (e *MimocodeExecutor) resolveBaseAndFingerprint(req *Request) (baseURL, fingerprint string) {
	baseURL = req.BaseURL
	if baseURL == "" {
		baseURL = "https://api.xiaomimimo.com/api/free-ai/openai"
	}
	fingerprint = mimocodeFingerprint(req.ProviderSpecificData)
	return baseURL, fingerprint
}

func (e *MimocodeExecutor) prepareBody(req *Request) []byte {
	body := req.Body
	body = JSONSet(body, "model", "mimo-auto")
	body = injectMimocodeMarker(body)
	return body
}

func (e *MimocodeExecutor) buildHeaders(stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"X-Mimo-Source": mimocodeSource,
		"User-Agent":    mimocodeUserAgents[time.Now().UnixNano()%int64(len(mimocodeUserAgents))],
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	return headers
}

func (e *MimocodeExecutor) accessToken(ctx context.Context, baseURL, fingerprint string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cached := e.tokens[fingerprint]; cached != nil && time.Until(cached.expiresAt) > mimocodeJWTBuffer {
		return cached.token, nil
	}

	token, expiresAt, err := e.bootstrap(ctx, baseURL, fingerprint)
	if err != nil {
		return "", err
	}
	e.tokens[fingerprint] = &mimocodeToken{token: token, expiresAt: expiresAt}
	return token, nil
}

func (e *MimocodeExecutor) invalidate(fingerprint string) {
	e.mu.Lock()
	delete(e.tokens, fingerprint)
	e.mu.Unlock()
}

func (e *MimocodeExecutor) bootstrap(ctx context.Context, baseURL, fingerprint string) (string, time.Time, error) {
	url := bootstrapURLFromBase(baseURL)
	payload := map[string]string{"client": fingerprint}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(ctx, mimocodeBootstrapTimeout)
	defer cancel()

	headers := map[string]string{"Content-Type": "application/json"}
	resp, err := e.DoRequest(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mimocode bootstrap: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("mimocode bootstrap failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var env struct {
		JWT string `json:"jwt"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil || env.JWT == "" {
		return "", time.Time{}, fmt.Errorf("mimocode bootstrap response missing jwt")
	}
	return env.JWT, mimocodeJWTExpiresAt(env.JWT), nil
}

func mimocodeJWTExpiresAt(jwt string) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return time.Now().Add(50 * time.Minute)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(50 * time.Minute)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Now().Add(50 * time.Minute)
	}
	return time.Unix(claims.Exp, 0)
}

func mimocodeFingerprint(data map[string]string) string {
	if fp := data["fingerprint"]; fp != "" {
		return fp
	}
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:])
}

func injectMimocodeMarker(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	for _, m := range messages.Array() {
		if m.Get("role").String() != "system" {
			continue
		}
		content := m.Get("content")
		if content.Type == gjson.String && strings.Contains(content.String(), mimocodeSystemMarker) {
			return body
		}
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}
	msgs, ok := req["messages"].([]any)
	if !ok {
		return body
	}
	req["messages"] = append([]any{map[string]any{"role": "system", "content": mimocodeSystemMarker}}, msgs...)
	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}
