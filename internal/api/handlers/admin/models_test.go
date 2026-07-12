package admin

import "testing"

func TestDefaultTestModel_CloudflareStripsProviderPrefix(t *testing.T) {
	got := defaultTestModel("cf")
	if got == "" {
		t.Fatal("defaultTestModel(cf) returned empty")
	}
	if got == "cf/meta/llama-3.2-1b-instruct" {
		t.Fatalf("defaultTestModel(cf) returned full gateway ID %q; want model name without cf/ prefix", got)
	}
	if got != "meta/llama-3.2-1b-instruct" {
		t.Fatalf("defaultTestModel(cf) = %q, want meta/llama-3.2-1b-instruct", got)
	}
}
