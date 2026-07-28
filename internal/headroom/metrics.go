package headroom

import "sync/atomic"

// Metrics holds the headroom counters.
type Metrics struct {
	total      atomic.Uint64
	bytesSaved atomic.Uint64
	errors     atomic.Uint64
}

// NewMetrics creates a new metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordSuccess increments total and bytesSaved. Bytes saved is the delta
// between original and compressed sizes, floored at zero.
func (m *Metrics) RecordSuccess(original, compressed int) {
	if m == nil {
		return
	}
	// Keep package-level counters in sync so admin status reflects real usage.
	IncTotal()
	AddBytesSaved(int64(original - compressed))
	m.total.Add(1)
	saved := original - compressed
	if saved > 0 {
		m.bytesSaved.Add(uint64(saved))
	}
}

// RecordError increments the error counter and total.
func (m *Metrics) RecordError() {
	if m == nil {
		return
	}
	IncTotal()
	IncErrors()
	m.total.Add(1)
	m.errors.Add(1)
}

// Total returns the total number of compression attempts.
func (m *Metrics) Total() uint64 {
	return m.total.Load()
}

// BytesSaved returns the cumulative number of bytes saved.
func (m *Metrics) BytesSaved() uint64 {
	return m.bytesSaved.Load()
}

// Errors returns the cumulative number of compression errors.
func (m *Metrics) Errors() uint64 {
	return m.errors.Load()
}
