package proxypool

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "proxy-resolver-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func insertPool(t *testing.T, database *sql.DB, id, name, typ, proxyURL, noProxy, relayAuth string, active bool, testStatus string) {
	t.Helper()
	now := time.Now().Unix()
	activeInt := 0
	if active {
		activeInt = 1
	}
	_, err := database.Exec(
		`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, typ, proxyURL, noProxy, relayAuth, activeInt, testStatus, now, now,
	)
	if err != nil {
		t.Fatalf("insert pool %s: %v", id, err)
	}
}

func insertGroup(t *testing.T, database *sql.DB, id, mode string, stickyLimit int, strictProxy, isActive bool, poolIDs []string) {
	t.Helper()
	now := time.Now().Unix()
	rawIDs, _ := json.Marshal(poolIDs)
	strictInt := 0
	if strictProxy {
		strictInt = 1
	}
	activeInt := 0
	if isActive {
		activeInt = 1
	}
	_, err := database.Exec(
		`INSERT INTO proxy_groups (id, name, mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, id, mode, stickyLimit, strictInt, string(rawIDs), activeInt, now, now,
	)
	if err != nil {
		t.Fatalf("insert group %s: %v", id, err)
	}
}

func TestResolveConnectionPoolDirect(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "p1", "test-p1", "http", "http://proxy.example:8080", "", "", true, "active")

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyPoolId":"p1"}`, "")
	if cfg.Source != "connection-pool" || cfg.ProxyURL != "http://proxy.example:8080" {
		t.Fatalf("unexpected connection pool config: %+v", cfg)
	}
}

func TestResolveConnectionPoolDirectErrorReturnsDirect(t *testing.T) {
	database := newTestDB(t)
	// Non-strict connection pool failing health check should fall back to direct.
	insertPool(t, database, "p2", "test-p2", "http", "http://dead.example:8080", "", "", true, "error")

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyPoolId":"p2"}`, "")
	if cfg.Source != "connection-pool" || cfg.ProxyURL != "" || cfg.Enabled {
		t.Fatalf("expected disabled direct fallback for error pool, got: %+v", cfg)
	}
}

func TestResolveGroup(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "g1-p1", "p1", "http", "http://p1.example:8080", "", "", true, "active")
	insertPool(t, database, "g1-p2", "p2", "http", "http://p2.example:8080", "", "", true, "active")
	insertGroup(t, database, "group1", "roundrobin", 1, false, true, []string{"g1-p1", "g1-p2"})

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyGroupId":"group1"}`, "")
	if cfg.Source != "connection-group" || cfg.ProxyURL == "" {
		t.Fatalf("unexpected group config: %+v", cfg)
	}
}

func TestResolveGroupSkipsErrorPools(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "g2-p1", "p1", "http", "http://p1.example:8080", "", "", true, "error")
	insertPool(t, database, "g2-p2", "p2", "http", "http://p2.example:8080", "", "", true, "active")
	insertGroup(t, database, "group2", "roundrobin", 1, false, true, []string{"g2-p1", "g2-p2"})

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyGroupId":"group2"}`, "")
	if cfg.Source != "connection-group" || cfg.ProxyURL != "http://p2.example:8080" {
		t.Fatalf("expected healthy pool g2-p2, got: %+v", cfg)
	}
}

func TestResolveGroupAllErrorAndNonStrict(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "g3-p1", "p1", "http", "http://p1.example:8080", "", "", true, "error")
	insertGroup(t, database, "group3", "roundrobin", 1, false, true, []string{"g3-p1"})

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyGroupId":"group3"}`, "")
	if cfg.Source != "connection-group" || cfg.ProxyURL != "" || cfg.Enabled {
		t.Fatalf("expected direct fallback when all pools error in non-strict group, got: %+v", cfg)
	}
}

func TestResolveGroupAllErrorAndStrict(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "g4-p1", "p1", "http", "http://p1.example:8080", "", "", true, "error")
	insertGroup(t, database, "group4", "roundrobin", 1, true, true, []string{"g4-p1"})

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyGroupId":"group4"}`, "")
	if cfg.Source != "none" || cfg.Enabled {
		t.Fatalf("expected no config when all pools error in strict group, got: %+v", cfg)
	}
}

