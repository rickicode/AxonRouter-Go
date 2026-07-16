package proxypool

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	mrand "math/rand"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	TypeHTTP       = "http"
	TypeVercel     = "vercel"
	TypeDeno       = "deno"
	TypeCloudflare = "cloudflare"

	DefaultCacheTTL = 30 * time.Second
)

var relayTypes = map[string]bool{TypeVercel: true, TypeDeno: true, TypeCloudflare: true}

func IsRelayType(t string) bool { return relayTypes[t] }

func DetectRelayType(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if strings.HasSuffix(h, ".vercel.app") || strings.HasSuffix(h, ".now.sh") {
		return TypeVercel
	}
	if strings.HasSuffix(h, ".deno.net") {
		return TypeDeno
	}
	if strings.HasSuffix(h, ".workers.dev") {
		return TypeCloudflare
	}
	return ""
}

func NormalizeType(value, proxyURL string) string {
	if value == TypeHTTP || IsRelayType(value) {
		return value
	}
	if detected := DetectRelayType(proxyURL); detected != "" {
		return detected
	}
	return TypeHTTP
}

func GenerateRelayAuth() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type Config struct {
	Source      string
	ProxyPoolID string
	Enabled     bool
	ProxyURL    string
	NoProxy     string
	StrictProxy bool
	RelayURL    string
	RelayAuth   string
	RelayType   string
}

type pool struct {
	ID         string
	Type       string
	ProxyURL   string
	NoProxy    string
	RelayAuth  string
	IsActive   bool
	TestStatus string
}

type stickyState struct {
	PoolID string
	Count  int
}

// cachedPool stores a pool row resolved from the DB.
type cachedPool struct {
	pool      pool
	fetchedAt time.Time
}

// cachedGroup stores a proxy group row resolved from the DB.
type cachedGroup struct {
	mode        string
	stickyLimit int
	strict      bool
	isActive    bool
	poolIDs     []string
	fetchedAt   time.Time
}

// cachedDefaults stores the provider proxy defaults setting JSON.
type cachedDefaults struct {
	defaults  map[string]map[string]any
	fetchedAt time.Time
}

// Resolver resolves proxy configuration for provider connections.
// It caches DB rows for pool/group/defaults to avoid per-request SQL queries.
type Resolver struct {
	db *sql.DB

	// rr/sticky state used by group selection strategies.
	mu     sync.Mutex
	rr     map[string]uint64
	sticky map[string]stickyState

	// cacheMu protects the caches below.
	cacheMu      sync.RWMutex
	cacheTTL     time.Duration
	pools        map[string]cachedPool
	groups       map[string]cachedGroup
	defaults     *cachedDefaults
	defaultsHit  bool
	defaultsWhen time.Time

	// onInvalidate, if set, is called whenever the cache is invalidated so
	// other subsystems (e.g. executor idle-connection pools) can react.
	onInvalidate func()
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{
		db:       db,
		rr:       map[string]uint64{},
		sticky:   map[string]stickyState{},
		cacheTTL: DefaultCacheTTL,
		pools:    map[string]cachedPool{},
		groups:   map[string]cachedGroup{},
	}
}

// SetCacheTTL allows tests or callers to configure the cache TTL.
func (r *Resolver) SetCacheTTL(d time.Duration) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cacheTTL = d
}

// SetOnInvalidate registers a callback invoked on every Invalidate call.
func (r *Resolver) SetOnInvalidate(fn func()) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.onInvalidate = fn
}

// Invalidate clears all cached pools/groups/defaults so the next lookup reads from DB.
func (r *Resolver) Invalidate() {
	r.cacheMu.Lock()
	r.pools = map[string]cachedPool{}
	r.groups = map[string]cachedGroup{}
	r.defaults = nil
	r.defaultsHit = false
	r.cacheMu.Unlock()
	if r.onInvalidate != nil {
		r.onInvalidate()
	}
}

func (r *Resolver) Resolve(providerSpecificData, providerID string) Config {
	var psd map[string]any
	_ = json.Unmarshal([]byte(providerSpecificData), &psd)

	if id := cleanID(psd["proxyGroupId"]); id != "" {
		if cfg, ok := r.resolveGroup(id, "connection-group"); ok {
			return cfg
		}
	}

	if id := cleanID(psd["proxyPoolId"]); id != "" {
		if cfg, ok := r.resolvePool(id, "connection-pool", false); ok {
			return cfg
		}
	}

	if providerID != "" {
		if cfg, ok := r.providerDefault(providerID); ok {
			return cfg
		}
	}

	return Config{Source: "none", ProxyPoolID: cleanID(psd["proxyPoolId"])}
}

