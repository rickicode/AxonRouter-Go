package version

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func githubJSON(tag string, published string, htmlURL string) string {
	return fmt.Sprintf(`{
		"tag_name": %q,
		"published_at": %q,
		"html_url": %q
	}`, tag, published, htmlURL)
}

func TestCheckLatest_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubJSON("v0.3.2", "2026-07-14T00:00:00Z", "https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.2")))
	}))
	defer server.Close()

	client := server.Client()
	oldURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = oldURL }()

	info, err := CheckLatest(client)
	if err != nil {
		t.Fatalf("CheckLatest returned error: %v", err)
	}
	if info.Tag != "v0.3.2" {
		t.Errorf("Tag = %q, want v0.3.2", info.Tag)
	}
	if info.Version != "0.3.2" {
		t.Errorf("Version = %q, want 0.3.2", info.Version)
	}
	if info.PublishedAt != "2026-07-14T00:00:00Z" {
		t.Errorf("PublishedAt = %q, want 2026-07-14T00:00:00Z", info.PublishedAt)
	}
	if !strings.Contains(info.HTMLURL, "/releases/tag/v0.3.2") {
		t.Errorf("HTMLURL = %q, want release tag URL", info.HTMLURL)
	}
}

func TestCheckLatest_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := server.Client()
	oldURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = oldURL }()

	_, err := CheckLatest(client)
	if err == nil {
		t.Fatalf("expected error for non-OK response")
	}
}

func TestCachedChecker_LatestVersion_Caches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubJSON("v0.3.2", "2026-07-14T00:00:00Z", "https://example.com/v0.3.2")))
	}))
	defer server.Close()

	oldURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = oldURL }()

	checker := NewChecker(server.Client())
	defer checker.Stop()
	checker.ttl = 5 * time.Minute

	// Wait for the background goroutine to finish its first fetch.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := checker.LatestVersion(); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	info, ok := checker.LatestVersion()
	if !ok {
		t.Fatalf("LatestVersion returned !ok")
	}
	if info.Version != "0.3.2" {
		t.Errorf("Version = %q, want 0.3.2", info.Version)
	}

	calls = 0
	_, _ = checker.LatestVersion()
	if calls != 0 {
		t.Errorf("LatestVersion triggered %d server calls, want 0", calls)
	}
}

func TestCachedChecker_LatestVersion_NeverBlocksOnNetwork(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubJSON("v0.3.2", "2026-07-14T00:00:00Z", "https://example.com/v0.3.2")))
	}))
	defer server.Close()

	oldURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = oldURL }()

	checker := NewChecker(server.Client())
	checker.ttl = 5 * time.Minute

	start := time.Now()
	_, ok := checker.LatestVersion()
	elapsed := time.Since(start)
	if ok {
		t.Fatalf("LatestVersion returned ok before first fetch completed")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("LatestVersion blocked for %v", elapsed)
	}

	close(block)
	checker.Stop()
}

func TestUpdateAvailable_Newer(t *testing.T) {
	SetTestVersion("0.3.3")
	defer ClearTestVersion()

	checker := NewChecker(nil)
	defer checker.Stop()
	checker.ttl = 5 * time.Minute
	checker.cached = ReleaseInfo{Version: "0.3.4"}
	checker.cachedAt = time.Now()

	available := checker.UpdateAvailable()
	if !available {
		t.Fatalf("expected update available when cached version 0.3.4 > current 0.3.3")
	}
}

func TestUpdateAvailable_Current(t *testing.T) {
	SetTestVersion("0.3.3")
	defer ClearTestVersion()

	checker := NewChecker(nil)
	defer checker.Stop()
	checker.ttl = 5 * time.Minute
	checker.cached = ReleaseInfo{Version: "0.3.3"}
	checker.cachedAt = time.Now()

	available := checker.UpdateAvailable()
	if available {
		t.Fatalf("expected no update when cached version equals current")
	}
}

func TestUpdateAvailable_Older(t *testing.T) {
	SetTestVersion("0.3.3")
	defer ClearTestVersion()

	checker := NewChecker(nil)
	defer checker.Stop()
	checker.ttl = 5 * time.Minute
	checker.cached = ReleaseInfo{Version: "0.3.0"}
	checker.cachedAt = time.Now()

	available := checker.UpdateAvailable()
	if available {
		t.Fatalf("expected no update when cached version is older")
	}
}
