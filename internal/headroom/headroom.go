// Package headroom provides the runtime contract and status helpers used by
// admin handlers to report Headroom counters. The actual implementation of
// headroom endpoint calls lives in a separate sub-issue and imports this
// package for its public API.
package headroom

import (
	"sync/atomic"
)

// DefaultEndpoint is the default Headroom API endpoint used when none is
// configured in settings.
const DefaultEndpoint = "https://api.headroom.example.com/v1"

// Status describes whether Headroom is active and reachable at runtime.
type Status int32

const (
	// StatusDisabled means headroom is not enabled in settings.
	StatusDisabled Status = iota
	// StatusIdle means enabled but not currently processing.
	StatusIdle
	// StatusRunning means a headroom request is in-flight.
	StatusRunning
)

// String returns the runtime status string used by the admin API.
func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "disabled"
	case StatusIdle:
		return "idle"
	case StatusRunning:
		return "running"
	default:
		return "unknown"
	}
}

// Default runtime counters. These are intentionally package-level variables
// so that future sub-issues can mutate them without needing to plumb a full
// object through every call site. The API handler reads them safely via
// LoadCounters.
var (
	running     int32
	total       int64
	bytesSaved  int64
	errorsCount int64
)

// SetEnabled marks Headroom as running/idle. False maps to disabled.
func SetEnabled(enabled bool) {
	if enabled {
		SetIdle()
	} else {
		atomic.StoreInt32((*int32)(&running), int32(StatusDisabled))
	}
}

// SetIdle records that Headroom is enabled but not currently running.
func SetIdle() {
	atomic.StoreInt32(&running, int32(StatusIdle))
}

// SetRunning records that a Headroom request is in-flight.
func SetRunning() {
	atomic.StoreInt32(&running, int32(StatusRunning))
}

// IncTotal increments the total Headroom request counter.
func IncTotal() {
	atomic.AddInt64(&total, 1)
}

// AddBytesSaved adds to the total bytes saved counter.
func AddBytesSaved(n int64) {
	if n <= 0 {
		return
	}
	atomic.AddInt64(&bytesSaved, n)
}

// IncErrors increments the Headroom error counter.
func IncErrors() {
	atomic.AddInt64(&errorsCount, 1)
}

// Counters is a snapshot of the current Headroom counters.
type Counters struct {
	Running    Status
	Total      int64
	BytesSaved int64
	Errors     int64
}

// LoadCounters returns a consistent snapshot of the Headroom counters.
func LoadCounters() Counters {
	return Counters{
		Running:    Status(atomic.LoadInt32(&running)),
		Total:      atomic.LoadInt64(&total),
		BytesSaved: atomic.LoadInt64(&bytesSaved),
		Errors:     atomic.LoadInt64(&errorsCount),
	}
}
