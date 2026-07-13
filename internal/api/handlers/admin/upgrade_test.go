package admin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rickicode/AxonRouter-Go/internal/version"
)

func TestUpgrade_DownloadsAndVerifiesBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	binary := []byte("fake binary content")
	sum := fmt.Sprintf("%x", sha256.Sum256(binary))
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", sum, asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["version"] != "0.3.2" {
		t.Errorf("version = %v, want 0.3.2", resp["version"])
	}
	if resp["asset"] != asset {
		t.Errorf("asset = %v, want %s", resp["asset"], asset)
	}

	wantPath := filepath.Join(h.binDir, asset)
	if resp["path"] != wantPath {
		t.Errorf("path = %v, want %s", resp["path"], wantPath)
	}

	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read downloaded binary: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("downloaded binary = %q, want %q", got, binary)
	}
}

func TestUpgrade_ChecksumMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	binary := []byte("fake binary content")
	asset := fmt.Sprintf("axonrouter-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"tag_name":"v0.3.2","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2"}`))
		case "/download/v0.3.2/" + asset:
			w.WriteHeader(http.StatusOK)
			w.Write(binary)
		case "/download/v0.3.2/checksums.txt":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", asset)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL+"/latest")
	h := NewUpgradeHandler(checker)
	h.client = server.Client()
	h.baseURL = server.URL + "/download"
	h.binDir = t.TempDir()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/upgrade", nil)
	h.Upgrade(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
