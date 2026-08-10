package connstate

import (
	"testing"
)

func BenchmarkModelScope_NonPerModel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModelScope("openai", "gpt-4o")
	}
}

func BenchmarkModelScope_PerModel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModelScope("oc", "qwen-coder-32b")
	}
}

func BenchmarkModelScope_AntigravityGemini(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModelScope("ag", "ag/gemini-2.5-pro")
	}
}

func BenchmarkModelScope_AntigravityClaude(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModelScope("ag", "ag/claude-sonnet-4")
	}
}

func BenchmarkHasPerModelQuota(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = HasPerModelQuota("ag")
	}
}
