package codientcli

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codient/internal/config"
	"codient/internal/prompt"
)

// TestSendTUIChrome_RoutesThroughChromeSink verifies the test seam: when a
// chromeSink is installed on the session, sendTUIChrome must invoke it with a
// message that reflects the current cfg.Model. Without the seam the chrome
// message would only land in a live *tea.Program, which is impractical to
// observe from unit tests.
func TestSendTUIChrome_RoutesThroughChromeSink(t *testing.T) {
	cfg := &config.Config{
		BaseURL:   "https://api.example.com/v1",
		APIKey:    "sk-test",
		Model:     "alpha-model",
		Workspace: t.TempDir(),
	}
	var captured []tuiChromeMsg
	s := &session{
		cfg:        cfg,
		mode:       prompt.ModeAuto,
		chromeSink: func(m tuiChromeMsg) { captured = append(captured, m) },
	}

	s.sendTUIChrome()
	if len(captured) != 1 {
		t.Fatalf("expected 1 chrome message, got %d", len(captured))
	}
	if captured[0].Model != "alpha-model" {
		t.Fatalf("chrome Model = %q, want %q", captured[0].Model, "alpha-model")
	}

	cfg.Model = "beta-model"
	s.sendTUIChrome()
	if len(captured) != 2 {
		t.Fatalf("expected 2 chrome messages, got %d", len(captured))
	}
	if captured[1].Model != "beta-model" {
		t.Fatalf("second chrome Model = %q, want %q", captured[1].Model, "beta-model")
	}
}

// TestSendTUIChrome_NoSinkNoTUIIsNoOp guards against regressions of the
// short-circuit behavior: when neither a sink nor a live TUI program is
// attached, sendTUIChrome must not panic (it is exercised from many code
// paths, including print-mode where no TUI exists).
func TestSendTUIChrome_NoSinkNoTUIIsNoOp(t *testing.T) {
	s := &session{
		cfg:  &config.Config{Model: "x"},
		mode: prompt.ModeAuto,
	}
	s.sendTUIChrome() // must not panic
}

// TestApplyPostSetupReload_RefreshesTUIChrome verifies the fix for the bug
// where the model footer below the input box was not refreshed after the
// interactive setup wizard changed the model.
//
// The slash-command handler used to forget to call sendTUIChrome after
// rebuilding the client / registry, leaving the footer pointing at the
// previous model until something else triggered a chrome refresh
// (turn completion, mode change, etc.). applyPostSetupReload now owns the
// post-wizard refresh and must always publish a chrome update.
func TestApplyPostSetupReload_RefreshesTUIChrome(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := &config.Config{
		BaseURL:   srv.URL + "/v1",
		APIKey:    "sk-test",
		Model:     "after-setup-model",
		Workspace: t.TempDir(),
	}

	var captured []tuiChromeMsg
	s := &session{
		cfg:        cfg,
		mode:       prompt.ModeAuto,
		chromeSink: func(m tuiChromeMsg) { captured = append(captured, m) },
	}

	s.applyPostSetupReload(context.Background())

	if len(captured) == 0 {
		t.Fatal("applyPostSetupReload must publish a chrome message")
	}
	last := captured[len(captured)-1]
	if last.Model != "after-setup-model" {
		t.Fatalf("chrome Model after reload = %q, want %q", last.Model, "after-setup-model")
	}
	if s.client == nil {
		t.Fatal("openai client should be rebuilt after setup")
	}
	if s.registry == nil {
		t.Fatal("tool registry should be rebuilt after setup")
	}
}

// TestSlashSetup_RefreshesTUIChromeAfterModelChange exercises the full
// `/setup` slash command path end-to-end: a mock OpenAI-compatible server
// advertises two models, fake stdin drives the wizard prompts, and the
// captured chrome stream must end with a message referencing the newly
// selected model so the user-input footer reflects the change.
//
// This is the regression test that pins down the original bug: simulating
// the user running `/setup` mid-session and selecting a different model.
func TestSlashSetup_RefreshesTUIChromeAfterModelChange(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"first-model"},{"id":"second-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		BaseURL:   srv.URL + "/v1",
		APIKey:    "sk-test",
		Model:     "first-model",
		Workspace: t.TempDir(),
	}

	var captured []tuiChromeMsg
	s := &session{
		cfg:        cfg,
		mode:       prompt.ModeAuto,
		chromeSink: func(m tuiChromeMsg) { captured = append(captured, m) },
	}

	// Wizard prompt sequence:
	//   1. Base URL  (blank -> keep cfg.BaseURL)
	//   2. API key   (blank -> keep cfg.APIKey)
	//   3. Model selection  -> "2" (picks "second-model")
	//   4. Embedding model  (blank -> stay empty, skips embedding override)
	//   5. High-reasoning override? (n -> skip)
	input := strings.Join([]string{"", "", "2", "", "n"}, "\n") + "\n"
	sc := bufio.NewScanner(strings.NewReader(input))

	cmds := s.buildSlashCommands(context.Background(), sc)
	cmd, _, ok := cmds.Parse("/setup")
	if !ok || cmd == nil {
		t.Fatal("/setup command should be registered")
	}
	if err := cmd.Run(""); err != nil {
		t.Fatalf("/setup run: %v", err)
	}

	if s.cfg.Model != "second-model" {
		t.Fatalf("cfg.Model after /setup = %q, want %q", s.cfg.Model, "second-model")
	}
	if len(captured) == 0 {
		t.Fatal("/setup must trigger at least one chrome refresh; got none")
	}
	last := captured[len(captured)-1]
	if last.Model != "second-model" {
		t.Fatalf("final chrome Model = %q, want %q (footer would stay stale on the old model otherwise)", last.Model, "second-model")
	}
}
