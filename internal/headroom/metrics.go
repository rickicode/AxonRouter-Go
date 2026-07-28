package headroom

import "sync/atomic"

// Metrics holds atomically-updated counters for headroom operations.
type Metrics struct {
	total      atomic.Int64
	bytesSaved atomic.Int64
	errors     atomic.Int64
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordTotal increments the total number of compression attempts.
func (m *Metrics) RecordTotal() {
	if m != nil {
		m.total.Add(1)
	}
}

// RecordBytesSaved adds the number of bytes saved by compression.
func (m *Metrics) RecordBytesSaved(saved int) {
	if m != nil && saved > 0 {
		m.bytesSaved.Add(int64(saved))
	}
}

// RecordError increments the error counter.
func (m *Metrics) RecordError() {
	if m != nil {
		m.errors.Add(1)
	}
}

// Snapshot returns the current counter values.
func (m *Metrics) Snapshot() (total, bytesSaved, errors int64) {
	if m == nil {
		return 0, 0, 0
	}
	return m.total.Load(), m.bytesSaved.Load(), m.errors.Load()
}

// headroom_total returns the total counter value.
func (m *Metrics) Total() int64 {
	if m == nil {
		return 0
	}
	return m.total.Load()
}

// BytesSaved returns the bytes saved counter value.
func (m *Metrics) BytesSaved() int64 {
	if m == nil {
		return 0
	}
	return m.bytesSaved.Load()
}

// Errors returns the error counter value.
func (m *Metrics) Errors() int64 {
	if m == nil {
		return 0
	}
	return m.errors.Load()
}
