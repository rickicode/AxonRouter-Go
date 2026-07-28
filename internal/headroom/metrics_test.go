package headroom

import "testing"

func TestMetrics(t *testing.T) {
	m := NewMetrics()
	m.RecordTotal()
	m.RecordBytesSaved(100)
	m.RecordError()

	total, saved, errs := m.Snapshot()
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if saved != 100 {
		t.Fatalf("expected saved 100, got %d", saved)
	}
	if errs != 1 {
		t.Fatalf("expected errors 1, got %d", errs)
	}
}

func TestMetrics_Nil(t *testing.T) {
	var m *Metrics
	m.RecordTotal()
	m.RecordBytesSaved(10)
	m.RecordError()
	total, saved, errs := m.Snapshot()
	if total != 0 || saved != 0 || errs != 0 {
		t.Fatal("nil metrics should be a no-op")
	}
}
