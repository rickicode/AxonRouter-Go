package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

func TestExchangeCode_NotUsed(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.ExchangeCode(context.Background(), "some-code")
	if err == nil || !strings.Contains(err.Error(), "device-code") {
		t.Fatalf("expected device-code error, got %v", err)
	}
}

func TestDeviceFlow_Success(t *testing.T) {
	polls := atomic.Int32{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"device_code":"dc123","user_code":"UC-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`)
		case "/login/oauth/access_token":
			polls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"gh-access","token_type":"bearer","scope":"read:user"}`)
		case "/copilot_internal/v2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"cp-token","expires_at":1234567890}`)
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":42,"login":"octocat","name":"Octo Cat","email":"octo@example.com"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.deviceCodeURL = ts.URL + "/login/device/code"
	svc.tokenURL = ts.URL + "/login/oauth/access_token"
	svc.copilotTokenURL = ts.URL + "/copilot_internal/v2/token"
	svc.userInfoURL = ts.URL + "/user"
	// Speed up polling for the test.
	svc.defaultPollTimeout = 5 * time.Second
	svc.postExchangeDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	port, resultChan, err := svc.StartLocalServer(ctx, "state-abc")
	if err != nil {
		t.Fatal(err)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0 for device flow", port)
	}

	authURL, err := svc.GenerateAuthURL(ctx, "state-abc:0")
	if err != nil {
		t.Fatal(err)
	}
	if authURL != "https://github.com/login/device" {
		t.Errorf("authURL = %q", authURL)
	}

	select {
	case creds := <-resultChan:
		if creds == nil {
			t.Fatal("expected credentials, got nil")
		}
		if creds.AccessToken != "gh-access" {
			t.Errorf("access token = %q, want gh-access", creds.AccessToken)
		}
		if creds.ProviderSpecific["copilot_token"] != "cp-token" {
			t.Errorf("copilot_token = %q, want cp-token", creds.ProviderSpecific["copilot_token"])
		}
		if creds.ProviderSpecific["github_login"] != "octocat" {
			t.Errorf("github_login = %q, want octocat", creds.ProviderSpecific["github_login"])
		}
		if creds.ProviderSpecific["github_email"] != "octo@example.com" {
			t.Errorf("github_email = %q", creds.ProviderSpecific["github_email"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for credentials")
	}

	if polls.Load() == 0 {
		t.Error("token endpoint was never polled")
	}
}

func TestDeviceFlow_PendingThenSuccess(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"device_code":"dc","user_code":"UC","verification_uri":"https://verify","expires_in":900,"interval":1}`)
		case "/login/oauth/access_token":
			calls++
			if calls < 2 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"ok"}`)
		case "/copilot_internal/v2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"cptok"}`)
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":1,"login":"a"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.deviceCodeURL = ts.URL + "/login/device/code"
	svc.tokenURL = ts.URL + "/login/oauth/access_token"
	svc.copilotTokenURL = ts.URL + "/copilot_internal/v2/token"
	svc.userInfoURL = ts.URL + "/user"
	svc.defaultPollTimeout = 5 * time.Second
	svc.postExchangeDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, resultChan, err := svc.StartLocalServer(ctx, "st")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case creds := <-resultChan:
		if creds == nil {
			t.Fatal("expected credentials")
		}
		if creds.AccessToken != "ok" {
			t.Errorf("access token = %q, want ok", creds.AccessToken)
		}
		if calls < 2 {
			t.Errorf("token endpoint called %d times, want >=2", calls)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for credentials")
	}
}

func TestRefreshToken_Unsupported(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.RefreshToken(context.Background(), &auth.Credentials{})
	if err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("expected refresh unsupported error, got %v", err)
	}
}
