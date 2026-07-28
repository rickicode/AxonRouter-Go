package headroom

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := NewConfig(map[string]string{
		"AXON_HEADROOM_ENABLED":         "true",
		"AXON_HEADROOM_ENDPOINT":        "http://127.0.0.1:9123",
		"AXON_HEADROOM_TIMEOUT_MS":      "5000",
		"AXON_HEADROOM_MAX_PAYLOAD_BYTES": "1024",
	})
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Endpoint != "http://127.0.0.1:9123" {
		t.Fatalf("unexpected endpoint %s", cfg.Endpoint)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("unexpected timeout %v", cfg.Timeout)
	}
	if cfg.MaxPayloadBytes != 1024 {
		t.Fatalf("unexpected max payload %d", cfg.MaxPayloadBytes)
	}
}

func TestDefaultConfig_Fallbacks(t *testing.T) {
	cfg := NewConfig(map[string]string{})
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.Endpoint != "http://127.0.0.1:9123" {
		t.Fatalf("unexpected endpoint %s", cfg.Endpoint)
	}
	if cfg.Timeout != 30000*time.Millisecond {
		t.Fatalf("unexpected timeout %v", cfg.Timeout)
	}
	if cfg.MaxPayloadBytes != 524288 {
		t.Fatalf("unexpected max payload %d", cfg.MaxPayloadBytes)
	}
}
