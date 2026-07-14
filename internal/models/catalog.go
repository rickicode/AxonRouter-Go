package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/modalities"
)

//go:embed models.json
var embeddedModelsJSON []byte

const (
	refreshInterval      = 3 * time.Hour
	providerSyncInterval = 24 * time.Hour
	fetchTimeout         = 15 * time.Second
)

var remoteURLs = []string{
	"https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json",
	"https://models.router-for.me/models.json",
}

// providerEndpoints maps catalog keys to upstream /v1/models URLs.
// These are fetched periodically and merged into the in-memory catalog.
// Only add endpoints that work without authentication (no-auth / public).
// For providers requiring API keys, models are discovered dynamically
// via the ListModels handler when connections exist.
var providerEndpoints = map[string]string{
	// OpenCode Free: filter to -free suffix models only.
	"oc": "https://opencode.ai/zen/v1/models",
}

// providerFreeOnly filters models to only include those with "-free" suffix.
// Used for free-tier providers where the upstream returns all models (free + paid).
var providerFreeOnly = map[string]bool{
	"oc": true,
}

// modelEntry is a single model definition from models.json.
type modelEntry struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	ServiceKinds []string `json:"service_kinds"`
}

// catalog is the full models.json structure: provider → []modelEntry.
type catalog map[string][]modelEntry

var (
	mu        sync.RWMutex
	current   catalog
	once      sync.Once
	startTime time.Time
)

func init() {
	loadEmbedded()
}

func loadEmbedded() {
	var c catalog
	if err := json.Unmarshal(embeddedModelsJSON, &c); err != nil {
		log.Printf("WARN: failed to parse embedded models.json: %v", err)
		return
	}
	stripAtPrefix(c)
	mergeModalities(c)
	mu.Lock()
	current = c
	mu.Unlock()
}

// mergeModalities folds per-modality registry models into the catalog so they
// appear alongside static entries. Each model is tagged with its modality as a
// service kind (e.g. "embedding", "image").
func mergeModalities(c catalog) {
	for _, providerTypeID := range modalities.Providers() {
		kinds := modalities.ServiceKinds(providerTypeID)
		for _, kind := range kinds {
			for _, model := range modalities.Models(providerTypeID, kind) {
				id := strings.TrimPrefix(model, "@")
				idx := -1
				for i, e := range c[providerTypeID] {
					if e.ID == id {
						idx = i
						break
					}
				}
				if idx >= 0 {
					c[providerTypeID][idx].ServiceKinds = appendUnique(c[providerTypeID][idx].ServiceKinds, kind)
					continue
				}
				c[providerTypeID] = append(c[providerTypeID], modelEntry{ID: id, ServiceKinds: []string{kind}})
			}
		}
	}
}

func appendUnique(existing []string, value string) []string {
	for _, v := range existing {
		if v == value {
			return existing
		}
	}
	return append(existing, value)
}

// stripAtPrefix removes leading "@" from all model IDs in the catalog.
func stripAtPrefix(c catalog) {
	for _, entries := range c {
		for i := range entries {
			entries[i].ID = strings.TrimPrefix(entries[i].ID, "@")
		}
	}
}

// ProviderCount returns the number of provider keys currently loaded in the catalog.
func ProviderCount() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(current)
}

// ModelCount returns the total number of model entries across all providers.
func ModelCount() int {
	mu.RLock()
	defer mu.RUnlock()
	var total int
	for _, entries := range current {
		total += len(entries)
	}
	return total
}

// GetModelIDs returns model IDs (without provider prefix) for a provider key.
// The key matches models.json top-level keys: "claude", "codex-free", "codex-pro",
// "gemini", "antigravity", "kimi", "xai", "opencode", "mimocode", etc.
func GetModelIDs(providerKey string) []string {
	mu.RLock()
	defer mu.RUnlock()
	entries, ok := current[providerKey]
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}

// GetAllModelIDs returns the union of model IDs across multiple provider keys.
func GetAllModelIDs(keys ...string) []string {
	seen := make(map[string]struct{})
	var ids []string
	mu.RLock()
	defer mu.RUnlock()
	for _, key := range keys {
		for _, e := range current[key] {
			if _, ok := seen[e.ID]; !ok {
				seen[e.ID] = struct{}{}
				ids = append(ids, e.ID)
			}
		}
	}
	return ids
}

// ServiceKindsForModelID returns the explicit service kinds for a canonical
// model ID across all provider keys in the catalog. Returns nil if the model
// has no explicit service kinds.
func ServiceKindsForModelID(modelID string) []string {
	mu.RLock()
	defer mu.RUnlock()
	for _, entries := range current {
		for _, e := range entries {
			if e.ID == modelID && len(e.ServiceKinds) > 0 {
				out := make([]string, len(e.ServiceKinds))
				copy(out, e.ServiceKinds)
				return out
			}
		}
	}
	return nil
}

