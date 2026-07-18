package admin

import "testing"

func TestDefaultSettingsUsesCompactLogFormat(t *testing.T) {
	if got := DefaultSettings["log_format"]; got != "compact" {
		t.Fatalf("DefaultSettings[log_format] = %q, want %q", got, "compact")
	}
}
