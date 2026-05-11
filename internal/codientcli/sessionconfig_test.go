package codientcli

import (
	"strings"
	"testing"

	"codient/internal/config"
)

// TestSessionConfig_MouseEnabledRoundTrip verifies that /config can read,
// toggle, and validate the mouse_enabled key (which controls TUI mouse
// capture / native text selection).
func TestSessionConfig_MouseEnabledRoundTrip(t *testing.T) {
	cfg := &config.Config{MouseEnabled: true}
	s := &session{cfg: cfg}

	got, ok := s.getConfigValue("mouse_enabled")
	if !ok {
		t.Fatal("getConfigValue(mouse_enabled): not found")
	}
	if got != "true" {
		t.Fatalf("getConfigValue(mouse_enabled): got %q want %q", got, "true")
	}

	if err := s.setConfig("mouse_enabled", "false"); err != nil {
		t.Fatalf("setConfig(mouse_enabled, false): %v", err)
	}
	if cfg.MouseEnabled {
		t.Fatal("setConfig(mouse_enabled, false): cfg.MouseEnabled should be false")
	}
	got, _ = s.getConfigValue("mouse_enabled")
	if got != "false" {
		t.Fatalf("getConfigValue(mouse_enabled) after disable: got %q want %q", got, "false")
	}

	if err := s.setConfig("mouse_enabled", "true"); err != nil {
		t.Fatalf("setConfig(mouse_enabled, true): %v", err)
	}
	if !cfg.MouseEnabled {
		t.Fatal("setConfig(mouse_enabled, true): cfg.MouseEnabled should be true")
	}

	err := s.setConfig("mouse_enabled", "maybe")
	if err == nil {
		t.Fatal("setConfig(mouse_enabled, maybe): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("setConfig(mouse_enabled, maybe): error %q should mention valid values", err.Error())
	}
}