func TestResolveProviderDefault(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "pdef1", "pdef1", "http", "http://pdef.example:8080", "", "", true, "active")

	defaults := map[string]map[string]any{
		"openai": {"proxyPoolId": "pdef1"},
	}
	raw, _ := json.Marshal(defaults)
	database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "provider_proxy_defaults", string(raw), time.Now().Unix())

	r := NewResolver(database)
	cfg := r.Resolve("", "openai")
	if cfg.Source != "provider-default-pool" || cfg.ProxyURL != "http://pdef.example:8080" {
		t.Fatalf("unexpected provider default config: %+v", cfg)
	}
}

func TestResolveProviderDefaultErrorFallsBack(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "pdef2", "pdef2", "http", "http://pdef.example:8080", "", "", true, "error")

	defaults := map[string]map[string]any{
		"openai": {"proxyPoolId": "pdef2"},
	}
	raw, _ := json.Marshal(defaults)
	database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "provider_proxy_defaults", string(raw), time.Now().Unix())

	r := NewResolver(database)
	cfg := r.Resolve("", "openai")
	if cfg.Source != "provider-default-pool" || cfg.Enabled || cfg.ProxyURL != "" {
		t.Fatalf("expected disabled fallback for error default pool, got: %+v", cfg)
	}
}

func TestResolvePriorityGroupBeatsPool(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "prio-p1", "p1", "http", "http://p1.example:8080", "", "", true, "active")
	insertPool(t, database, "prio-p2", "p2", "http", "http://p2.example:8080", "", "", true, "active")
	insertGroup(t, database, "prio-group", "roundrobin", 1, false, true, []string{"prio-p2"})

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyGroupId":"prio-group","proxyPoolId":"prio-p1"}`, "")
	if cfg.ProxyURL != "http://p2.example:8080" {
		t.Fatalf("expected group (p2) to beat direct pool (p1), got: %+v", cfg)
	}
}

func TestCacheAvoidsDBQueries(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "cache-p1", "p1", "http", "http://p1.example:8080", "", "", true, "active")

	r := NewResolver(database)
	r.SetCacheTTL(time.Minute)

	// Prime cache.
	_ = r.Resolve(`{"proxyPoolId":"cache-p1"}`, "")

	// Delete the pool row. With a cold resolver the next Resolve would fail.
	if _, err := database.Exec("DELETE FROM proxy_pools WHERE id = ?", "cache-p1"); err != nil {
		t.Fatalf("delete pool: %v", err)
	}

	cfg := r.Resolve(`{"proxyPoolId":"cache-p1"}`, "")
	if cfg.ProxyURL != "http://p1.example:8080" {
		t.Fatalf("expected cached pool to survive DB delete, got: %+v", cfg)
	}

	// After invalidation the deleted pool should resolve as none.
	r.Invalidate()
	cfg = r.Resolve(`{"proxyPoolId":"cache-p1"}`, "")
	if cfg.Source != "none" || cfg.Enabled {
		t.Fatalf("expected cache invalidation to expose DB delete, got: %+v", cfg)
	}
}

func TestPickRoundRobinDistribution(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "rr1", "rr1", "http", "http://rr1.example:8080", "", "", true, "active")
	insertPool(t, database, "rr2", "rr2", "http", "http://rr2.example:8080", "", "", true, "active")
	insertPool(t, database, "rr3", "rr3", "http", "http://rr3.example:8080", "", "", true, "active")
	insertGroup(t, database, "rr-group", "roundrobin", 1, false, true, []string{"rr1", "rr2", "rr3"})

	r := NewResolver(database)
	seen := map[string]int{}
	for range 9 {
		cfg := r.Resolve(`{"proxyGroupId":"rr-group"}`, "")
		seen[cfg.ProxyURL]++
	}
	for _, want := range []string{"http://rr1.example:8080", "http://rr2.example:8080", "http://rr3.example:8080"} {
		if seen[want] != 3 {
			t.Fatalf("round-robin distribution wrong for %s: got %v", want, seen)
		}
	}
}

