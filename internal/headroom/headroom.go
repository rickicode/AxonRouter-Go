// Package headroom provides runtime status counters used by the admin API.
package headroom

import (
	"sync/atomic"
)

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
// so that service/client can mutate them without plumbing objects through
// every call site.
var (
	running    int32
	total      int64
	bytesSaved int64
	errCount   int64
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
	atomic.AddInt64(&errCount, 1)
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
		Errors:     atomic.LoadInt64(&errCount),
	}
}
