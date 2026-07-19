package db

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDevinQoderProviderTypesSeeded(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	rows, err := d.Query("SELECT id, display_name, format, base_url, category, service_kinds FROM provider_types WHERE id IN ('devin','qoder')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	got := map[string]struct {
		DisplayName  string
		Format       string
		BaseURL      string
		Category     string
		ServiceKinds []string
	}{}
	for rows.Next() {
		var id, displayName, format, baseURL, category, serviceKindsRaw string
		if err := rows.Scan(&id, &displayName, &format, &baseURL, &category, &serviceKindsRaw); err != nil {
			t.Fatal(err)
		}
		var serviceKinds []string
		if err := json.Unmarshal([]byte(serviceKindsRaw), &serviceKinds); err != nil {
			t.Fatal(err)
		}
		got[id] = struct {
			DisplayName  string
			Format       string
			BaseURL      string
			Category     string
			ServiceKinds []string
		}{displayName, format, baseURL, category, serviceKinds}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		DisplayName  string
		Format       string
		BaseURL      string
		Category     string
		ServiceKinds []string
	}{
		"devin": {
			DisplayName:  "Devin CLI",
			Format:       "devin-cli",
			BaseURL:      "",
			Category:     "apikey",
			ServiceKinds: []string{"llm"},
		},
		"qoder": {
			DisplayName:  "Qoder",
			Format:       "qoder",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Category:     "apikey",
			ServiceKinds: []string{"llm"},
		},
	}

	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Fatalf("provider type %q not seeded", id)
		}
		if g.DisplayName != w.DisplayName {
			t.Errorf("%q display_name = %q, want %q", id, g.DisplayName, w.DisplayName)
		}
		if g.Format != w.Format {
			t.Errorf("%q format = %q, want %q", id, g.Format, w.Format)
		}
		if g.BaseURL != w.BaseURL {
			t.Errorf("%q base_url = %q, want %q", id, g.BaseURL, w.BaseURL)
		}
		if g.Category != w.Category {
			t.Errorf("%q category = %q, want %q", id, g.Category, w.Category)
		}
		if len(g.ServiceKinds) != len(w.ServiceKinds) || g.ServiceKinds[0] != w.ServiceKinds[0] {
			t.Errorf("%q service_kinds = %v, want %v", id, g.ServiceKinds, w.ServiceKinds)
		}
	}
}