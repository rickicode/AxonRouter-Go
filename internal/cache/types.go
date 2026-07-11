package cache

import "time"

// CacheStorage is the interface for response cache implementations.
type CacheStorage interface {
	Get(key string) (CacheEntry, bool)
	Set(key string, entry CacheEntry)
	Flush()
	Stats() CacheStats
}

// CacheEntry holds a cached response.
type CacheEntry struct {
	Body        []byte
	StatusCode  int
	ContentType string
	CreatedAt   time.Time
}

// CacheStats reports cache health.
type CacheStats struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
	Size   int    `json:"size"`
}

// HitRate returns the cache hit rate as a percentage.
func (s CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}
