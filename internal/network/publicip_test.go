package network

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPublicIP_EnvOverride(t *testing.T) {
	t.Setenv("AXON_PUBLIC_IP", "203.0.113.5")

	ip, err := PublicIP(http.DefaultClient)
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if ip != "203.0.113.5" {
		t.Errorf("PublicIP = %q, want 203.0.113.5", ip)
	}
}

func TestPublicIP_PrimaryService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("198.51.100.7\n"))
	}))
	defer server.Close()

	oldPrimary := ipv4HazipURL
	ipv4HazipURL = server.URL + "/"
	defer func() { ipv4HazipURL = oldPrimary }()

	ip, err := PublicIP(server.Client())
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if ip != "198.51.100.7" {
		t.Errorf("PublicIP = %q, want 198.51.100.7", ip)
	}
}

func TestPublicIP_FallbackService(t *testing.T) {
	primaryCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("192.0.2.42"))
	}))
	defer fallback.Close()

	oldPrimary := ipv4HazipURL
	oldFallback := ifconfigURL
	ipv4HazipURL = primary.URL + "/"
	ifconfigURL = fallback.URL + "/"
	defer func() {
		ipv4HazipURL = oldPrimary
		ifconfigURL = oldFallback
	}()

	ip, err := PublicIP(fallback.Client())
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if ip != "192.0.2.42" {
		t.Errorf("PublicIP = %q, want 192.0.2.42", ip)
	}
	if primaryCalls == 0 {
		t.Errorf("primary service was never called")
	}
}

func TestPublicIP_BothServicesFail(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fallback.Close()

	oldPrimary := ipv4HazipURL
	oldFallback := ifconfigURL
	ipv4HazipURL = primary.URL + "/"
	ifconfigURL = fallback.URL + "/"
	defer func() {
		ipv4HazipURL = oldPrimary
		ifconfigURL = oldFallback
	}()

	_, err := PublicIP(fallback.Client())
	if err == nil {
		t.Fatalf("expected error when both services fail")
	}
}

func TestPublicIP_TrimsWhitespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("  198.51.100.9  \n"))
	}))
	defer server.Close()

	oldPrimary := ipv4HazipURL
	ipv4HazipURL = server.URL + "/"
	defer func() { ipv4HazipURL = oldPrimary }()

	ip, err := PublicIP(server.Client())
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if strings.Contains(ip, " ") || strings.Contains(ip, "\n") {
		t.Errorf("PublicIP contains whitespace: %q", ip)
	}
	if ip != "198.51.100.9" {
		t.Errorf("PublicIP = %q, want 198.51.100.9", ip)
	}
}

func TestPublicIP_RespectsMaxRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.Repeat("x", 128)))
	}))
	defer server.Close()

	oldPrimary := ipv4HazipURL
	ipv4HazipURL = server.URL + "/"
	defer func() { ipv4HazipURL = oldPrimary }()

	ip, err := PublicIP(server.Client())
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if len(ip) > 64 {
		t.Errorf("PublicIP length = %d, want at most 64", len(ip))
	}
}

func TestPublicIP_EmptyEnvFallsThrough(t *testing.T) {
	os.Unsetenv("AXON_PUBLIC_IP")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("198.51.100.11"))
	}))
	defer server.Close()

	oldPrimary := ipv4HazipURL
	ipv4HazipURL = server.URL + "/"
	defer func() { ipv4HazipURL = oldPrimary }()

	ip, err := PublicIP(server.Client())
	if err != nil {
		t.Fatalf("PublicIP returned error: %v", err)
	}
	if ip != "198.51.100.11" {
		t.Errorf("PublicIP = %q, want 198.51.100.11", ip)
	}
}
