package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

//go:embed models.json
var embeddedModelsJSON []byte

const (
	refreshInterval = 3 * time.Hour
	fetchTimeout    = 15 * time.Second
)

var remoteURLs = []string{
	"https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json",
	"https://models.router-for.me/models.json",
}

// modelEntry is a single model definition from models.json.
type modelEntry struct {
	ID string `json:"id"`
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
	mu.Lock()
	current = c
	mu.Unlock()
}

// GetModelIDs returns model IDs (without provider prefix) for a provider key.
// The key matches models.json top-level keys: "claude", "codex-free", "codex-pro",
// "gemini", "antigravity", "kimi", "xai", etc.
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

// StartUpdater starts a background goroutine that refreshes the model catalog
// from remote URLs every 3 hours. Safe to call multiple times; only one runs.
func StartUpdater(ctx context.Context) {
	once.Do(func() {
		startTime = time.Now()
		go run(ctx)
	})
}

func run(ctx context.Context) {
	// Fetch immediately on startup
	tryFetch(ctx)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryFetch(ctx)
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
		mu.Lock()
		current = c
		mu.Unlock()
		log.Printf("model catalog updated from %s (%d providers)", url, len(c))
		return
	}
	log.Printf("WARN: all model catalog remote URLs failed, using embedded fallback")
}

func fetchCatalog(ctx context.Context, url string) (catalog, error) {
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

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
	if len(c) == 0 {
		return nil, fmt.Errorf("empty catalog")
	}
	return c, nil
}
