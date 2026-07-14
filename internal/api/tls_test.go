package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/api/handlers/admin"
)

func TestTLS_Get_ReturnsDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	h := admin.NewTLSHandler(dir, http.DefaultClient)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/tls-config", nil)
	h.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %+v", resp)
	}
	if data["enabled"] != false {
		t.Errorf("enabled = %v, want false", data["enabled"])
	}
	if data["valid"] != true {
		t.Errorf("valid = %v, want true for disabled config", data["valid"])
	}
	wantCertDir := filepath.Join(dir, "certs")
	if data["certDir"] != wantCertDir {
		t.Errorf("certDir = %v, want %v", data["certDir"], wantCertDir)
	}
}

func TestTLS_Put_SaveAndRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	h := admin.NewTLSHandler(dir, http.DefaultClient)

	body := `{"enabled":true,"domain":"example.com","email":"admin@example.com","acceptTOS":true,"staging":true,"certCache":"custom-certs"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/tls-config", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Put(c)

	if w.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/tls-config", nil)
	h.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := resp["data"].(map[string]any)
	if data["enabled"] != true {
		t.Errorf("enabled = %v, want true", data["enabled"])
	}
	if data["domain"] != "example.com" {
		t.Errorf("domain = %v, want example.com", data["domain"])
	}
	if data["email"] != "admin@example.com" {
		t.Errorf("email = %v, want admin@example.com", data["email"])
	}
	if data["acceptTOS"] != true {
		t.Errorf("acceptTOS = %v, want true", data["acceptTOS"])
	}
	if data["staging"] != true {
		t.Errorf("staging = %v, want true", data["staging"])
	}
	if data["certCache"] != "custom-certs" {
		t.Errorf("certCache = %v, want custom-certs", data["certCache"])
	}
	if data["valid"] != true {
		t.Errorf("valid = %v, want true", data["valid"])
	}
	wantCertDir := filepath.Join(dir, "custom-certs")
	if data["certDir"] != wantCertDir {
		t.Errorf("certDir = %v, want %v", data["certDir"], wantCertDir)
	}

	if _, err := os.Stat(filepath.Join(dir, "https.yml")); err != nil {
		t.Errorf("expected https.yml to exist: %v", err)
	}
}

func TestTLS_Put_ValidationFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	h := admin.NewTLSHandler(dir, http.DefaultClient)

	body := `{"enabled":true,"domain":"example.com","email":"","acceptTOS":true}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/tls-config", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Put(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message, got %+v", resp)
	}
}

func TestTLS_PublicIP_EnvOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Setenv("AXON_PUBLIC_IP", "  1.2.3.4  ")
	h := admin.NewTLSHandler(dir, http.DefaultClient)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/tls-config/public-ip", nil)
	h.PublicIP(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := resp["data"].(map[string]any)
	if data["ip"] != "1.2.3.4" {
		t.Errorf("ip = %v, want 1.2.3.4", data["ip"])
	}
}

func TestTLS_CheckDNS_MatchesLocalhost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()

	// Resolve localhost to know what public IP to fake.
	addrs, err := net.LookupHost("localhost")
	if err != nil || len(addrs) == 0 {
		t.Fatalf("could not resolve localhost: %v", err)
	}
	t.Setenv("AXON_PUBLIC_IP", addrs[0])

	h := admin.NewTLSHandler(dir, http.DefaultClient)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/tls-config/check-dns?domain=localhost", nil)
	h.CheckDNS(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := resp["data"].(map[string]any)
	if data["domain"] != "localhost" {
		t.Errorf("domain = %v, want localhost", data["domain"])
	}
	if data["matches"] != true {
		t.Errorf("matches = %v, want true", data["matches"])
	}
}