// GetModelDisplayNames returns a map of model ID → display_name for a provider key.
// Used by quota fetchers to map upstream model IDs to human-readable names.
func GetModelDisplayNames(providerKey string) map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	entries, ok := current[providerKey]
	if !ok {
		return nil
	}
	names := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			names[e.ID] = e.DisplayName
		}
	}
	return names
}

// GetModelServiceKinds returns the service kinds for a model ID under a provider key.
func GetModelServiceKinds(providerKey, modelID string) []string {
	mu.RLock()
	defer mu.RUnlock()
	entries, ok := current[providerKey]
	if !ok {
		return nil
	}
	for _, e := range entries {
		if e.ID == modelID {
			out := make([]string, len(e.ServiceKinds))
			copy(out, e.ServiceKinds)
			return out
		}
	}
	return nil
}

// StartUpdater starts a background goroutine that refreshes the model catalog
// from remote URLs every 3 hours, and syncs per-provider models every 24 hours.
// Safe to call multiple times; only one runs.
func StartUpdater(ctx context.Context) {
	once.Do(func() {
		startTime = time.Now()
		go run(ctx)
	})
}

// SyncNow triggers an immediate sync of per-provider models from upstream endpoints.
// Safe to call from API handlers — runs synchronously.
func SyncNow(ctx context.Context) {
	tryFetchProviders(ctx)
}

func run(ctx context.Context) {
	// Fetch full catalog immediately on startup
	tryFetch(ctx)
	// Also sync per-provider endpoints immediately
	tryFetchProviders(ctx)

	catalogTicker := time.NewTicker(refreshInterval)
	defer catalogTicker.Stop()
	providerTicker := time.NewTicker(providerSyncInterval)
	defer providerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-catalogTicker.C:
			tryFetch(ctx)
			tryFetchProviders(ctx) // re-merge per-provider models after full refresh
		case <-providerTicker.C:
			tryFetchProviders(ctx)
		}
	}
}

func tryFetch(ctx context.Context) {
	for _, url := range remoteURLs {
		c, err := fetchCatalog(ctx, url)
		if err != nil {
			log.Printf("WARN: model catalog fetch failed from %s: %v", url, err)
			continue
		}
		stripAtPrefix(c)
		mergeModalities(c)
	mu.Lock()
	// Merge: remote catalog updates existing keys and adds new ones,
	// but preserves local-only providers (mimocode, opencode, etc.)
	// that may not exist in the remote catalog yet.
	for k, v := range c {
		// Preserve explicit service kinds from the existing catalog when the
		// remote entry does not include them. Without this, periodic fetches
		// would strip modality tags from static/registry models.
		old := current[k]
		for i := range v {
			if len(v[i].ServiceKinds) != 0 {
				continue
			}
			for _, e := range old {
				if e.ID == v[i].ID && len(e.ServiceKinds) > 0 {
					v[i].ServiceKinds = e.ServiceKinds
					break
				}
			}
		}
		current[k] = v
	}
	mu.Unlock()
		log.Printf("model catalog updated from %s (%d providers, %d total)", url, len(c), len(current))
		return
	}
	log.Printf("WARN: all model catalog remote URLs failed, using embedded fallback")
}

// tryFetchProviders fetches models from per-provider upstream endpoints
// and merges them into the in-memory catalog. This keeps no-auth provider
// models (like opencode) up-to-date even without stored connections.
func tryFetchProviders(ctx context.Context) {
	for catalogKey, endpoint := range providerEndpoints {
		models, err := fetchProviderModels(ctx, endpoint)
		if err != nil {
			log.Printf("WARN: provider model sync failed for %s (%s): %v", catalogKey, endpoint, err)
			continue
		}
		if len(models) == 0 {
			continue
		}
		entries := make([]modelEntry, 0, len(models))
		for _, id := range models {
			// Filter: free-only providers only keep models with "-free" suffix
			if providerFreeOnly[catalogKey] && !strings.HasSuffix(id, "-free") {
				continue
			}
			entries = append(entries, modelEntry{ID: strings.TrimPrefix(id, "@")})
		}
		mu.Lock()
		current[catalogKey] = entries
		mu.Unlock()
		log.Printf("provider model sync: %s updated (%d models)", catalogKey, len(entries))
	}
}

// fetchProviderModels fetches model IDs from an OpenAI-compatible /v1/models endpoint.
// Returns model IDs or an error.
func fetchProviderModels(ctx context.Context, endpoint string) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse OpenAI-format: {"data": [{"id": "model-name"}, ...]}
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &modelsResp); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	ids := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// fetchCatalog fetches and parses the full catalog from a remote URL.
func fetchCatalog(ctx context.Context, url string) (catalog, error) {
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var c catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return c, nil
}
