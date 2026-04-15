package tokenest

import (
	"strings"
	"testing"
)

func TestEstimate(t *testing.T) {
	if got := Estimate(""); got < 1 {
		t.Fatalf("empty string should estimate >=1, got %d", got)
	}
	long := strings.Repeat("a", 400)
	est := Estimate(long)
	if est < 90 || est > 120 {
		t.Fatalf("400 chars expected ~100 tokens, got %d", est)
	}
}

func TestMessageOverhead(t *testing.T) {
	if MessageOverhead != 4 {
		t.Fatalf("MessageOverhead = %d, want 4", MessageOverhead)
	}
}
