package telemetry

import (
	"sync/atomic"
)

// CodexCounters holds request-scoped telemetry for the Codex executor.
// Values are updated atomically and exposed via the admin /metrics endpoint.
type CodexCounters struct {
	RequestsTotal          atomic.Uint64
	IncompleteStreamsTotal atomic.Uint64
	ReplayHitsTotal        atomic.Uint64
	IdentityConfuseTotal   atomic.Uint64
}

// DefaultCodexCounters is the package-level telemetry sink used by the Codex
// executor. Tests may replace it with a local instance.
var DefaultCodexCounters CodexCounters