func TestPickStickyHonoursLimit(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "st1", "st1", "http", "http://st1.example:8080", "", "", true, "active")
	insertPool(t, database, "st2", "st2", "http", "http://st2.example:8080", "", "", true, "active")
	// stickyLimit=2 means use the same pool twice before rotating.
	insertGroup(t, database, "sticky-group", "sticky", 2, false, true, []string{"st1", "st2"})

	r := NewResolver(database)
	seq := make([]string, 0, 8)
	for range 8 {
		cfg := r.Resolve(`{"proxyGroupId":"sticky-group"}`, "")
		seq = append(seq, cfg.ProxyURL)
	}
	for i, want := range []string{
		"http://st1.example:8080", "http://st1.example:8080",
		"http://st2.example:8080", "http://st2.example:8080",
		"http://st1.example:8080", "http://st1.example:8080",
		"http://st2.example:8080", "http://st2.example:8080",
	} {
		if seq[i] != want {
			t.Fatalf("sticky sequence mismatch at %d: got %v", i, seq)
		}
	}
}

func TestPickRandomPicksFromActivePools(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "rand1", "rand1", "http", "http://rand1.example:8080", "", "", true, "active")
	insertPool(t, database, "rand2", "rand2", "http", "http://rand2.example:8080", "", "", true, "active")
	insertPool(t, database, "rand3", "rand3", "http", "http://rand3.example:8080", "", "", true, "active")
	insertGroup(t, database, "random-group", "random", 1, false, true, []string{"rand1", "rand2", "rand3"})

	r := NewResolver(database)
	seq := make([]string, 0, 90)
	seen := map[string]int{}
	for range 90 {
		cfg := r.Resolve(`{"proxyGroupId":"random-group"}`, "")
		seq = append(seq, cfg.ProxyURL)
		seen[cfg.ProxyURL]++
	}
	if len(seen) != 3 {
		t.Fatalf("expected all 3 random pools to be selected, got: %v", seen)
	}
	hasAdjacentRepeat := false
	for i := 1; i < len(seq); i++ {
		if seq[i] == seq[i-1] {
			hasAdjacentRepeat = true
			break
		}
	}
	if !hasAdjacentRepeat {
		t.Fatalf("expected random sequence to contain adjacent repeats, got: %v", seq)
	}
}

func TestResolveGroupRandomSkipsErrorAndStrictFallback(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "rand-err", "rand-err", "http", "http://rand-err.example:8080", "", "", true, "error")
	insertPool(t, database, "rand-ok", "rand-ok", "http", "http://rand-ok.example:8080", "", "", true, "active")
	insertGroup(t, database, "random-error-group", "random", 1, false, true, []string{"rand-err", "rand-ok"})

	r := NewResolver(database)
	for range 20 {
		cfg := r.Resolve(`{"proxyGroupId":"random-error-group"}`, "")
		if cfg.ProxyURL != "http://rand-ok.example:8080" {
			t.Fatalf("expected only healthy pool, got: %+v", cfg)
		}
	}

	// All-error non-strict group should fall back to direct connection.
	insertPool(t, database, "rand-err2", "rand-err2", "http", "http://rand-err2.example:8080", "", "", true, "error")
	insertGroup(t, database, "random-all-error-group", "random", 1, false, true, []string{"rand-err2"})
	cfg := r.Resolve(`{"proxyGroupId":"random-all-error-group"}`, "")
	if cfg.Source != "connection-group" || cfg.ProxyURL != "" || cfg.Enabled {
		t.Fatalf("expected direct fallback for all-error non-strict random group, got: %+v", cfg)
	}

	// All-error strict group should return no config.
	insertGroup(t, database, "random-all-error-strict-group", "random", 1, true, true, []string{"rand-err2"})
	cfg = r.Resolve(`{"proxyGroupId":"random-all-error-strict-group"}`, "")
	if cfg.Source != "none" || cfg.Enabled {
		t.Fatalf("expected no config for all-error strict random group, got: %+v", cfg)
	}
}

