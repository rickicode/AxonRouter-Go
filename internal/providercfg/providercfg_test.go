package providercfg

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

func swapReadFileHook(fn func(string) ([]byte, error)) func() {
	orig := readFileHook
	readFileHook = fn
	return func() { readFileHook = orig }
}

func TestGet_CachesInMemoryAfterFirstDiskRead(t *testing.T) {
	dir := t.TempDir()

	var reads atomic.Int64
	restore := swapReadFileHook(func(name string) ([]byte, error) {
		reads.Add(1)
		return os.ReadFile(name)
	})
	defer restore()

	m := NewManager(dir)
	if err := m.Save("cached-provider", ProviderSettings{RoutingMode: RoundRobin}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Force a cold load on the next Get.
	m.mu.Lock()
	delete(m.settings, "cached-provider")
	m.mu.Unlock()

	s1, err := m.Get("cached-provider")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if s1.RoutingMode != RoundRobin {
		t.Fatalf("RoutingMode = %q, want %q", s1.RoutingMode, RoundRobin)
	}

	s2, err := m.Get("cached-provider")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if reads.Load() != 1 {
		t.Fatalf("expected exactly 1 disk read for two Gets, got %d", reads.Load())
	}
	if s1 != s2 {
		t.Fatalf("expected identical settings, got %+v and %+v", s1, s2)
	}
}

func TestGet_SingleflightCollapsesConcurrentColdReads(t *testing.T) {
	dir := t.TempDir()

	var reads atomic.Int64
	restore := swapReadFileHook(func(name string) ([]byte, error) {
		reads.Add(1)
		return os.ReadFile(name)
	})
	defer restore()

	m := NewManager(dir)
	if err := m.Save("cold-provider", ProviderSettings{RoutingMode: FirstEligible}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	m.mu.Lock()
	delete(m.settings, "cold-provider")
	m.mu.Unlock()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s, err := m.Get("cold-provider")
			if err != nil {
				errCh <- err.Error()
				return
			}
			if s.RoutingMode != FirstEligible {
				errCh <- "unexpected routing mode"
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for msg := range errCh {
		t.Fatalf("concurrent Get problem: %s", msg)
	}

	if reads.Load() != 1 {
		t.Fatalf("expected exactly 1 disk read for %d concurrent cold Gets, got %d", n, reads.Load())
	}
}

func BenchmarkGet_ParallelColdLoad(b *testing.B) {
	dir := b.TempDir()

	var reads atomic.Int64
	restore := swapReadFileHook(func(name string) ([]byte, error) {
		reads.Add(1)
		return os.ReadFile(name)
	})
	defer restore()

	m := NewManager(dir)
	if err := m.Save("bench-provider", ProviderSettings{RoutingMode: RoundRobin}); err != nil {
		b.Fatalf("Save failed: %v", err)
	}

	reads.Store(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.mu.Lock()
			delete(m.settings, "bench-provider")
			m.mu.Unlock()
			_, _ = m.Get("bench-provider")
		}
	})
	b.StopTimer()

	b.ReportMetric(float64(reads.Load())/float64(b.N), "reads/op")
}
