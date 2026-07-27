package telemetry

import "sync/atomic"

// CodexMetrics holds atomic counters for Codex-specific telemetry.
// The counters are safe for concurrent use and are exposed through the
// existing metrics endpoint or dashboard stats mechanism.
type CodexMetrics struct {
	RequestsTotal          atomic.Int64
	IncompleteStreamsTotal atomic.Int64
	ReplayHitsTotal        atomic.Int64
	IdentityConfuseTotal   atomic.Int64
}

var codexMetricsInst CodexMetrics

// GetCodexMetrics returns the package-level Codex telemetry counters.
func GetCodexMetrics() *CodexMetrics {
	return &codexMetricsInst
}

// ResetCodexCounters zeroes the Codex telemetry counters. It is intended for
// tests only and is not safe to call concurrently with live traffic.
func ResetCodexCounters() {
	c := GetCodexMetrics()
	c.RequestsTotal.Store(0)
	c.IncompleteStreamsTotal.Store(0)
	c.ReplayHitsTotal.Store(0)
	c.IdentityConfuseTotal.Store(0)
}