func TestResolveCandidatesGroupRoundRobinRotatesPrimary(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "crr1", "crr1", "http", "http://crr1.example:8080", "", "", true, "active")
	insertPool(t, database, "crr2", "crr2", "http", "http://crr2.example:8080", "", "", true, "active")
	insertPool(t, database, "crr3", "crr3", "http", "http://crr3.example:8080", "", "", true, "active")
	insertGroup(t, database, "cand-rr", "roundrobin", 1, false, true, []string{"crr1", "crr2", "crr3"})

	r := NewResolver(database)
	var primaries []string
	for range 3 {
		cands := r.ResolveCandidates(`{"proxyGroupId":"cand-rr"}`, "")
		if len(cands) == 0 {
			t.Fatal("expected candidates")
		}
		primaries = append(primaries, cands[0].ProxyPoolID)
	}
	// Round-robin must rotate the primary on every request.
	if primaries[0] != "crr1" || primaries[1] != "crr2" || primaries[2] != "crr3" {
		t.Fatalf("expected rotating primaries crr1,crr2,crr3, got %v", primaries)
	}
}

func TestResolveCandidatesGroupStickyPrimary(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "cs1", "cs1", "http", "http://cs1.example:8080", "", "", true, "active")
	insertPool(t, database, "cs2", "cs2", "http", "http://cs2.example:8080", "", "", true, "active")
	insertGroup(t, database, "cand-sticky", "sticky", 2, false, true, []string{"cs1", "cs2"})

	r := NewResolver(database)
	first := r.ResolveCandidates(`{"proxyGroupId":"cand-sticky"}`, "")
	second := r.ResolveCandidates(`{"proxyGroupId":"cand-sticky"}`, "")
	if first[0].ProxyPoolID != second[0].ProxyPoolID {
		t.Fatalf("sticky primary rotated too early: %s -> %s", first[0].ProxyPoolID, second[0].ProxyPoolID)
	}
	third := r.ResolveCandidates(`{"proxyGroupId":"cand-sticky"}`, "")
	if third[0].ProxyPoolID == first[0].ProxyPoolID {
		t.Fatalf("sticky primary should rotate after stickyLimit, still %s", third[0].ProxyPoolID)
	}
}

func TestResolveCandidatesGroupIncludesAllActivePoolsAndDirectFallback(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "c1", "c1", "http", "http://c1.example:8080", "", "", true, "active")
	insertPool(t, database, "c2", "c2", "http", "http://c2.example:8080", "", "", true, "active")
	insertPool(t, database, "c3", "c3", "http", "http://c3.example:8080", "", "", true, "error") // must be skipped
	insertGroup(t, database, "cand-all", "roundrobin", 1, false, true, []string{"c1", "c2", "c3"})

	r := NewResolver(database)
	cands := r.ResolveCandidates(`{"proxyGroupId":"cand-all"}`, "")
	if len(cands) != 3 { // 2 healthy pools + trailing direct fallback
		t.Fatalf("expected 3 candidates (2 pools + direct fallback), got %+v", cands)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.ProxyPoolID] = true
	}
	if !got["c1"] || !got["c2"] || got["c3"] {
		t.Fatalf("expected only healthy pools c1,c2 in candidates, got %+v", cands)
	}
	if last := cands[len(cands)-1]; last.Source != "direct-fallback" {
		t.Fatalf("expected trailing direct-fallback for non-strict group, got %+v", last)
	}
}

