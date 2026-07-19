package codebuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

func TestNewOAuthServiceUsesDefaultTimeout(t *testing.T) {
	svc := NewOAuthService(nil)
	if svc.httpClient.Timeout != httpClientTimeout {
		t.Fatalf("http client timeout = %v, want %v", svc.httpClient.Timeout, httpClientTimeout)
	}
}

func TestRequestDeviceCodeSuccess(t *testing.T) {
	var gotQuery url.Values
	var gotHeaders http.Header
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		gotQuery = r.URL.Query()
		gotHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"state":   "tencent-state-123",
				"authUrl": "https://codebuddy.ai/auth/confirm",
			},
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.stateURL = ts.URL + "/state"

	ctx := context.Background()
	dc, err := svc.requestDeviceCode(ctx)
	if err != nil {
		t.Fatalf("requestDeviceCode error = %v", err)
	}
	if dc.state != "tencent-state-123" {
		t.Errorf("state = %q, want tencent-state-123", dc.state)
	}
	if dc.authUrl != "https://codebuddy.ai/auth/confirm" {
		t.Errorf("authUrl = %q, want https://codebuddy.ai/auth/confirm", dc.authUrl)
	}
	if gotQuery.Get("platform") != platform {
		t.Errorf("query platform = %v, want %q", gotQuery.Get("platform"), platform)
	}
	if strings.TrimSpace(gotBody) != "{}" {
		t.Errorf("body = %q, want {}", gotBody)
	}
	if gotHeaders.Get("User-Agent") != userAgent {
		t.Errorf("User-Agent = %q, want %q", gotHeaders.Get("User-Agent"), userAgent)
	}
	if gotHeaders.Get("X-Product") != "SaaS" {
		t.Errorf("X-Product = %q, want SaaS", gotHeaders.Get("X-Product"))
	}
}

func TestRequestDeviceCodeMissingState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"authUrl": "https://codebuddy.ai/auth/confirm",
			},
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.stateURL = ts.URL + "/state"

	_, err := svc.requestDeviceCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestPollTokenSuccessAfterPending(t *testing.T) {
	var pollCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Query().Get("state"); got != "tencent-state-123" {
			t.Fatalf("state = %q, want tencent-state-123", got)
		}
		count := atomic.AddInt32(&pollCount, 1)
		if count == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 11217,
				"msg":  "RetryFetchToken",
				"data": map[string]any{},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"accessToken":  "access-xyz",
				"refreshToken": "refresh-xyz",
				"tokenType":    "Bearer",
				"expiresIn":    3600,
			},
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.tokenURL = ts.URL + "/token"
	svc.testPollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	creds, err := svc.pollToken(ctx, "tencent-state-123")
	if err != nil {
		t.Fatalf("pollToken error = %v", err)
	}
	if creds.AccessToken != "access-xyz" {
		t.Errorf("access token = %q, want access-xyz", creds.AccessToken)
	}
	if creds.RefreshToken != "refresh-xyz" {
		t.Errorf("refresh token = %q, want refresh-xyz", creds.RefreshToken)
	}
	if atomic.LoadInt32(&pollCount) != 2 {
		t.Errorf("poll count = %d, want 2", atomic.LoadInt32(&pollCount))
	}
}

func TestRefreshTokenSuccess(t *testing.T) {
	var gotHeaders http.Header
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		gotHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"accessToken":  "new-access",
				"refreshToken": "new-refresh",
				"expiresIn":    1800,
			},
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.refreshURL = ts.URL + "/refresh"

	ctx := context.Background()
	creds := &auth.Credentials{RefreshToken: "old-refresh"}
	newCreds, err := svc.RefreshToken(ctx, creds)
	if err != nil {
		t.Fatalf("RefreshToken error = %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Errorf("access token = %q, want new-access", newCreds.AccessToken)
	}
	if newCreds.RefreshToken != "new-refresh" {
		t.Errorf("refresh token = %q, want new-refresh", newCreds.RefreshToken)
	}
	if gotHeaders.Get("X-Refresh-Token") != "old-refresh" {
		t.Errorf("X-Refresh-Token = %q, want old-refresh", gotHeaders.Get("X-Refresh-Token"))
	}
	if strings.TrimSpace(gotBody) != "{}" {
		t.Errorf("body = %q, want {}", gotBody)
	}
}

func TestExchangeCodeNotSupported(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.ExchangeCode(context.Background(), "some-code")
	if err == nil {
		t.Fatal("expected ExchangeCode to return error")
	}
	if !strings.Contains(err.Error(), "device-code") {
		t.Errorf("error = %q, expected mention of device-code flow", err.Error())
	}
}

func TestGenerateAuthURLAndGetUserCode(t *testing.T) {
	svc := NewOAuthService(nil)
	svc.states["state-abc"] = &deviceFlowState{
		state:   "tencent-state-abc",
		authUrl: "https://codebuddy.ai/auth/abc",
	}

	url, err := svc.GenerateAuthURL(context.Background(), "state-abc:0")
	if err != nil {
		t.Fatalf("GenerateAuthURL error = %v", err)
	}
	if url != "https://codebuddy.ai/auth/abc" {
		t.Errorf("auth URL = %q", url)
	}
	if got := svc.GetUserCode("state-abc"); got != "tencent-state-abc" {
		t.Errorf("GetUserCode = %q, want tencent-state-abc", got)
	}
}

func TestGenerateAuthURLFailsForUnknownState(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.GenerateAuthURL(context.Background(), "unknown:0")
	if err == nil {
		t.Fatal("expected error for unknown state")
	}
}
