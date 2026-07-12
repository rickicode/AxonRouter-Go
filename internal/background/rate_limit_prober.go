package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// RateLimitProber periodically checks connections in cooldown and resets them
// when they become available again (cooldown expired + probe succeeds).
// It also probes per-model locked models so that after proxy/IP rotation a
// previously-limited model recovers without waiting for the full TTL.
type RateLimitProber struct {
	once       sync.Once
	db         *sql.DB
	writeQueue *db.WriteQueue
	store      *connstate.Store
	elig       *connstate.EligibilityManager
	exhaustion *quota.ExhaustionCache
	registry   *executor.Registry
	resolver   *proxypool.Resolver
	stopCh     chan struct{}
}

func NewRateLimitProber(
	database *sql.DB,
	writeQueue *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	exhaustion *quota.ExhaustionCache,
	registry *executor.Registry,
	resolver *proxypool.Resolver,
) *RateLimitProber {
	return &RateLimitProber{
		db:         database,
		writeQueue: writeQueue,
		store:      store,
		elig:       elig,
		exhaustion: exhaustion,
		registry:   registry,
		resolver:   resolver,
		stopCh:     make(chan struct{}),
	}
}

func (p *RateLimitProber) Start(ctx context.Context) {
	p.once.Do(func() {
		go p.run(ctx)
	})
}

func (p *RateLimitProber) Stop() {
	close(p.stopCh)
}

func (p *RateLimitProber) run(ctx context.Context) {
	log.Println("background: rate-limit prober started (1 min interval)")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.check()
			p.probePerModelLocks()
		}
	}
}

// check finds oc connections with expired cooldown and probes them.
// Rows are loaded into a slice and the cursor is closed BEFORE any HTTP probes
// to avoid holding a pooled DB connection across network I/O.
func (p *RateLimitProber) check() {
	now := time.Now().Unix()
	rows, err := p.db.Query(`
		SELECT id, name
		FROM connections
		WHERE provider_type_id = 'oc'
		AND is_active = 1
		AND status IN ('rate_limited', 'quota_exhausted')
		AND cooldown_until IS NOT NULL
		AND cooldown_until <= ?
	`, now)
	if err != nil {
		return
	}

	// Scan all rows into a slice, then close the cursor immediately.
	type connRow struct{ id, name string }
	var candidates []connRow
	for rows.Next() {
		var r connRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			continue
		}
		candidates = append(candidates, r)
	}
	rows.Close()

	if len(candidates) == 0 {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, r := range candidates {
		// Probe OpenCode Free endpoint
		req, err := http.NewRequest("POST", "https://opencode.ai/zen/v1/chat/completions", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Connection recovered — reset connection-wide exhaustion only.
			// Per-model scoped marks expire via their own TTL so other models stay blocked.
			p.exhaustion.Clear(quota.ExhaustKey(r.id, ""))
			p.store.UpdateStatus(r.id, connstate.StatusReady)
			connID := r.id
			updatedAt := now
			p.writeQueue.Enqueue("rateLimitProber:recover", func(d *sql.DB) error {
				_, err := d.Exec(`UPDATE connections SET status='ready', cooldown_until=NULL, last_error=NULL, updated_at=? WHERE id=?`,
					updatedAt, connID)
				return err
			})
			p.elig.Update(p.store)
			log.Printf("rate-limit prober: %s (%s) recovered → ready", r.name, r.id[:8])
		}
	}
}

// connInfo holds connection + provider type data loaded for probing.
type connInfo struct {
	connID  string
	name    string
	apiKey  string
	token   string
	baseURL string
	format  string
	psd     string
}

