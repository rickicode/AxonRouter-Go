package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHTTPSConfig_IsValid_Disabled(t *testing.T) {
	cfg := HTTPSConfig{Enabled: false, CertCache: "certs"}
	ok, msg := cfg.IsValid()
	if !ok || msg != "" {
		t.Fatalf("expected (true, \"\"), got (%v, %q)", ok, msg)
	}
}

func TestHTTPSConfig_IsValid_EnabledMissingDomain(t *testing.T) {
	cfg := HTTPSConfig{Enabled: true, Email: "admin@example.com", AcceptTOS: true}
	ok, msg := cfg.IsValid()
	if ok || msg == "" {
		t.Fatalf("expected false with message, got (%v, %q)", ok, msg)
	}
}

func TestHTTPSConfig_IsValid_EnabledMissingEmail(t *testing.T) {
	cfg := HTTPSConfig{Enabled: true, Domain: "example.com", AcceptTOS: true}
	ok, msg := cfg.IsValid()
	if ok || msg == "" {
		t.Fatalf("expected false with message, got (%v, %q)", ok, msg)
	}
}

func TestHTTPSConfig_IsValid_EnabledTOSNotAccepted(t *testing.T) {
	cfg := HTTPSConfig{Enabled: true, Domain: "example.com", Email: "admin@example.com"}
	ok, msg := cfg.IsValid()
	if ok || msg == "" {
		t.Fatalf("expected false with message, got (%v, %q)", ok, msg)
	}
}

func TestHTTPSConfig_IsValid_EnabledValid(t *testing.T) {
	cfg := HTTPSConfig{Enabled: true, Domain: "example.com", Email: "admin@example.com", AcceptTOS: true}
	ok, msg := cfg.IsValid()
	if !ok || msg != "" {
		t.Fatalf("expected (true, \"\"), got (%v, %q)", ok, msg)
	}
}

func TestLoadHTTPSConfig_MissingFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadHTTPSConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled || cfg.Domain != "" || cfg.Email != "" || cfg.AcceptTOS || cfg.Staging || cfg.CertCache != "certs" {
		t.Fatalf("expected zero/default config, got %+v", cfg)
	}
}

func TestSaveAndLoadHTTPSConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &HTTPSConfig{
		Enabled:   true,
		Domain:    "example.com",
		Email:     "admin@example.com",
		AcceptTOS: true,
		Staging:   true,
		CertCache: "custom-certs",
	}
	if err := SaveHTTPSConfig(dir, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadHTTPSConfig(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.Enabled != cfg.Enabled ||
		loaded.Domain != cfg.Domain ||
		loaded.Email != cfg.Email ||
		loaded.AcceptTOS != cfg.AcceptTOS ||
		loaded.Staging != cfg.Staging ||
		loaded.CertCache != cfg.CertCache {
		t.Fatalf("loaded config does not match saved config: got %+v, want %+v", loaded, cfg)
	}

	if _, err := os.Stat(filepath.Join(dir, "https.yaml")); err != nil {
		t.Fatalf("expected https.yaml to exist: %v", err)
	}
}
