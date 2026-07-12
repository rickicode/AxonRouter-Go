package admin

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPiConfigDiscovery(t *testing.T) {
	c := piConfig(CLIToolSelection{UseDiscovery: true}, "sk-test", "http://localhost:3777/v1")
	if c.ConfigPath != "~/.pi/agent/models.json" {
		t.Fatalf("cfgPath = %q", c.ConfigPath)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(c.ConfigContent), &out); err != nil {
		t.Fatalf("pi discovery JSON invalid: %v\n%s", err, c.ConfigContent)
	}
	prov, ok := out["AxonRouter"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing AxonRouter provider")
	}
	disc, ok := prov["discovery"].(map[string]interface{})
	if !ok || disc["type"] != "openai-models-list" {
		t.Fatalf("discovery mapping wrong: %#v", prov["discovery"])
	}
}

func TestPiConfigManual(t *testing.T) {
	c := piConfig(CLIToolSelection{Models: []string{"cx/gpt-5.4", "oc/go"}}, "sk-test", "http://localhost:3777/v1")
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(c.ConfigContent), &out); err != nil {
		t.Fatalf("pi manual JSON invalid: %v", err)
	}
	prov := out["AxonRouter"].(map[string]interface{})
	models, ok := prov["models"].([]interface{})
	if !ok || len(models) != 2 {
		t.Fatalf("expected 2 models, got %#v", prov["models"])
	}
	if _, hasDisc := prov["discovery"]; hasDisc {
		t.Fatalf("discovery should be absent in manual mode")
	}
}

func TestOmpConfigDiscovery(t *testing.T) {
	c := ompConfig(CLIToolSelection{UseDiscovery: true}, "sk-test", "http://localhost:3777/v1")
	if c.ConfigPath != "~/.omp/agent/models.yml" {
		t.Fatalf("cfgPath = %q", c.ConfigPath)
	}
	// nested discovery mapping, NOT a flat string
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(c.ConfigContent), &out); err != nil {
		t.Fatalf("omp discovery YAML invalid: %v\n%s", err, c.ConfigContent)
	}
	ax := out["axonrouter-go"].(map[string]interface{})
	disc, ok := ax["discovery"].(map[string]interface{})
	if !ok || disc["type"] != "openai-models-list" {
		t.Fatalf("omp discovery not nested mapping: %#v", ax["discovery"])
	}
}

func TestOmpConfigManual(t *testing.T) {
	c := ompConfig(CLIToolSelection{Models: []string{"cx/gpt-5.4"}}, "sk-test", "http://localhost:3777/v1")
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(c.ConfigContent), &out); err != nil {
		t.Fatalf("omp manual YAML invalid: %v", err)
	}
	ax := out["axonrouter-go"].(map[string]interface{})
	models, ok := ax["models"].([]interface{})
	if !ok || len(models) != 1 {
		t.Fatalf("expected 1 model, got %#v", ax["models"])
	}
	if !strings.Contains(c.ConfigContent, "- id: cx/gpt-5.4") {
		t.Fatalf("manual model missing: %s", c.ConfigContent)
	}
}
