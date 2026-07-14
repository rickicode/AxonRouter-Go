package modalities

import (
	"slices"
	"testing"
)

func TestServiceKinds_CF(t *testing.T) {
	kinds := ServiceKinds("cf")
	want := []string{"llm", "embedding", "image"}
	if len(kinds) != len(want) {
		t.Fatalf("ServiceKinds(cf) = %v, want %v", kinds, want)
	}
	for _, w := range want {
		if !slices.Contains(kinds, w) {
			t.Errorf("ServiceKinds(cf) missing %q; got %v", w, kinds)
		}
	}
}

func TestModels_CF(t *testing.T) {
	emb := Models("cf", "embedding")
	if len(emb) == 0 {
		t.Fatal("Models(cf, embedding) is empty")
	}
	if !slices.Contains(emb, "@cf/baai/bge-base-en-v1.5") {
		t.Errorf("Models(cf, embedding) missing @cf/baai/bge-base-en-v1.5; got %v", emb)
	}

	img := Models("cf", "image")
	if len(img) == 0 {
		t.Fatal("Models(cf, image) is empty")
	}
	if !slices.Contains(img, "@cf/black-forest-labs/flux-1-schnell") {
		t.Errorf("Models(cf, image) missing @cf/black-forest-labs/flux-1-schnell; got %v", img)
	}
}

func TestModels_UnknownModality(t *testing.T) {
	got := Models("cf", "video")
	if len(got) != 0 {
		t.Errorf("Models(cf, video) = %v, want empty", got)
	}
}

func TestModels_UnknownProvider(t *testing.T) {
	got := Models("unknown", "embedding")
	if len(got) != 0 {
		t.Errorf("Models(unknown, embedding) = %v, want empty", got)
	}
}

func TestSupportsModel_CF(t *testing.T) {
	if !SupportsModel("cf", "embedding", "@cf/baai/bge-base-en-v1.5") {
		t.Error("SupportsModel(cf, embedding, @cf/baai/bge-base-en-v1.5) = false, want true")
	}
	if !SupportsModel("cf", "image", "@cf/black-forest-labs/flux-1-schnell") {
		t.Error("SupportsModel(cf, image, @cf/black-forest-labs/flux-1-schnell) = false, want true")
	}
	if SupportsModel("cf", "embedding", "@cf/black-forest-labs/flux-1-schnell") {
		t.Error("SupportsModel(cf, embedding, flux) = true, want false")
	}
	if SupportsModel("cf", "image", "@cf/baai/bge-base-en-v1.5") {
		t.Error("SupportsModel(cf, image, bge) = true, want false")
	}
}

func TestSupportsModel_UnknownProvider(t *testing.T) {
	if SupportsModel("not-real", "embedding", "@cf/baai/bge-base-en-v1.5") {
		t.Error("SupportsModel(not-real, ...) = true, want false")
	}
}
