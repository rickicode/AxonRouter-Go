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
		"providers": {
			"provider_types",
			"connections",
			"provider_models",
			"model_rate_limits",
			"combos",
			"combo_steps",
		},
		"config": {
			"settings",
			"model_pricing",
			"proxy_pools",
			"proxy_groups",
			"rotation_state",
			"compression_metrics",
		},
		"api_keys": {
			"api_keys",
			"api_key_usage",
		},
		"usage": {
			"request_logs",
		},
		"cache": {
			"response_cache",
			"quota_cache",
		},
	}

	if !reflect.DeepEqual(CategoryTables, want) {
		t.Fatalf("CategoryTables = %#v, want %#v", CategoryTables, want)
	}
}

func TestAllCategoriesReturnsSortedCopy(t *testing.T) {
	got := AllCategories()
	want := []string{"api_keys", "cache", "config", "providers", "usage"}
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