// ResolveCandidates returns an ordered list of proxy configurations to try for
// a request: the primary resolution first, then alternative pools (when the
// connection uses a proxy group), and finally a direct fallback (unless a
// strict proxy was selected). The executor retries across these on transient
// proxy/network failures.
func (r *Resolver) ResolveCandidates(providerSpecificData, providerID string) []Config {
	var psd map[string]any
	_ = json.Unmarshal([]byte(providerSpecificData), &psd)

	var cands []Config
	if id := cleanID(psd["proxyGroupId"]); id != "" {
		if g, ok := r.getGroup(id); ok && g.isActive {
			active, _ := r.activePools(g.poolIDs)
			for _, pid := range active {
				if cfg, ok := r.resolvePool(pid, "connection-group", g.strict); ok {
					cands = append(cands, cfg)
				}
			}
		}
	}
	if len(cands) == 0 {
		if id := cleanID(psd["proxyPoolId"]); id != "" {
			if cfg, ok := r.resolvePool(id, "connection-pool", false); ok {
				cands = append(cands, cfg)
			}
		}
	}
	if len(cands) == 0 && providerID != "" {
		if cfg, ok := r.providerDefault(providerID); ok {
			cands = append(cands, cfg)
		}
	}

	// Direct fallback unless the selected proxy is strict.
	// MiMoCode aggressively rate-limits/bans the server's direct IP; if a proxy
	// is configured for a MiMoCode connection, never fall back to direct.
	strict := len(cands) > 0 && cands[0].StrictProxy
	noDirect := providerID == "mimocode" && len(cands) > 0
	if !strict && !noDirect {
		cands = append(cands, Config{Source: "direct-fallback"})
	}
	return cands
}

func (r *Resolver) providerDefault(providerID string) (Config, bool) {
	defaults, ok := r.getDefaults()
	if !ok {
		return Config{}, false
	}

	cfg := defaults[providerID]
	if id := cleanID(cfg["proxyGroupId"]); id != "" {
		if out, ok := r.resolveGroup(id, "provider-default-group"); ok {
			return out, true
		}
	}
	if id := cleanID(cfg["proxyPoolId"]); id != "" {
		return r.resolvePool(id, "provider-default-pool", false)
	}
	return Config{}, false
}

func (r *Resolver) resolvePool(id, source string, strict bool) (Config, bool) {
	p, ok := r.getPool(id)
	if !ok || !p.IsActive || strings.TrimSpace(p.ProxyURL) == "" {
		return Config{}, false
	}
	// Pools that failed the health check should not be used unless forced.
	if p.TestStatus == "error" {
		if strict {
			return Config{}, false
		}
		// Non-strict: fall back to direct connection.
		return Config{Source: source}, true
	}

	cfg := Config{Source: source, ProxyPoolID: p.ID, Enabled: true, ProxyURL: p.ProxyURL, NoProxy: p.NoProxy, StrictProxy: strict}
	if IsRelayType(p.Type) {
		cfg.RelayURL, cfg.RelayAuth, cfg.RelayType = p.ProxyURL, p.RelayAuth, p.Type
	}
	return cfg, true
}

func (r *Resolver) resolveGroup(id, source string) (Config, bool) {
	g, ok := r.getGroup(id)
	if !ok || !g.isActive {
		return Config{}, false
	}

	active, hasError := r.activePools(g.poolIDs)
	if len(active) == 0 {
		if hasError && !g.strict {
			return Config{Source: source}, true
		}
		return Config{}, false
	}
	selected := r.pick(id, g.mode, g.stickyLimit, active)
	return r.resolvePool(selected, source, g.strict)
}

func (r *Resolver) activePools(ids []string) ([]string, bool) {
	active := make([]string, 0, len(ids))
	hasError := false
	for _, id := range ids {
		p, ok := r.getPool(id)
		if !ok || !p.IsActive || strings.TrimSpace(p.ProxyURL) == "" {
			continue
		}
		if p.TestStatus == "error" {
			hasError = true
			continue
		}
		active = append(active, id)
	}
	return active, hasError
}

