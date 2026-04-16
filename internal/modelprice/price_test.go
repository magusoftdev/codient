package modelprice

import (
	"testing"

	"codient/internal/tokentracker"
)

func TestLookup(t *testing.T) {
	in, out, ok := Lookup("gpt-4o-mini-2024-07-18")
	if !ok || in != 0.15 || out != 0.60 {
		t.Fatalf("gpt-4o-mini: %v %v %v", in, out, ok)
	}
	in, out, ok = Lookup("gpt-4o")
	if !ok || in != 2.50 || out != 10.00 {
		t.Fatalf("gpt-4o: %v %v %v", in, out, ok)
	}
	if _, _, ok := Lookup("local-model"); ok {
		t.Fatal("expected no match")
	}
}

func TestEstimateCost(t *testing.T) {
	u := tokentracker.Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000}
	c := EstimateCost(u, 2.0, 4.0)
	if c < 3.99 || c > 4.01 {
		t.Fatalf("cost %v", c)
	}
}
