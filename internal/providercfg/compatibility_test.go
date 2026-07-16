package providercfg

import "testing"

func TestCompatibilityFor_Defaults(t *testing.T) {
	cf := CompatibilityFor("cf")
	if cf.ModelPrefix != "@cf/" {
		t.Fatalf("cf ModelPrefix = %q, want @cf/", cf.ModelPrefix)
	}
	if cf.MaxTokensCap != 8192 {
		t.Fatalf("cf MaxTokensCap = %d, want 8192", cf.MaxTokensCap)
	}
	if cf.ReasoningMaxTokensCap != 4096 {
		t.Fatalf("cf ReasoningMaxTokensCap = %d, want 4096", cf.ReasoningMaxTokensCap)
	}
	if !cf.FlattenContentArrays {
		t.Fatal("cf FlattenContentArrays should be true")
	}

	bedrock := CompatibilityFor("bedrock")
	if bedrock.StripProviderPrefix != "bedrock/" {
		t.Fatalf("bedrock StripProviderPrefix = %q, want bedrock/", bedrock.StripProviderPrefix)
	}
	if bedrock.ModelPrefix != "" {
		t.Fatalf("bedrock ModelPrefix should be empty, got %q", bedrock.ModelPrefix)
	}
}

func TestCompatibilityFor_UnknownProvider(t *testing.T) {
	got := CompatibilityFor("unknown")
	if got.ModelPrefix != "" || got.MaxTokensCap != 0 {
		t.Fatalf("unknown provider should return zero compatibility, got %+v", got)
	}
}

func TestCompatibility_HasReasoning(t *testing.T) {
	c := Compatibility{ReasoningLevels: []string{"low", "medium", "high"}}
	if !c.HasReasoning("medium") {
		t.Fatal("expected medium to be accepted")
	}
	if c.HasReasoning("max") {
		t.Fatal("expected max to be rejected")
	}
}
