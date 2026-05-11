package codientcli

import (
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/prompt"
	"codient/internal/sessionstore"
)

// TestApplyStoredSessionState_LoadsHistoryIgnoresPersistedMode verifies that
// resuming a session restores history but ignores the persisted Mode field —
// the runtime mode is always ModeAuto and the orchestrator picks the internal
// mode per turn.
func TestApplyStoredSessionState_LoadsHistoryIgnoresPersistedMode(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Workspace: tmp, Model: "gpt-4o-mini"}
	s := &session{cfg: cfg, mode: prompt.ModeAuto}
	st := &sessionstore.SessionState{
		ID:        "tst_20260101_000000",
		Workspace: tmp,
		Mode:      "ask", // persisted but no longer authoritative.
		Model:     "gpt-4o-mini",
		Messages: sessionstore.FromOpenAI([]openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("prev"),
			openai.AssistantMessage("ok"),
		}),
	}
	if err := sessionstore.Save(st); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessionstore.LoadByWorkspaceAndID(tmp, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyStoredSessionState(loaded); err != nil {
		t.Fatal(err)
	}
	if s.mode != prompt.ModeAuto {
		t.Fatalf("session should remain in ModeAuto after resume; got %v", s.mode)
	}
	if len(s.history) != 2 {
		t.Fatalf("history len: %d", len(s.history))
	}
	if s.sessionID != st.ID {
		t.Fatalf("session id: %q", s.sessionID)
	}
}

func TestApplyStoredSessionState_WorkspaceMismatch(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Workspace: tmp}
	s := &session{cfg: cfg, mode: prompt.ModeAuto}
	st := &sessionstore.SessionState{
		ID:        "x",
		Workspace: "/other/root",
		Mode:      "build",
		Messages:  sessionstore.FromOpenAI([]openai.ChatCompletionMessageParamUnion{openai.UserMessage("a")}),
	}
	if err := s.applyStoredSessionState(st); err == nil {
		t.Fatal("expected workspace mismatch error")
	}
}

func TestCountUserMessagesInOpenAIHistory(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("a"),
		openai.AssistantMessage("b"),
		openai.UserMessage("c"),
	}
	if n := countUserMessagesInOpenAIHistory(msgs); n != 2 {
		t.Fatalf("got %d", n)
	}
}
