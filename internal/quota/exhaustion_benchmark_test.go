package quota

import (
	"testing"
	"time"
)

func BenchmarkExhaustKey_Connection(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExhaustKey("conn-abc123", "")
	}
}

func BenchmarkExhaustKey_Scope(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExhaustKey("conn-abc123", "gpt-4o")
	}
}

func BenchmarkIsExhausted_Hit(b *testing.B) {
	cache := NewExhaustionCache()
	key := ExhaustKey("conn-abc123", "")
	cache.MarkExhausted(key, 10*time.Minute)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.IsExhausted(key)
	}
}

func BenchmarkIsExhausted_Miss(b *testing.B) {
	cache := NewExhaustionCache()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.IsExhausted("missing-key")
	}
}

func BenchmarkIsExhaustedScope(b *testing.B) {
	cache := NewExhaustionCache()
	cache.MarkExhausted(ExhaustKey("conn-abc123", "gpt-4o"), 10*time.Minute)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.IsExhaustedScope("conn-abc123", "gpt-4o")
	}
}