// probePerModelLocks tests per-model locked models for oc/ag providers.
// For each connection with locked models, it sends a minimal test request for
// the locked model through the connection's proxy config. On success the model
// cooldown and scoped exhaustion are cleared so the model can route again.
// This is especially important for OC with proxy rotation: after an IP change
// the upstream may accept requests again before the cooldown TTL expires.
func (p *RateLimitProber) probePerModelLocks() {
	if p.registry == nil {
		return
	}

	// Collect all per-model locked models across all connections.
	type lockEntry struct {
		connID   string
		provider string
		modelID  string
		scope    string
	}
	var locks []lockEntry

	p.store.Range(func(connID string, cs *connstate.ConnectionState) bool {
		provider := cs.Prefix
		if !connstate.HasPerModelQuota(provider) {
			return true
		}

		// Collect locked models from cooldown.
		cooldowns := cs.ModelCooldowns()
		seen := make(map[string]bool, len(cooldowns))
		for modelID := range cooldowns {
			scope := connstate.ModelScope(provider, modelID)
			locks = append(locks, lockEntry{connID: connID, provider: provider, modelID: modelID, scope: scope})
			seen[scope] = true
		}

		// Also collect scoped exhaustion entries that may not have a model cooldown.
		for _, scope := range p.exhaustion.ScopesForConn(connID) {
			if !seen[scope] {
				// No model cooldown for this scope — use a canonical probe model.
				modelID := canonicalModelForScope(provider, scope)
				if modelID != "" {
					locks = append(locks, lockEntry{connID: connID, provider: provider, modelID: modelID, scope: scope})
				}
			}
		}
		return true
	})

	if len(locks) == 0 {
		return
	}

	// Group locks by connection to load credentials once per connection.
	connCache := make(map[string]*connInfo)
	for _, l := range locks {
		if _, ok := connCache[l.connID]; ok {
			continue
		}
		ci, err := p.loadConnProbe(l.connID)
		if err != nil {
			continue
		}
		connCache[l.connID] = ci
	}

	for _, l := range locks {
		ci := connCache[l.connID]
		if ci == nil {
			continue
		}

		exec, _, ok := p.registry.Get(l.provider)
		if !ok {
			continue
		}

		body := buildProbeBody(ci.format, l.modelID)

		// Build proxy context if resolver is available.
		ctx := context.Background()
		if p.resolver != nil {
			cfg := p.resolver.Resolve(ci.psd, l.provider)
			ctx = executor.ContextWithProxy(ctx, executor.ProxyConfig{
				Enabled:     cfg.Enabled,
				ProxyURL:    cfg.ProxyURL,
				NoProxy:     cfg.NoProxy,
				RelayURL:    cfg.RelayURL,
				RelayAuth:   cfg.RelayAuth,
				RelayType:   cfg.RelayType,
				StrictProxy: cfg.StrictProxy,
			})
		}

		psdMap := make(map[string]string)
		if ci.psd != "" {
			var tmp map[string]any
			if json.Unmarshal([]byte(ci.psd), &tmp) == nil {
				for k, v := range tmp {
					if s, ok := v.(string); ok {
						psdMap[k] = s
					}
				}
			}
		}

		streamResult, err := exec.ExecuteStream(ctx, &executor.Request{
			APIKey:               ci.apiKey,
			AccessToken:          ci.token,
			BaseURL:              ci.baseURL,
			Body:                 body,
			Provider:             l.provider,
			Model:                l.modelID,
			ProviderSpecificData: psdMap,
		})
		if err != nil {
			det := connstate.DetectError(0, "", err, l.provider, l.modelID, nil)
			if det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota {
				// Model is still locked — update cooldown with fresh detection.
				cs := p.store.Get(l.connID)
				if cs != nil && det.CooldownUntil != nil {
					cs.SetModelCooldown(l.modelID, *det.CooldownUntil)
					scope := connstate.ModelScope(l.provider, l.modelID)
					p.exhaustion.MarkExhausted(quota.ExhaustKey(l.connID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
				}
			}
			continue
		}

		// Read first chunk to verify connectivity.
		var probeErr error
		for chunk := range streamResult.Chunks {
			if chunk.Err != nil {
				probeErr = chunk.Err
				break
			}
		}
		if probeErr != nil {
			det := connstate.DetectError(0, "", probeErr, l.provider, l.modelID, nil)
			if det.Category == connstate.ErrorRateLimit || det.Category == connstate.ErrorQuota {
				cs := p.store.Get(l.connID)
				if cs != nil && det.CooldownUntil != nil {
					cs.SetModelCooldown(l.modelID, *det.CooldownUntil)
					scope := connstate.ModelScope(l.provider, l.modelID)
					p.exhaustion.MarkExhausted(quota.ExhaustKey(l.connID, scope), quota.TTLFromCooldown(det.CooldownUntil, 5*time.Minute))
				}
			}
			continue
		}

		// Probe succeeded — clear model lock.
		cs := p.store.Get(l.connID)
		if cs != nil {
			cs.ClearModelCooldown(l.modelID)
		}
		p.exhaustion.Clear(quota.ExhaustKey(l.connID, l.scope))
		p.persistModelRecovery(l.connID, l.modelID)
		p.elig.Update(p.store)
		log.Printf("model prober: %s/%s recovered via probe on %s", l.provider, l.modelID, l.connID[:8])
	}
}

// loadConnProbe loads connection + provider type info for probing.
func (p *RateLimitProber) loadConnProbe(connID string) (*connInfo, error) {
	var ci connInfo
	var psd sql.NullString
	err := p.db.QueryRow(`
		SELECT c.id, c.name, COALESCE(c.api_key,''), COALESCE(c.oauth_token,''),
		       COALESCE(pt.base_url,''), pt.format, c.provider_specific_data
		FROM connections c
		JOIN provider_types pt ON c.provider_type_id = pt.id
		WHERE c.id = ? AND c.is_active = 1
	`, connID).Scan(&ci.connID, &ci.name, &ci.apiKey, &ci.token, &ci.baseURL, &ci.format, &psd)
	if err != nil {
		return nil, err
	}
	if psd.Valid {
		ci.psd = psd.String
	}
	return &ci, nil
}

// persistModelRecovery writes a last_success_at for the connection so the
// dashboard reflects the recovery without changing connection status.
func (p *RateLimitProber) persistModelRecovery(connID, modelID string) {
	now := time.Now().Unix()
	p.writeQueue.Enqueue("modelProber:recover", func(d *sql.DB) error {
		_, err := d.Exec(`UPDATE connections SET last_success_at = ?, updated_at = ? WHERE id = ?`, now, now, connID)
		return err
	})
}

// canonicalModelForScope returns a representative model name for a scope
// that has no model cooldown entry. For exact-model scopes the scope itself
// is the model. For family scopes, returns a canonical model name.
func canonicalModelForScope(provider, scope string) string {
	if strings.HasPrefix(scope, "family:") {
		family := strings.TrimPrefix(scope, "family:")
		switch family {
		case "gemini":
			return "gemini-3.5"
		case "claude":
			return "claude-sonnet-4-6"
		default:
			return ""
		}
	}
	return scope // exact model scope
}

// buildProbeBody constructs a minimal test request body matching the provider's
// native API format. Uses max_tokens=5 to minimise cost.
func buildProbeBody(format, model string) []byte {
	switch executor.ProviderFormat(format) {
	case executor.FormatClaude:
		body := map[string]any{
			"model":      model,
			"max_tokens": 5,
			"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		}
		b, _ := json.Marshal(body)
		return b
	case executor.FormatGemini, executor.FormatAntigravity:
		body := map[string]any{
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "hi"}}},
			},
			"generationConfig": map[string]any{"maxOutputTokens": 5},
		}
		b, _ := json.Marshal(body)
		return b
	default:
		body := map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": "hi"}},
			"max_tokens": 5,
		}
		b, _ := json.Marshal(body)
		return b
	}
}
