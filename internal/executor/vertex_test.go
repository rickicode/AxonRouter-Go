package executor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func generateTestServiceAccount(t *testing.T, tokenURI string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}
	privateKeyPEM := string(pem.EncodeToMemory(block))

	sa := map[string]any{
		"type":           "service_account",
		"project_id":     "test-project",
		"private_key_id": "key-id",
		"private_key":    privateKeyPEM,
		"client_email":   "test@example.iam.gserviceaccount.com",
		"client_id":      "123",
		"auth_uri":       "https://accounts.google.com/o/oauth2/auth",
		"token_uri":      tokenURI,
	}
	b, _ := json.Marshal(sa)
	return string(b)
}

type vertexTestTransport struct {
	tokenHost string
	apiHost   string
}

func (rt *vertexTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	if host == "oauth2.googleapis.com" || strings.HasSuffix(host, ".googleapis.com") && strings.Contains(host, "oauth2") {
		req.URL.Scheme = "http"
		req.URL.Host = rt.tokenHost
	} else if host == "aiplatform.googleapis.com" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.apiHost
	}
	return http.DefaultTransport.RoundTrip(req)
}

func setupVertexTest(t *testing.T) (*httptest.Server, *httptest.Server, *VertexExecutor, func()) {
	t.Helper()

	var tokenReqCount int
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenReqCount++
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}

		// Verify JWT-bearer grant. A real parse would verify the JWT signature; here we
		// just ensure the assertion is present.
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		assertion := r.FormValue("assertion")
		if assertion == "" || r.FormValue("grant_type") != vertexTokenGrantType {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		parts := strings.Split(assertion, ".")
		if len(parts) != 3 {
			http.Error(w, "bad jwt", http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/test-project/locations/global/endpoints/openapi/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body := ioReadAll(r)
		model := gjson.GetBytes(body, "model").String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "chat.completion",
			"model":  model,
			"choices": []map[string]any{{"message": map[string]any{
				"role":    "assistant",
				"content": "ok",
			}}},
		})
	}))

	authURL, _ := url.Parse(auth.URL)
	apiURL, _ := url.Parse(api.URL)
	rt := &vertexTestTransport{tokenHost: authURL.Host, apiHost: apiURL.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt

	exec := NewVertexExecutor(NewBaseExecutor())
	exec.Client.Transport = rt
	exec.streamBase.Transport = rt

	return auth, api, exec, func() {
		http.DefaultClient.Transport = old
		auth.Close()
		api.Close()
	}
}

func ioReadAll(r *http.Request) []byte {
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return b
}

func TestVertexExecutor_ExchangesServiceAccountAndStripsModelPrefix(t *testing.T) {
	_, _, exec, cleanup := setupVertexTest(t)
	defer cleanup()

	saJSON := generateTestServiceAccount(t, "https://oauth2.googleapis.com/token")
	resp, err := exec.Execute(context.Background(), &Request{
		Provider: "vertex",
		APIKey:   saJSON,
		Body:     []byte(`{"model":"vertex/gemini-2.5-pro","messages":[{"role":"user","content":"hi"}]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	model := gjson.GetBytes(resp.Body, "model").String()
	if model != "gemini-2.5-pro" {
		t.Errorf("upstream model = %q, want gemini-2.5-pro", model)
	}
}

func TestVertexExecutor_CachesAccessToken(t *testing.T) {
	_, _, exec, cleanup := setupVertexTest(t)
	defer cleanup()

	saJSON := generateTestServiceAccount(t, "https://oauth2.googleapis.com/token")
	_, _ = exec.Execute(context.Background(), &Request{
		Provider: "vertex",
		APIKey:   saJSON,
		Body:     []byte(`{"model":"vertex/gemini-2.5-pro","messages":[]}`),
	})
	_, _ = exec.Execute(context.Background(), &Request{
		Provider: "vertex",
		APIKey:   saJSON,
		Body:     []byte(`{"model":"vertex/gemini-2.5-pro","messages":[]}`),
	})

	if len(exec.tokens) != 1 {
		t.Errorf("expected 1 cached token, got %d", len(exec.tokens))
	}
}

func TestVertexExecutor_ResolvesBaseURLFromProviderSpecificData(t *testing.T) {
	_, api, exec, cleanup := setupVertexTest(t)
	defer cleanup()
	_ = api

	saJSON := generateTestServiceAccount(t, "https://oauth2.googleapis.com/token")
	// Override base_url to point to a custom path; our mock only accepts the default
	// test-project/global path, so this verifies placeholder substitution happens.
	_, err := exec.Execute(context.Background(), &Request{
		Provider: "vertex",
		APIKey:   saJSON,
		BaseURL:  "https://aiplatform.googleapis.com/v1/projects/{projectId}/locations/{location}/endpoints/openapi",
		ProviderSpecificData: map[string]string{
			"projectId": "test-project",
			"location":  "global",
		},
		Body: []byte(`{"model":"vertex/gemini-2.5-pro","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestVertexExecutor_ErrorsWithoutServiceAccount(t *testing.T) {
	exec := NewVertexExecutor(NewBaseExecutor())
	_, err := exec.Execute(context.Background(), &Request{
		Provider: "vertex",
		APIKey:   "",
		Body:     []byte(`{"model":"vertex/gemini-2.5-pro","messages":[]}`),
	})
	if err == nil {
		t.Fatal("expected error without service account")
	}
}

func TestVertexExecutor_DefaultsExpiresInToOneHour(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   0,
			"token_type":   "Bearer",
		})
	}))
	defer auth.Close()

	authURL, _ := url.Parse(auth.URL)
	rt := &vertexTestTransport{tokenHost: authURL.Host, apiHost: authURL.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	defer func() { http.DefaultClient.Transport = old }()

	exec := NewVertexExecutor(NewBaseExecutor())
	exec.Client.Transport = rt

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := buildVertexJWT(key, "test@example.com", "https://oauth2.googleapis.com/token")
	if err != nil {
		t.Fatal(err)
	}
	_, expiresIn, err := exchangeVertexJWT(context.Background(), "https://oauth2.googleapis.com/token", jwt)
	if err != nil {
		t.Fatalf("exchangeVertexJWT failed: %v", err)
	}
	if expiresIn != 3600 {
		t.Errorf("expiresIn = %d, want 3600", expiresIn)
	}
}

func TestBuildVertexJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := buildVertexJWT(key, "test@example.com", "https://oauth2.googleapis.com/token")
	if err != nil {
		t.Fatalf("build jwt failed: %v", err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 jwt parts, got %d", len(parts))
	}
}

func TestResolveVertexBaseURL(t *testing.T) {
	base := "https://aiplatform.googleapis.com/v1/projects/{projectId}/locations/{location}/endpoints/openapi"
	got := resolveVertexBaseURL(base, "proj1", map[string]string{"location": "us-central1"})
	want := "https://aiplatform.googleapis.com/v1/projects/proj1/locations/us-central1/endpoints/openapi"
	if got != want {
		t.Errorf("resolveVertexBaseURL = %q, want %q", got, want)
	}
}

func TestStripVertexModelPrefix(t *testing.T) {
	in := []byte(`{"model":"vertex/gemini-2.5-pro"}`)
	out := stripVertexModelPrefix(in)
	if got := gjson.GetBytes(out, "model").String(); got != "gemini-2.5-pro" {
		t.Errorf("model = %q, want gemini-2.5-pro", got)
	}
}
