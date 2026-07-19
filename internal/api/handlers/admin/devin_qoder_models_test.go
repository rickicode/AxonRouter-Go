package admin

import (
	"slices"
	"testing"
)

func TestDefaultTestModel_DevinQoder(t *testing.T) {
	if got := defaultTestModel("devin"); got != "devin" {
		t.Errorf("defaultTestModel(devin) = %q, want devin", got)
	}
	if got := defaultTestModel("qoder"); got != "qoder-rome-30ba3b" {
		t.Errorf("defaultTestModel(qoder) = %q, want qoder-rome-30ba3b", got)
	}
}

func TestStaticModels_DevinQoder(t *testing.T) {
	devin := staticModels("devin")
	if len(devin) != 1 || devin[0] != "devin" {
		t.Errorf("staticModels(devin) = %v, want [devin]", devin)
	}

	qoder := staticModels("qoder")
	if !slices.Contains(qoder, "qoder-rome-30ba3b") {
		t.Errorf("staticModels(qoder) missing qoder-rome-30ba3b, got %v", qoder)
	}
	if !slices.Contains(qoder, "deepseek-r1") {
		t.Errorf("staticModels(qoder) missing deepseek-r1, got %v", qoder)
	}
}

func TestListModelEntries_DevinQoder(t *testing.T) {
	h := &ModelHandler{}

	devinEntries := h.listModelEntries("devin", []string{"llm"}, nil, staticModels("devin"), nil)
	if len(devinEntries) != 1 {
		t.Fatalf("expected 1 devin entry, got %d", len(devinEntries))
	}
	if devinEntries[0]["id"] != "devin/devin" {
		t.Errorf("expected devin/devin, got %v", devinEntries[0]["id"])
	}
	if !slices.Contains(kindsOf(devinEntries[0]), "llm") {
		t.Errorf("devin entry missing llm service kind: %v", devinEntries[0]["service_kinds"])
	}

	qoderEntries := h.listModelEntries("qoder", []string{"llm"}, nil, staticModels("qoder"), nil)
	if len(qoderEntries) == 0 {
		t.Fatal("expected qoder model entries")
	}
	found := false
	for _, e := range qoderEntries {
		if e["id"] == "qoder/qoder-rome-30ba3b" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected qoder/qoder-rome-30ba3b in entries, got %v", qoderEntries)
	}
}