package background

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// UsageFlush periodically logs the tracker's buffer state for monitoring.
// The actual flushing is handled by usage.Tracker's internal goroutine.
type UsageFlush struct {
	once sync.Once
	tracker *usage.Tracker
	interval time.Duration
	stopCh chan struct{}
	stopOnce sync.Once
}

// NewUsageFlush creates a new usage flush monitor.
// Monitors buffer every 30 seconds.
func NewUsageFlush(tracker *usage.Tracker) *UsageFlush {
	return &UsageFlush{
		tracker:  tracker,
		interval: 30 * time.Second,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (uf *UsageFlush) Start(ctx context.Context) {
	uf.once.Do(func() {
		go uf.run(ctx)
	})
}

func (uf *UsageFlush) run(ctx context.Context) {
	log.Println("background: usage flush monitor started")

	// Immediate first report
	uf.report()

	ticker := time.NewTicker(uf.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			uf.report()
		case <-ctx.Done():
			log.Println("background: usage flush monitor stopped")
			return
		case <-uf.stopCh:
			return
		}
	}
}

func (uf *UsageFlush) report() {
	bufLen := uf.tracker.BufferLen()
	if bufLen > 5000 {
		log.Printf("background: usage buffer HIGH: %d/10000 entries", bufLen)
	}
}

// Stop signals the monitor to stop.
func (uf *UsageFlush) Stop() {
	uf.stopOnce.Do(func() {
		close(uf.stopCh)
	})
}
