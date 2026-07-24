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

func TestNextRoundRobinIndexPerModelCursor(t *testing.T) {
	m := NewManager(t.TempDir())

	provider := "openai"
	modelA := "gpt-4o"
	modelB := "gpt-3.5-turbo"
	total := 3

	if got := m.NextRoundRobinIndex(provider, modelA, total); got != 0 {
		t.Fatalf("first index for modelA = %d, want 0", got)
	}

	// modelB must start from its own cursor, not inherit modelA's counter.
	if got := m.NextRoundRobinIndex(provider, modelB, total); got != 0 {
		t.Fatalf("first index for modelB = %d, want 0 (cursor should be independent)", got)
	}

	wantA := []int{1, 2, 0, 1}
	for _, want := range wantA {
		if got := m.NextRoundRobinIndex(provider, modelA, total); got != want {
			t.Fatalf("modelA index = %d, want %d", got, want)
		}
	}

	// modelB should have advanced only once so far.
	if got := m.NextRoundRobinIndex(provider, modelB, total); got != 1 {
		t.Fatalf("modelB index after skip = %d, want 1", got)
	}
}

func TestNextRoundRobinIndexIndependentPerProvider(t *testing.T) {
	m := NewManager(t.TempDir())

	model := "gpt-4o"
	total := 2

	m.NextRoundRobinIndex("openai", model, total)
	if got := m.NextRoundRobinIndex("openai", model, total); got != 1 {
		t.Fatalf("openai cursor = %d, want 1", got)
	}

	// A different provider with the same model name should start fresh.
	if got := m.NextRoundRobinIndex("cx", model, total); got != 0 {
		t.Fatalf("cx cursor = %d, want 0", got)
	}
}

func TestValidRoutingModes_IncludesAffinity(t *testing.T) {
	modes := ValidRoutingModes()
	have := false
	for _, m := range modes {
		if m == Affinity {
			have = true
		}
		if m == "" {
			t.Fatalf("empty routing mode in ValidRoutingModes")
		}
	}
	if !have {
		t.Fatalf("expected Affinity in ValidRoutingModes, got %v", modes)
	}
}

func TestAffinity_Value(t *testing.T) {
	if Affinity != "affinity" {
		t.Fatalf("Affinity = %q, want affinity", Affinity)
	}
}

func intPtr(n int) *int { return &n }

func TestHoldback_Defaults(t *testing.T) {
	m := NewManager(t.TempDir())
	ms, bytes := m.Holdback("openai")
	if ms != DefaultHoldbackMs {
		t.Fatalf("Holdback ms = %d, want %d", ms, DefaultHoldbackMs)
	}
	if bytes != DefaultHoldbackBytes {
		t.Fatalf("Holdback bytes = %d, want %d", bytes, DefaultHoldbackBytes)
	}
}

func TestHoldback_PerProviderSettings(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Save("cx", ProviderSettings{
		RoutingMode:   RoundRobin,
		HoldbackMs:    intPtr(50),
		HoldbackBytes: intPtr(1024),
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	ms, bytes := m.Holdback("cx")
	if ms != 50 {
		t.Fatalf("Holdback ms = %d, want 50", ms)
	}
	if bytes != 1024 {
		t.Fatalf("Holdback bytes = %d, want 1024", bytes)
	}
	// A different provider without overrides should keep defaults.
	ms, bytes = m.Holdback("openai")
	if ms != DefaultHoldbackMs || bytes != DefaultHoldbackBytes {
		t.Fatalf("unconfigured provider holdback = %d/%d, want defaults", ms, bytes)
	}
}

func TestHoldback_EnvOverride(t *testing.T) {
	t.Setenv("AXON_RESPONSES_HOLDBACK_MS", "100")
	t.Setenv("AXON_RESPONSES_HOLDBACK_BYTES", "2048")
	m := NewManager(t.TempDir())
	ms, bytes := m.Holdback("openai")
	if ms != 100 {
		t.Fatalf("Holdback ms = %d, want 100", ms)
	}
	if bytes != 2048 {
		t.Fatalf("Holdback bytes = %d, want 2048", bytes)
	}
}
