package codientcli

import (
	"strings"
	"testing"
)

func TestApplyTaskToFirstTurnIfNeeded_priorUserTurnsSkipsTaskBlock(t *testing.T) {
	got, err := applyTaskToFirstTurnIfNeeded(1, "hello", "goal", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
	got2, err := applyTaskToFirstTurnIfNeeded(0, "hello", "goal", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got2, "Objective") || !strings.Contains(got2, "hello") {
		t.Fatalf("expected task block: %q", got2)
	}
}