func (r *Resolver) pick(groupID, mode string, limit int, ids []string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch mode {
	case "sticky":
		if limit < 1 {
			limit = 1
		}
		st := r.sticky[groupID]
		if st.PoolID != "" && st.Count < limit && containsID(ids, st.PoolID) {
			st.Count++
			r.sticky[groupID] = st
			return st.PoolID
		}
		next := ids[0]
		if st.PoolID != "" {
			for i, id := range ids {
				if id == st.PoolID {
					next = ids[(i+1)%len(ids)]
					break
				}
			}
		}
		r.sticky[groupID] = stickyState{PoolID: next, Count: 1}
		return next
	case "random":
		return ids[mrand.Intn(len(ids))]
	default:
		i := r.rr[groupID] % uint64(len(ids))
		r.rr[groupID]++
		return ids[i]
	}
}

// getPool returns a pool from cache or DB.
func (r *Resolver) getPool(id string) (pool, bool) {
	if id == "" {
		return pool{}, false
	}
	now := time.Now()
	r.cacheMu.RLock()
	if c, ok := r.pools[id]; ok && now.Sub(c.fetchedAt) < r.cacheTTL {
		r.cacheMu.RUnlock()
		return c.pool, true
	}
	r.cacheMu.RUnlock()

	var p pool
	var active int
	err := r.db.QueryRow("SELECT id, type, proxy_url, no_proxy, relay_auth, is_active, test_status FROM proxy_pools WHERE id = ?", id).Scan(
		&p.ID, &p.Type, &p.ProxyURL, &p.NoProxy, &p.RelayAuth, &active, &p.TestStatus,
	)
	p.IsActive = active != 0
	if err != nil {
		return pool{}, false
	}

	r.cacheMu.Lock()
	r.pools[id] = cachedPool{pool: p, fetchedAt: time.Now()}
	r.cacheMu.Unlock()
	return p, true
}

// getGroup returns a proxy group from cache or DB.
func (r *Resolver) getGroup(id string) (cachedGroup, bool) {
	if id == "" {
		return cachedGroup{}, false
	}
	now := time.Now()
	r.cacheMu.RLock()
	if g, ok := r.groups[id]; ok && now.Sub(g.fetchedAt) < r.cacheTTL {
		r.cacheMu.RUnlock()
		return g, true
	}
	r.cacheMu.RUnlock()

	var mode, idsJSON string
	var stickyLimit, strictInt, activeInt int
	if err := r.db.QueryRow("SELECT mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active FROM proxy_groups WHERE id = ?", id).Scan(
		&mode, &stickyLimit, &strictInt, &idsJSON, &activeInt,
	); err != nil {
		return cachedGroup{}, false
	}
	var ids []string
	_ = json.Unmarshal([]byte(idsJSON), &ids)
	if ids == nil {
		ids = []string{}
	}
	g := cachedGroup{
		mode:        mode,
		stickyLimit: stickyLimit,
		strict:      strictInt != 0,
		isActive:    activeInt != 0,
		poolIDs:     ids,
		fetchedAt:   time.Now(),
	}
	r.cacheMu.Lock()
	r.groups[id] = g
	r.cacheMu.Unlock()
	return g, true
}

// getDefaults returns provider proxy defaults from cache or DB.
func (r *Resolver) getDefaults() (map[string]map[string]any, bool) {
	now := time.Now()
	r.cacheMu.RLock()
	if r.defaults != nil && now.Sub(r.defaults.fetchedAt) < r.cacheTTL {
		r.cacheMu.RUnlock()
		return r.defaults.defaults, true
	}
	if r.defaultsHit && now.Sub(r.defaultsWhen) < r.cacheTTL {
		r.cacheMu.RUnlock()
		return nil, false
	}
	r.cacheMu.RUnlock()

	var raw string
	if err := r.db.QueryRow("SELECT value FROM settings WHERE key = 'provider_proxy_defaults'").Scan(&raw); err != nil || raw == "" {
		r.cacheMu.Lock()
		r.defaultsHit = true
		r.defaultsWhen = time.Now()
		r.cacheMu.Unlock()
		return nil, false
	}
	var defaults map[string]map[string]any
	if json.Unmarshal([]byte(raw), &defaults) != nil {
		r.cacheMu.Lock()
		r.defaultsHit = true
		r.defaultsWhen = time.Now()
		r.cacheMu.Unlock()
		return nil, false
	}

	r.cacheMu.Lock()
	r.defaults = &cachedDefaults{defaults: defaults, fetchedAt: time.Now()}
	r.defaultsHit = true
	r.defaultsWhen = r.defaults.fetchedAt
	r.cacheMu.Unlock()
	return defaults, true
}

func cleanID(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "__none__" {
		return ""
	}
	return s
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
