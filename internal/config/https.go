package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultCertCache = "certs"

type HTTPSConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Domain    string `yaml:"domain" json:"domain"`
	Email     string `yaml:"email" json:"email"`
	AcceptTOS bool   `yaml:"acceptTOS" json:"accept_tos"`
	Staging   bool   `yaml:"staging" json:"staging"`
	CertCache string `yaml:"certCache" json:"cert_cache"`
}

func (c HTTPSConfig) IsValid() (bool, string) {
	if !c.Enabled {
		return true, ""
	}
	if c.Domain == "" {
		return false, "domain is required"
	}
	if c.Email == "" {
		return false, "email is required"
	}
	if !c.AcceptTOS {
		return false, "must accept terms of service"
	}
	return true, ""
}

func LoadHTTPSConfig(dataDir string) (*HTTPSConfig, error) {
	path := filepath.Join(dataDir, "https.yml")
	cfg := &HTTPSConfig{CertCache: defaultCertCache}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.CertCache == "" {
		cfg.CertCache = defaultCertCache
	}
	return cfg, nil
}

func SaveHTTPSConfig(dataDir string, cfg *HTTPSConfig) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dataDir, "https.yml")
	save := *cfg
	if save.CertCache == "" {
		save.CertCache = defaultCertCache
	}

	data, err := yaml.Marshal(&save)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
