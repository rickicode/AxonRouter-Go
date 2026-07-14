package models

import (
	"slices"
	"testing"
)

func TestGetModelIDs_CFIncludesEmbeddingAndImageModels(t *testing.T) {
	ids := GetModelIDs("cf")
	if len(ids) == 0 {
		t.Fatal("GetModelIDs(cf) returned empty")
	}
	want := []string{
		"cf/baai/bge-base-en-v1.5",
		"cf/black-forest-labs/flux-1-schnell",
	}
	for _, w := range want {
		if !slices.Contains(ids, w) {
			t.Errorf("GetModelIDs(cf) missing %q; got %v", w, ids)
		}
	}
}

func TestGetAllModelIDs_CFIncludesEmbeddingAndImageModels(t *testing.T) {
	ids := GetAllModelIDs("cf")
	if !slices.Contains(ids, "cf/baai/bge-base-en-v1.5") {
		t.Errorf("GetAllModelIDs(cf) missing embedding model; got %v", ids)
	}
	if !slices.Contains(ids, "cf/black-forest-labs/flux-1-schnell") {
		t.Errorf("GetAllModelIDs(cf) missing image model; got %v", ids)
	}
}

func TestGetModelServiceKinds_CatalogLLM(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/meta/llama-3.2-1b-instruct")
	if !slices.Contains(kinds, "llm") {
		t.Errorf("GetModelServiceKinds(cf, llama) = %v, want llm", kinds)
	}
}

func TestGetModelServiceKinds_ModalitiesEmbedding(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/baai/bge-base-en-v1.5")
	if len(kinds) != 1 || kinds[0] != "embedding" {
		t.Errorf("GetModelServiceKinds(cf, bge) = %v, want [embedding]", kinds)
	}
}

func TestGetModelServiceKinds_ModalitiesImage(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/black-forest-labs/flux-1-schnell")
	if len(kinds) != 1 || kinds[0] != "image" {
		t.Errorf("GetModelServiceKinds(cf, flux) = %v, want [image]", kinds)
	}
}

func TestGetModelServiceKinds_UnknownModel(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/not-a-model")
	if len(kinds) != 0 {
		t.Errorf("GetModelServiceKinds(cf, not-a-model) = %v, want empty", kinds)
	}
}
