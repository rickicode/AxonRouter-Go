package backup

import (
	"reflect"
	"sort"
	"testing"
)

func TestBackupFormatTypesExposeVersionedEnvelopeAndRows(t *testing.T) {
	headerType := reflect.TypeOf(Header{})

	fields := map[string]string{
		"Format":     "string",
		"Version":    "int",
		"Categories": "[]string",
		"CreatedAt":  "int64",
	}
	for name, wantType := range fields {
		field, ok := headerType.FieldByName(name)
		if !ok {
			t.Fatalf("Header missing field %s", name)
		}
		if got := field.Type.String(); got != wantType {
			t.Fatalf("Header.%s type = %s, want %s", name, got, wantType)
		}
	}

	rowType := reflect.TypeOf(Row{})
	rowFields := map[string]reflect.Type{
		"Table": reflect.TypeOf(""),
		"Data":  reflect.TypeOf(map[string]any{}),
	}
	for name, wantType := range rowFields {
		field, ok := rowType.FieldByName(name)
		if !ok {
			t.Fatalf("Row missing field %s", name)
		}
		if got := field.Type; got != wantType {
			t.Fatalf("Row.%s type = %s, want %s", name, got, wantType)
		}
	}
}

func TestCategoryTablesDefinesBackupGroups(t *testing.T) {
	want := map[string][]string{
		"core": {
			"provider_types",
			"connections",
			"model_rate_limits",
			"api_keys",
			"settings",
			"rotation_state",
			"api_key_usage",
			"quota_cache",
			"proxy_pools",
			"proxy_groups",
			"model_pricing",
			"provider_models",
		},
		"combos": {
			"combos",
			"combo_steps",
		},
		"logs": {
			"request_logs",
			"compression_metrics",
		},
		"cache": {
			"response_cache",
		},
	}

	if !reflect.DeepEqual(CategoryTables, want) {
		t.Fatalf("CategoryTables = %#v, want %#v", CategoryTables, want)
	}
}

func TestAllCategoriesReturnsSortedCopy(t *testing.T) {
	got := AllCategories()
	want := []string{"cache", "combos", "core", "logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllCategories() = %#v, want %#v", got, want)
	}

	if !sort.StringsAreSorted(got) {
		t.Fatalf("AllCategories() is not sorted: %#v", got)
	}

	got[0] = "mutated"
	again := AllCategories()
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("AllCategories() returned shared state after mutation: %#v", again)
	}
}
