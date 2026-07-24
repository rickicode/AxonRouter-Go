package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestQoderPricingSeed verifies the verified Qoder / DashScope (Qwen Cloud)
// model IDs are seeded in model_pricing with non-zero input/output prices.
func TestQoderPricingSeed(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"qwen3.7-max":      true,
		"qwen3.7-plus":     true,
		"qwen3.6-flash":    true,
		"qwen3-vl-plus":    true,
		"qwen3-coder-plus": true,
		"ultimate":         true,
	}

	rows, err := d.Query(`SELECT model_id, display_name, input_per_1k, output_per_1k FROM model_pricing WHERE model_id IN ('qwen3.7-max','qwen3.7-plus','qwen3.6-flash','qwen3-vl-plus','qwen3-coder-plus','ultimate')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		var inPrice, outPrice float64
		if err := rows.Scan(&id, &name, &inPrice, &outPrice); err != nil {
			t.Fatal(err)
		}
		if name == "" {
			t.Errorf("%s: display_name is empty", id)
		}
		if inPrice == 0 || outPrice == 0 {
			t.Errorf("%s: input_per_1k=%v output_per_1k=%v, both must be non-zero", id, inPrice, outPrice)
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for id := range want {
			missing = append(missing, id)
		}
		t.Errorf("missing Qoder pricing rows: %v", missing)
	}
}