func TestResolveCandidatesGroupStrictNoDirectFallback(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "cst1", "cst1", "http", "http://cst1.example:8080", "", "", true, "active")
	insertPool(t, database, "cst2", "cst2", "http", "http://cst2.example:8080", "", "", true, "active")
	insertGroup(t, database, "cand-strict", "roundrobin", 1, true, true, []string{"cst1", "cst2"})

	r := NewResolver(database)
	cands := r.ResolveCandidates(`{"proxyGroupId":"cand-strict"}`, "")
	if len(cands) != 2 {
		t.Fatalf("expected exactly 2 candidates (no direct fallback) for strict group, got %+v", cands)
	}
	for _, c := range cands {
		if c.Source == "direct-fallback" {
			t.Fatalf("strict group must not include direct fallback, got %+v", cands)
		}
		if !c.StrictProxy {
			t.Fatalf("expected StrictProxy on group candidates, got %+v", c)
		}
	}
}

func TestResolveCandidatesErrorPoolNoPhantom(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "dead1", "dead1", "http", "http://dead1.example:8080", "", "", true, "error")

	r := NewResolver(database)
	cands := r.ResolveCandidates(`{"proxyPoolId":"dead1"}`, "")
	// A dead non-strict pool must resolve to a single direct fallback — never a
	// phantom candidate followed by a second direct fallback (2x direct attempts).
	if len(cands) != 1 {
		t.Fatalf("expected exactly 1 direct-fallback candidate, got %+v", cands)
	}
	if cands[0].Source != "direct-fallback" {
		t.Fatalf("expected direct-fallback, got %+v", cands[0])
	}
}

func TestResolveCandidatesErrorPoolStopsProviderDefaultFallthrough(t *testing.T) {
	database := newTestDB(t)
	// Connection pins dead1 (error); a healthy provider-default pool also
	// exists. The pinned pool's intent (go direct) must win — the resolver
	// must NOT silently route through the provider default, and must not
	// emit a phantom.
	insertPool(t, database, "dead2", "dead2", "http", "http://dead2.example:8080", "", "", true, "error")
	insertPool(t, database, "pdef3", "pdef3", "http", "http://pdef3.example:8080", "", "", true, "active")
	defaults := map[string]map[string]any{"openai": {"proxyPoolId": "pdef3"}}
	raw, _ := json.Marshal(defaults)
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "provider_proxy_defaults", string(raw), time.Now().Unix()); err != nil {
		t.Fatalf("insert defaults: %v", err)
	}

	r := NewResolver(database)
	cands := r.ResolveCandidates(`{"proxyPoolId":"dead2"}`, "openai")
	if len(cands) != 1 || cands[0].Source != "direct-fallback" {
		t.Fatalf("expected single direct-fallback (no phantom, no provider-default), got %+v", cands)
	}
}

func TestResolveCandidatesProviderDefaultErrorNoPhantom(t *testing.T) {
	database := newTestDB(t)
	// Provider-default pool is dead (non-strict): must yield a single direct
	// fallback, not [phantom, direct].
	insertPool(t, database, "pdef4", "pdef4", "http", "http://pdef4.example:8080", "", "", true, "error")
	defaults := map[string]map[string]any{"openai": {"proxyPoolId": "pdef4"}}
	raw, _ := json.Marshal(defaults)
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, "provider_proxy_defaults", string(raw), time.Now().Unix()); err != nil {
		t.Fatalf("insert defaults: %v", err)
	}

	r := NewResolver(database)
	cands := r.ResolveCandidates("", "openai")
	if len(cands) != 1 || cands[0].Source != "direct-fallback" {
		t.Fatalf("expected single direct-fallback for dead provider-default pool, got %+v", cands)
	}
}

func TestRelayTypeRewritten(t *testing.T) {
	database := newTestDB(t)
	insertPool(t, database, "relay1", "relay1", "vercel", "https://relay1.vercel.app", "", "rauth123", true, "active")

	r := NewResolver(database)
	cfg := r.Resolve(`{"proxyPoolId":"relay1"}`, "")
	if cfg.ProxyURL != "https://relay1.vercel.app" {
		t.Fatalf("expected ProxyURL to hold relay URL, got: %+v", cfg)
	}
	if cfg.RelayURL != "https://relay1.vercel.app" || cfg.RelayAuth != "rauth123" {
		t.Fatalf("expected relay config to be populated, got: %+v", cfg)
	}
}
