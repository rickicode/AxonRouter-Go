package db

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestProviderTypeCategoryAndServiceKinds verifies the provider_types table
// carries the new category and service_kinds columns and that built-in providers
// are seeded with values matching the frontend provider catalog.
func TestProviderTypeCategoryAndServiceKinds(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	rows, err := d.Query("PRAGMA table_info(provider_types)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	wantColumns := map[string]bool{
		"category":      false,
		"service_kinds": false,
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			t.Fatal(err)
		}
		if _, ok := wantColumns[name]; ok {
			wantColumns[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, found := range wantColumns {
		if !found {
			t.Errorf("missing column %q in provider_types", name)
		}
	}

	type row struct {
		id           string
		category     string
		serviceKinds string
	}
	got := map[string]row{}
	r, err := d.Query("SELECT id, category, service_kinds FROM provider_types WHERE id IN ('cf','cx','oc','claude','mimocode','glm','minimax','kimi','mistral','cerebras','together','fireworks','novita','lambda','pollinations','grok-cli','codebuddy')")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for r.Next() {
		var gr row
		if err := r.Scan(&gr.id, &gr.category, &gr.serviceKinds); err != nil {
			t.Fatal(err)
		}
		got[gr.id] = gr
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id           string
		category     string
		serviceKinds []string
	}{
		{"cf", "apikey", []string{"llm", "embedding", "image"}},
		{"cx", "oauth", []string{"llm"}},
		{"oc", "no-auth", []string{"llm"}},
	{"claude", "apikey", []string{"llm"}},
	{"mimocode", "no-auth", []string{"llm"}},
	{"glm", "apikey", []string{"llm"}},
		{"minimax", "apikey", []string{"llm"}},
		{"kimi", "apikey", []string{"llm"}},
		{"mistral", "apikey", []string{"llm"}},
		{"cerebras", "apikey", []string{"llm"}},
		{"together", "apikey", []string{"llm"}},
		{"fireworks", "apikey", []string{"llm"}},
		{"novita", "apikey", []string{"llm"}},
		{"lambda", "apikey", []string{"llm"}},
		{"pollinations", "apikey", []string{"llm"}},
		{"grok-cli", "oauth", []string{"llm"}},
		{"codebuddy", "oauth", []string{"llm"}},
	}
	for _, c := range cases {
		gr, ok := got[c.id]
		if !ok {
			t.Errorf("provider %q not seeded", c.id)
			continue
		}
		if gr.category != c.category {
			t.Errorf("provider %q category = %q, want %q", c.id, gr.category, c.category)
		}
		var kinds []string
		if err := json.Unmarshal([]byte(gr.serviceKinds), &kinds); err != nil {
			t.Fatalf("provider %q service_kinds is not valid JSON: %v", c.id, err)
		}
		if !slicesEqual(kinds, c.serviceKinds) {
			t.Errorf("provider %q service_kinds = %v, want %v", c.id, kinds, c.serviceKinds)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
