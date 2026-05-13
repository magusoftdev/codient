package codientcli

import (
	"context"
	"strings"
	"testing"

	"codient/internal/agent"
)

func TestMakeBuildSelfCritiqueGatesOnMutation(t *testing.T) {
	check := makeBuildSelfCritique()
	if got := check(context.Background(), agent.PostReplyCheckInfo{Mutated: false, TurnTools: []string{"write_file"}}); got != "" {
		t.Fatalf("expected no critique without successful mutation, got %q", got)
	}
	got := check(context.Background(), agent.PostReplyCheckInfo{Mutated: true, TurnTools: []string{"write_file"}})
	if !strings.Contains(got, "self-critique") {
		t.Fatalf("expected self-critique prompt, got %q", got)
	}
	if got := check(context.Background(), agent.PostReplyCheckInfo{Mutated: true, TurnTools: []string{"write_file"}, AutoCheckExhausted: true}); got != "" {
		t.Fatalf("expected exhausted auto-check to skip critique, got %q", got)
	}
}
