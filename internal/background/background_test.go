package background

import (
	"context"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

func TestBackgroundStoppersAreIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)

	cleanup := NewCleanup(nil, nil, 30)
	cleanup.Start(ctx)
	quotaScheduler := NewQuotaScheduler(store, elig, 1)
	quotaScheduler.Start(ctx)
	rateLimiter := NewRateLimitProber(nil, nil, store, elig, quota.NewExhaustionCache(), executor.GetRegistry(), proxypool.NewResolver(nil))
	rateLimiter.Start(ctx)

	// Calling Stop twice must not panic.
	cleanup.Stop()
	cleanup.Stop()
	quotaScheduler.Stop()
	quotaScheduler.Stop()
	rateLimiter.Stop()
	rateLimiter.Stop()
}
