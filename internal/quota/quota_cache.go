package quota

import (
	"sync"
	"time"
)

// quotaCacheTTL is how long a cached ConnectionQuota remains fresh.
const quotaCacheTTL = 60 * time.Second

// cachedQuota holds a ConnectionQuota and the time it was fetched.
type cachedQuota struct {
	cq        ConnectionQuota
	fetchedAt time.Time
}

var quotaCache sync.Map // string connectionID -> cachedQuota

// ClearQuotaCache removes the cached quota for a connection, or all cached
// entries if connID is empty.
func ClearQuotaCache(connID string) {
	if connID == "" {
		quotaCache.Range(func(key, value any) bool {
			quotaCache.Delete(key)
			return true
		})
		return
	}
	quotaCache.Delete(connID)
}

func getCachedQuota(connID string) (ConnectionQuota, bool) {
	if v, ok := quotaCache.Load(connID); ok {
		cached := v.(cachedQuota)
		if time.Since(cached.fetchedAt) < quotaCacheTTL {
			return cached.cq, true
		}
	}
	return ConnectionQuota{}, false
}

func setCachedQuota(connID string, cq ConnectionQuota) {
	cached := cachedQuota{cq: cq, fetchedAt: time.Now()}
	quotaCache.Store(connID, cached)
}
