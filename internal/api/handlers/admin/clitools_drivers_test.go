package admin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setTempHome points os.UserHomeDir() at a temp dir for the duration of the test.
func setTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	return home
}

func ctxDone(t *testing.T) context.Context { return context.Background() }

func TestOpenclawDriverApplyReset(t *testing.T) {
	home := setTempHome(t)
	d := openclawDriver{}
	path := d.settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"agents":{"list":[{"id":"agent1","name":"A1","agentDir":"` + home + `/.openclaw/agents/agent1","model":"cc/old"}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := CLIToolSelection{
		Model:       "cc/claude-opus-4-7",
		BaseURL:     "http://localhost:3777/v1",
		AgentModels: map[string]string{"agent1": "cc/claude-sonnet-5"},
	}
	cfg, err := d.apply(ctxDone(t), sel, "sk_test")
	if err != nil {
		t.Fatalf("apply err: %v", err)
	}
	content := cfg.ConfigContent
	if !strings.Contains(content, `9router/cc/claude-opus-4-7`) {
		t.Fatalf("default model not written: %s", content)
	}
	if !strings.Contains(content, `"primary": "9router/cc/claude-opus-4-7"`) {
		t.Fatalf("agents.defaults.model.primary missing: %s", content)
	}
	agentModels := filepath.Join(home, ".openclaw", "agents", "agent1", "models.json")
	bs, err := os.ReadFile(agentModels)
	if err != nil {
		t.Fatalf("agent models.json not written: %v", err)
	}
	if !strings.Contains(string(bs), "cc/claude-sonnet-5") {
		t.Fatalf("per-agent model not written: %s", bs)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(path)
	if strings.Contains(string(bs), "9router") {
		t.Fatalf("reset did not remove 9router: %s", bs)
	}
	inst, has, _, err := d.detect(ctxDone(t))
	if err != nil || !inst || has {
		t.Fatalf("detect mismatch: inst=%v has=%v err=%v", inst, has, err)
	}
}

func TestClineDriverApplyReset(t *testing.T) {
	setTempHome(t)
	d := clineDriver{}
	sel := CLIToolSelection{Model: "cc/claude-sonnet-5", BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(d.globalStatePath())
	s := string(bs)
	if !strings.Contains(s, `"openAiBaseUrl": "http://localhost:3777"`) {
		t.Fatalf("base without /v1 expected: %s", s)
	}
	if !strings.Contains(s, `"actModeApiProvider": "openai"`) {
		t.Fatalf("actModeApiProvider not openai: %s", s)
	}
	sb, _ := os.ReadFile(d.secretsPath())
	if !strings.Contains(string(sb), "sk_test") {
		t.Fatalf("secrets missing key: %s", sb)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect should be installed+hasUs: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(d.globalStatePath())
	if strings.Contains(string(bs), "9router") || strings.Contains(string(bs), "openAiBaseUrl") {
		t.Fatalf("reset incomplete: %s", bs)
	}
}

func TestKiloDriverApplyReset(t *testing.T) {
	setTempHome(t)
	d := kiloDriver{}
	sel := CLIToolSelection{Model: "cc/claude-sonnet-5", BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(d.authPath())
	s := string(bs)
	if !strings.Contains(s, `"openai-compatible"`) || !strings.Contains(s, "http://localhost:3777/v1") {
		t.Fatalf("auth entry missing: %s", s)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect mismatch: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(d.authPath())
	if strings.Contains(string(bs), "9router") || strings.Contains(string(bs), "openai-compatible") {
		t.Fatalf("reset incomplete: %s", bs)
	}
}

func TestDroidDriverActiveModel(t *testing.T) {
	setTempHome(t)
	d := droidDriver{}
	sel := CLIToolSelection{
		Models:      []string{"cc/a", "cc/b", "cc/c"},
		ActiveModel: "cc/b",
		BaseURL:     "http://localhost:3777/v1",
	}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(d.settingsPath())
	s := string(bs)
	if !strings.Contains(s, "custom:9Router-0") || !strings.Contains(s, "cc/b") {
		t.Fatalf("active model not first: %s", s)
	}
	if !strings.Contains(s, "openai") {
		t.Fatalf("provider missing: %s", s)
	}
	sel.ActiveModel = ""
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply2 err: %v", err)
	}
	bs, _ = os.ReadFile(d.settingsPath())
	if !strings.Contains(string(bs), `"id": "custom:9Router-0"`) || !strings.Contains(string(bs), `"model": "cc/a"`) {
		t.Fatalf("default should be cc/a when activeModel empty: %s", bs)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(d.settingsPath())
	if strings.Contains(string(bs), "9Router") {
		t.Fatalf("reset incomplete: %s", bs)
	}
}

func TestHermesDriverApplyReset(t *testing.T) {
	setTempHome(t)
	d := hermesDriver{}
	p := d.configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("other: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := CLIToolSelection{Model: "cc/claude-sonnet-5", BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(p)
	s := string(bs)
	if !strings.Contains(s, `default: "cc/claude-sonnet-5"`) || !strings.Contains(s, `provider: "custom"`) || !strings.Contains(s, "http://localhost:3777/v1") {
		t.Fatalf("model block wrong: %s", s)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect mismatch: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(p)
	if strings.Contains(string(bs), "model:") {
		t.Fatalf("reset did not remove model block: %s", bs)
	}
}

func TestDeepseekTuiDriverApplyReset(t *testing.T) {
	setTempHome(t)
	d := deepseekTuiDriver{}
	sel := CLIToolSelection{Model: "cc/claude-sonnet-5", BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(d.configPath())
	s := string(bs)
	if !strings.Contains(s, `provider = "openai"`) || !strings.Contains(s, "http://localhost:3777/v1") || !strings.Contains(s, `model = "cc/claude-sonnet-5"`) {
		t.Fatalf("config wrong: %s", s)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect mismatch: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(d.configPath())
	if !strings.Contains(string(bs), `provider = "deepseek"`) {
		t.Fatalf("reset wrong: %s", bs)
	}
}

func TestJcodeDriverApplyReset(t *testing.T) {
	setTempHome(t)
	d := jcodeDriver{}
	sel := CLIToolSelection{Models: []string{"cc/claude-opus-4-7", "cc/claude-sonnet-5"}, BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(d.configPath())
	s := string(bs)
	if !strings.Contains(s, "providers.9router") || !strings.Contains(s, "openai-compatible") || !strings.Contains(s, "localhost:3777/v1") {
		t.Fatalf("config wrong: %s", s)
	}
	ebs, _ := os.ReadFile(d.envPath())
	if !strings.Contains(string(ebs), `JCODE_9ROUTER_API_KEY="sk_test"`) {
		t.Fatalf("env wrong: %s", ebs)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect mismatch: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(d.configPath())
	if strings.Contains(string(bs), "9router") {
		t.Fatalf("reset incomplete: %s", bs)
	}
}

func TestCopilotDriverApplyReset(t *testing.T) {
	home := setTempHome(t)
	d := copilotDriver{}
	path := filepath.Join(home, ".config", "Code", "User", "chatLanguageModels.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	sel := CLIToolSelection{Models: []string{"cc/claude-sonnet-5"}, BaseURL: "http://localhost:3777/v1"}
	if _, err := d.apply(ctxDone(t), sel, "sk_test"); err != nil {
		t.Fatalf("apply err: %v", err)
	}
	bs, _ := os.ReadFile(path)
	s := string(bs)
	if !strings.Contains(s, `"name": "9Router"`) || !strings.Contains(s, "#models.ai.azure.com") || !strings.Contains(s, "sk_test") {
		t.Fatalf("config wrong: %s", s)
	}
	inst, has, _, _ := d.detect(ctxDone(t))
	if !inst || !has {
		t.Fatalf("detect mismatch: inst=%v has=%v", inst, has)
	}
	if err := d.reset(ctxDone(t)); err != nil {
		t.Fatalf("reset err: %v", err)
	}
	bs, _ = os.ReadFile(path)
	if strings.Contains(string(bs), "9Router") {
		t.Fatalf("reset incomplete: %s", bs)
	}
}

func TestCLIToolStatusShape(t *testing.T) {
	st := CLIToolStatus{Configured: true, Installed: true, HasRouter: true}
	if st.Configured != true || st.Installed != true || st.HasRouter != true {
		t.Fatalf("status shape wrong: %+v", st)
	}
}
