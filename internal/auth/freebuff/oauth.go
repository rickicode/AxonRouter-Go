// Package freebuff implements the Freebuff device-code login flow.
package freebuff

import (
	"context"
	"crypto/rand"
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
	DeviceCodeURL = "https://freebuff.com/api/auth/cli/code"
	StatusURL     = "https://freebuff.com/api/auth/cli/status"
	UserAgent     = "codebuff-cli/0.0.138"
	pollInterval  = 2 * time.Second
	pollTimeout   = 5 * time.Minute
)

type OAuthService struct {
	httpClient *http.Client
	codeURL    string
	statusURL  string
	mu         sync.Mutex
	states     map[string]*flowState
}

type flowState struct {
	loginURL string
}

func NewOAuthService(client *http.Client) *OAuthService {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{
		httpClient: client,
		codeURL:    DeviceCodeURL,
		statusURL:  StatusURL,
		states:     make(map[string]*flowState),
	}
}

func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fingerprintID, err := randomID()
	if err != nil {
		return 0, nil, fmt.Errorf("freebuff: generate fingerprint: %w", err)
	}
	code, err := s.requestDeviceCode(ctx, fingerprintID)
	if err != nil {
		return 0, nil, err
	}

	stateKey := stateParam(state)
	s.mu.Lock()
	s.states[stateKey] = &flowState{loginURL: code.LoginURL}
	s.mu.Unlock()

	result := make(chan *auth.Credentials, 1)
	go func() {
		creds, pollErr := s.poll(ctx, code)
		s.mu.Lock()
		delete(s.states, stateKey)
		s.mu.Unlock()
		if pollErr != nil {
			result <- &auth.Credentials{ProviderSpecific: map[string]string{"__oauth_error__": pollErr.Error()}}
			return
		}
		// Persist the fingerprint so the executor can reuse it as the stable
		// codebuff_metadata.client_id. Mirrors the reference: the server echoes
		// the fingerprint it chose (data.fingerprintId || ours) — prefer the
		// echoed one and fall back to the locally generated id.
		if creds.ProviderSpecific == nil {
			creds.ProviderSpecific = map[string]string{}
		}
		if creds.ProviderSpecific["fingerprintId"] == "" {
			creds.ProviderSpecific["fingerprintId"] = fingerprintID
		}
		result <- creds
	}()
	return 0, result, nil
}

func (s *OAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.states[stateParam(state)]
	if flow == nil || strings.TrimSpace(flow.loginURL) == "" {
		return "", fmt.Errorf("freebuff: no pending device flow")
	}
	return flow.loginURL, nil
}

func (s *OAuthService) ExchangeCode(context.Context, string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("freebuff uses device-code flow; authorization-code exchange is not supported")
}

func (s *OAuthService) RefreshToken(context.Context, *auth.Credentials) (*auth.Credentials, error) {
	return nil, fmt.Errorf("freebuff access tokens do not expose a refresh-token flow; reconnect the account")
}

func (s *OAuthService) requestDeviceCode(ctx context.Context, fingerprintID string) (*deviceCodeResponse, error) {
	payload, _ := json.Marshal(map[string]string{"fingerprintId": fingerprintID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.codeURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("freebuff device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("freebuff device code request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("freebuff device code response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("freebuff device code request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out deviceCodeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("freebuff device code response: %w", err)
	}
	if out.FingerprintID == "" {
		out.FingerprintID = fingerprintID
	}
	if out.LoginURL == "" || out.FingerprintHash == "" {
		return nil, fmt.Errorf("freebuff device code response missing loginUrl or fingerprintHash")
	}
	return &out, nil
}

type deviceCodeResponse struct {
	FingerprintID   string `json:"fingerprintId"`
	FingerprintHash string `json:"fingerprintHash"`
	LoginURL        string `json:"loginUrl"`
	ExpiresAt       string `json:"expiresAt"`
}

type statusResponse struct {
	User *struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		AuthToken     string `json:"authToken"`
		FingerprintID string `json:"fingerprintId"`
	} `json:"user"`
	Error string `json:"error"`
}

func (s *OAuthService) poll(ctx context.Context, code *deviceCodeResponse) (*auth.Credentials, error) {
	deadline := time.Now().Add(pollTimeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("freebuff device code expired or timed out")
		}
		creds, pending, err := s.pollOnce(ctx, code)
		if err != nil {
			if pending {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(pollInterval):
					continue
				}
			}
			return nil, err
		}
		if creds != nil {
			return creds, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (s *OAuthService) pollOnce(ctx context.Context, code *deviceCodeResponse) (*auth.Credentials, bool, error) {
	q := url.Values{}
	q.Set("fingerprintId", code.FingerprintID)
	q.Set("fingerprintHash", code.FingerprintHash)
	q.Set("expiresAt", code.ExpiresAt)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.statusURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, false, fmt.Errorf("freebuff status request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("freebuff status request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("freebuff status response: %w", err)
	}
	var out statusResponse
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode == http.StatusUnauthorized || out.Error == "authorization_pending" {
		return nil, true, fmt.Errorf("freebuff authorization pending")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("freebuff status failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out.User == nil || out.User.AuthToken == "" {
		return nil, true, fmt.Errorf("freebuff authorization pending")
	}
	return &auth.Credentials{
		AccessToken: out.User.AuthToken,
		Email:       out.User.Email,
		AccountID:   out.User.ID,
		ProviderSpecific: map[string]string{
			"fingerprintId": out.User.FingerprintID,
			"userId":        out.User.ID,
			"email":         out.User.Email,
			"name":          out.User.Name,
		},
	}, false, nil
}

func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func stateParam(state string) string {
	if i := strings.IndexByte(state, ':'); i >= 0 {
		return state[:i]
	}
	return state
}
