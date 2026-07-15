package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/modalities"
)

//go:embed models.json
var embeddedModelsJSON []byte

const (
	refreshInterval = 3 * time.Hour
	providerSyncInterval = 24 * time.Hour
	fetchTimeout = 15 * time.Second
	cfDiscoveryTTL = 5 * time.Minute
	openRouterDiscoveryTTL = 5 * time.Minute

	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
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

type cfDiscoveryCacheState struct {
	mu   sync.Mutex
	last time.Time
}

var (
	mu sync.RWMutex
	current catalog
	once sync.Once
	startTime time.Time

	cfDiscoveryCache cfDiscoveryCacheState

	openRouterDiscoveryCache struct {
		mu  sync.Mutex
		last time.Time
	}
)

func resetCloudflareDiscoveryCache() {
	cfDiscoveryCache.mu.Lock()
	cfDiscoveryCache.last = time.Time{}
	cfDiscoveryCache.mu.Unlock()
}

// DiscoverCloudflareModelsCached fetches Cloudflare Workers AI models and merges
// them into the shared catalog, but only if the cached entry has expired. This
// prevents every /v1/models request from hitting Cloudflare's API.
func DiscoverCloudflareModelsCached(apiKey, accountID string) {
	cfDiscoveryCache.mu.Lock()
	defer cfDiscoveryCache.mu.Unlock()
	if time.Since(cfDiscoveryCache.last) < cfDiscoveryTTL {
		return
	}
	ids, kinds, err := FetchCloudflareModels(apiKey, accountID)
	if err != nil || len(ids) == 0 {
		return
	}
	cfDiscoveryCache.last = time.Now()
	MergeProviderModelIDs("cf", ids, kinds)
}

func resetOpenRouterDiscoveryCache() {
	openRouterDiscoveryCache.mu.Lock()
	openRouterDiscoveryCache.last = time.Time{}
	openRouterDiscoveryCache.mu.Unlock()
}

// DiscoverOpenRouterModelsCached fetches OpenRouter's public model list and
// merges free models (prompt + completion pricing == 0) into the shared
// catalog under the "openrouter" key. Results are cached for five minutes so
// /v1/models stays fast and does not hammer the upstream endpoint.
func DiscoverOpenRouterModelsCached() {
	openRouterDiscoveryCache.mu.Lock()
	defer openRouterDiscoveryCache.mu.Unlock()
	if time.Since(openRouterDiscoveryCache.last) < openRouterDiscoveryTTL {
		return
	}
	ids, err := fetchOpenRouterFreeModels()
	if err != nil || len(ids) == 0 {
		return
	}
	openRouterDiscoveryCache.last = time.Now()

	entries := make([]modelEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, modelEntry{ID: strings.TrimPrefix(id, "@"), ServiceKinds: []string{"llm"}})
	}
	mu.Lock()
	current["openrouter"] = mergeProviderEntries(current["openrouter"], entries)
	mu.Unlock()
}

func fetchOpenRouterFreeModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	setOpenRouterHeaders(req.Header)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter models returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
				Image      string `json:"image"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var ids []string
	for _, m := range envelope.Data {
		if m.ID == "" {
			continue
		}
		if !isOpenRouterFree(m.Pricing.Prompt, m.Pricing.Completion, m.Pricing.Image) {
			continue
		}
		id := strings.TrimPrefix(m.ID, "@")
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func isOpenRouterFree(prompt, completion, image string) bool {
	prompt = strings.TrimSpace(prompt)
	completion = strings.TrimSpace(completion)
	image = strings.TrimSpace(image)

	// Treat empty/missing pricing as non-free to be safe.
	if prompt == "" || completion == "" {
		return false
	}
	if prompt == "0" && completion == "0" {
		return true
	}
	pp, err := strconv.ParseFloat(prompt, 64)
	if err != nil {
		return false
	}
	cp, err := strconv.ParseFloat(completion, 64)
	if err != nil {
		return false
	}
	if pp != 0 || cp != 0 {
		return false
	}
	if image == "" || image == "0" {
		return true
	}
	ip, err := strconv.ParseFloat(image, 64)
	if err != nil {
		return false
	}
	return ip == 0
}

func setOpenRouterHeaders(h http.Header) {
	referer := os.Getenv("OPENROUTER_HTTP_REFERER")
	title := os.Getenv("OPENROUTER_X_TITLE")
	if referer == "" {
		referer = "https://endpoint-proxy.local"
	}
	if title == "" {
		title = "Endpoint Proxy"
	}
	h.Set("HTTP-Referer", referer)
	h.Set("X-Title", title)
}

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

// HasServiceKind reports whether the given model ID under a provider key is
// tagged with the requested service kind.
func HasServiceKind(providerKey, modelID, kind string) bool {
	for _, k := range GetModelServiceKinds(providerKey, modelID) {
		if k == kind {
			return true
		}
	}
	return false
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
	// Merge: overlay remote entries on top of the existing catalog per provider.
	// Local-only providers and local-only models are preserved, while remote
	// updates still refresh display names and add new models. Service kinds are
	// preserved when the remote entry does not include them.
	for k, v := range c {
		current[k] = mergeProviderEntries(current[k], v)
	}
	mu.Unlock()
		log.Printf("model catalog updated from %s (%d providers, %d total)", url, len(c), len(current))
		return
	}
	log.Printf("WARN: all model catalog remote URLs failed, using embedded fallback")
}

// MergeProviderModelIDs overlays discovered model IDs into the in-memory catalog
// under the given provider key. kindMap maps model ID -> service kinds. Existing
// entries not present in the IDs are kept; service kinds are preserved when the
// discovered entry omits them.
func MergeProviderModelIDs(providerKey string, ids []string, kindMap map[string][]string) {
	mu.Lock()
	defer mu.Unlock()
	fetched := make([]modelEntry, 0, len(ids))
	for _, id := range ids {
		fetched = append(fetched, modelEntry{ID: id, ServiceKinds: kindMap[id]})
	}
	current[providerKey] = mergeProviderEntries(current[providerKey], fetched)
}

// cfTaskServiceKinds maps Cloudflare Workers AI task names to our service kind tags.
var CFTaskServiceKinds = map[string][]string{
	"Text Generation":             {"llm"},
	"Text Embeddings":             {"embedding"},
	"Text-to-Image":               {"image"},
	"Image-to-Text":               {"imageToText"},
	"Image Classification":        {"imageToText"},
	"Speech-to-Text":              {"stt"},
	"Automatic Speech Recognition": {"stt"},
	"Text-to-Speech":              {"tts"},
	"Translation":                 {"llm"},
	"Text Classification":         {"llm"},
}

// FetchCloudflareModels queries the official Cloudflare Workers AI model search
// API and returns the callable gateway model slugs (e.g. "cf/meta/llama-3.3-70b-instruct")
// plus a map of slug -> service kinds inferred from the upstream task metadata.
func FetchCloudflareModels(apiKey, accountID string) ([]string, map[string][]string, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/models/search?per_page=1000", accountID)
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("cloudflare models search returned HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Success bool `json:"success"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result []struct {
			Name string `json:"name"`
			Task struct {
				Name string `json:"name"`
			} `json:"task"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, nil, err
	}
	if !envelope.Success {
		var msgs []string
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, nil, fmt.Errorf("cloudflare models search failed: %s", strings.Join(msgs, "; "))
	}

	seen := make(map[string]struct{})
	var out []string
	kinds := make(map[string][]string)
	for _, m := range envelope.Result {
		slug := strings.TrimPrefix(strings.TrimSpace(m.Name), "@")
		if slug == "" {
			continue
		}
		if !strings.HasPrefix(slug, "cf/") {
			slug = "cf/" + slug
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
		if k := CFTaskServiceKinds[m.Task.Name]; len(k) > 0 {
			kinds[slug] = k
		}
	}
	return out, kinds, nil
}

// mergeProviderEntries overlays fetched entries on top of existing ones by ID.
// Existing entries not present in the fetched list are kept. Service kinds are
// copied from the existing entry when the fetched entry omits them.
func mergeProviderEntries(existing, fetched []modelEntry) []modelEntry {
	merged := make(map[string]modelEntry, len(existing))
	for _, e := range existing {
		merged[e.ID] = e
	}
	for _, e := range fetched {
		if len(e.ServiceKinds) == 0 {
			if old, ok := merged[e.ID]; ok && len(old.ServiceKinds) > 0 {
				e.ServiceKinds = old.ServiceKinds
			}
		}
		merged[e.ID] = e
	}
	out := make([]modelEntry, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	return out
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
