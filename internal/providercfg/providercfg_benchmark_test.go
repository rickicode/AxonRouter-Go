package providercfg

import (
	"testing"
)

func BenchmarkRoutingMode_Default(b *testing.B) {
	m := NewManager(b.TempDir())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.RoutingMode("openai")
	}
}

func BenchmarkRoutingMode_Saved(b *testing.B) {
	m := NewManager(b.TempDir())
	if err := m.Save("openai", ProviderSettings{RoutingMode: RoundRobin}); err != nil {
		b.Fatalf("save settings: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.RoutingMode("openai")
	}
}

func BenchmarkNextRoundRobinIndex(b *testing.B) {
	m := NewManager(b.TempDir())
	if err := m.Save("openai", ProviderSettings{RoutingMode: RoundRobin}); err != nil {
		b.Fatalf("save settings: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.NextRoundRobinIndex("openai", 10)
	}
}
