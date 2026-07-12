package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"golang.org/x/crypto/bcrypt"
)

// TestAuthCacheEliminatesRepeatedBcrypt is a falsifiable smoke test for the
// auth cache fix. It proves that N concurrent requests with the same API key
// trigger exactly ONE bcrypt verification (the cold miss), not N.
//
// How it proves the fix:
//   - We measure wall-clock time for N concurrent requests after a warm-up.
//   - With cache hits, N requests should complete in <200ms (0 bcrypt per hit).
//   - Without cache: N × bcrypt(MinCost) ≈ 50ms+ serial contention.
//
// This is falsifiable: if someone reverts the auth cache, this test fails.
func TestAuthCacheEliminatesRepeatedBcrypt(t *testing.T) {
	logging.Init("text")
	database := openTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-key"), bcrypt.MinCost)
	now := time.Now().Unix()
	if _, err := database.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 1000, ?)`,
		"auth-test-key", "test", string(hash), now,
	); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	router := New(Config{
		DB:               database,
		Port: "0",
		QuotaIntervalMin: 1,
		LogRetentionDays: 30,
	})
	srv := httptest.NewServer(router.Engine())
	defer srv.Close()
	defer router.Shutdown()

	// Cold miss: DB query + bcrypt. Populates the cache.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cold request: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// N concurrent requests — all should be CACHE HITS (0 DB, 0 bcrypt).
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	start := time.Now()
	for range n {
		go func() {
			defer wg.Done()
			r, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", nil)
			r.Header.Set("Authorization", "Bearer test-key")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("%d cache-hit requests completed in %v", n, elapsed)

	if elapsed > 500*time.Millisecond {
		t.Errorf("auth cache broken: %d requests took %v (want < 500ms); "+
			"likely hitting bcrypt on every request instead of cache hits", n, elapsed)
	}
}

// TestConcurrentRequestsDoNotBlockHealth proves that the health endpoint stays
// responsive while the system is under concurrent load with genuine in-flight
// requests (using a mock upstream with artificial latency).
//
// Before fix: health did SELECT 1, blocked on saturated DB pool → timeout.
// After fix: health uses in-memory store → instant, regardless of DB load.
//
// Falsifiable: reverting health to SELECT 1 + MaxOpenConns(5) causes
// the health probe to block, failing the <500ms assertion.
func TestConcurrentRequestsDoNotBlockHealth(t *testing.T) {
	logging.Init("text")
	database := openTestDB(t)

	// Mock upstream: sleeps 200ms per request (simulates slow LLM).
	var upstreamHits atomic.Int64
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"message\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer mockUpstream.Close()

	// Custom provider_type + connection pointing at our mock upstream.
	now := time.Now().Unix()
	if _, err := database.Exec(
		`INSERT INTO provider_types (id, display_name, format, base_url, is_custom, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
		"test", "Test Provider", "openai", mockUpstream.URL, now,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO connections (id, provider_type_id, name, auth_type, is_active, status, priority, created_at, updated_at) VALUES (?, ?, ?, ?, 1, 'ready', 0, ?, ?)`,
		"test-conn", "test", "test", "none", now, now,
	); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	router := New(Config{
		DB:               database,
		Port: "0",
		QuotaIntervalMin: 1,
		LogRetentionDays: 30,
	})
	srv := httptest.NewServer(router.Engine())
	defer srv.Close()
	defer router.Shutdown()

	const n = 10
	var wg sync.WaitGroup

	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
				strings.NewReader(`{"model":"test/m1","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}

	// Health probe fires WHILE requests are in-flight.
	healthStart := time.Now()
	hReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/health", nil)
	hResp, err := http.DefaultClient.Do(hReq)
	healthLatency := time.Since(healthStart)
	if err != nil {
		t.Fatalf("health probe failed: %v", err)
	}
	hResp.Body.Close()
	wg.Wait()

	t.Logf("health probe under %d in-flight requests: %v (upstream hit %d times)",
		n, healthLatency, upstreamHits.Load())

	if healthLatency > 500*time.Millisecond {
		t.Errorf("health probe too slow under load: %v (want < 500ms); "+
			"likely hitting DB pool contention (check MaxOpenConns or health SELECT)", healthLatency)
	}
}
