package proxypool

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
)

const (
	TypeHTTP       = "http"
	TypeVercel     = "vercel"
	TypeDeno       = "deno"
	TypeCloudflare = "cloudflare"
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
	Source       string
	ProxyPoolID  string
	Enabled      bool
	ProxyURL     string
	NoProxy      string
	StrictProxy  bool
	RelayURL     string
	RelayAuth    string
	RelayType    string
}

type Resolver struct {
	db     *sql.DB
	mu     sync.Mutex
	rr     map[string]uint64
	sticky map[string]stickyState
}

type stickyState struct {
	PoolID string
	Count  int
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

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db, rr: map[string]uint64{}, sticky: map[string]stickyState{}}
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

func (r *Resolver) providerDefault(providerID string) (Config, bool) {
	var raw string
	if err := r.db.QueryRow("SELECT value FROM settings WHERE key = 'provider_proxy_defaults'").Scan(&raw); err != nil || raw == "" {
		return Config{}, false
	}
	var defaults map[string]map[string]any
	if json.Unmarshal([]byte(raw), &defaults) != nil {
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
	cfg := Config{Source: source, ProxyPoolID: p.ID, Enabled: true, ProxyURL: p.ProxyURL, NoProxy: p.NoProxy, StrictProxy: strict}
	if IsRelayType(p.Type) {
		cfg.RelayURL, cfg.RelayAuth, cfg.RelayType = p.ProxyURL, p.RelayAuth, p.Type
	}
	return cfg, true
}

func (r *Resolver) resolveGroup(id, source string) (Config, bool) {
	var mode, idsJSON string
	var stickyLimit, strictInt, activeInt int
	if err := r.db.QueryRow("SELECT mode, sticky_limit, strict_proxy, proxy_pool_ids, is_active FROM proxy_groups WHERE id = ?", id).Scan(&mode, &stickyLimit, &strictInt, &idsJSON, &activeInt); err != nil || activeInt == 0 {
		return Config{}, false
	}
	var ids []string
	_ = json.Unmarshal([]byte(idsJSON), &ids)
	active, hasError := r.activePools(ids)
	if len(active) == 0 {
		if hasError && strictInt == 0 {
			return Config{Source: source}, true
		}
		return Config{}, false
	}
	selected := r.pick(id, mode, stickyLimit, active)
	return r.resolvePool(selected, source, strictInt != 0)
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
	if mode != "sticky" {
		i := r.rr[groupID] % uint64(len(ids))
		r.rr[groupID]++
		return ids[i]
	}
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
}

func (r *Resolver) getPool(id string) (pool, bool) {
	var p pool
	var active int
	err := r.db.QueryRow("SELECT id, type, proxy_url, no_proxy, relay_auth, is_active, test_status FROM proxy_pools WHERE id = ?", id).Scan(&p.ID, &p.Type, &p.ProxyURL, &p.NoProxy, &p.RelayAuth, &active, &p.TestStatus)
	p.IsActive = active != 0
	return p, err == nil
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
