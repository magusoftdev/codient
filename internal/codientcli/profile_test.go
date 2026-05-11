package codientcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"codient/internal/config"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func setupTestProfileEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")
	return dir
}

func writePC(t *testing.T, dir string, pc *config.PersistentConfig) {
	t.Helper()
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestSession(t *testing.T, dir string) *session {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return &session{cfg: cfg}
}

func TestProfileList(t *testing.T) {
	dir := setupTestProfileEnv(t)
	pc := &config.PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://test/v1",
		Profiles: map[string]config.ProfileOverride{
			"alpha": {Model: strPtr("a-model")},
			"beta":  {Model: strPtr("b-model")},
		},
		ActiveProfile: "alpha",
	}
	writePC(t, dir, pc)

	s := newTestSession(t, dir)
	if err := s.profileList(); err != nil {
		t.Fatal(err)
	}
}

func TestProfileSaveAndDelete(t *testing.T) {
	dir := setupTestProfileEnv(t)
	pc := &config.PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://test/v1",
		Model:         "test-model",
	}
	writePC(t, dir, pc)

	s := newTestSession(t, dir)

	// Save a new profile.
	if err := s.profileSave("new-prof", false); err != nil {
		t.Fatalf("profileSave: %v", err)
	}
	if s.cfg.ActiveProfile != "new-prof" {
		t.Fatalf("expected active profile 'new-prof', got %q", s.cfg.ActiveProfile)
	}
	if _, ok := s.cfg.Profiles["new-prof"]; !ok {
		t.Fatal("profile 'new-prof' should exist in cfg.Profiles")
	}

	// Overwrite without force should fail.
	err := s.profileSave("new-prof", false)
	if err == nil {
		t.Fatal("expected error when overwriting without force")
	}

	// Overwrite with force should succeed.
	if err := s.profileSave("new-prof", true); err != nil {
		t.Fatalf("profileSave --force: %v", err)
	}

	// Delete the active profile without force should fail.
	err = s.profileDelete("new-prof", false)
	if err == nil {
		t.Fatal("expected error when deleting active profile without force")
	}

	// Delete with force should succeed.
	if err := s.profileDelete("new-prof", true); err != nil {
		t.Fatalf("profileDelete --force: %v", err)
	}
	if s.cfg.ActiveProfile != "" {
		t.Fatalf("expected empty active profile after delete, got %q", s.cfg.ActiveProfile)
	}
	if _, ok := s.cfg.Profiles["new-prof"]; ok {
		t.Fatal("profile 'new-prof' should not exist after delete")
	}
}

func TestProfileDiff(t *testing.T) {
	dir := setupTestProfileEnv(t)
	pc := &config.PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://test/v1",
		Model:         "base-model",
		Profiles: map[string]config.ProfileOverride{
			"alt": {
				Model:   strPtr("alt-model"),
				LintCmd: strPtr("custom-lint"),
			},
		},
	}
	writePC(t, dir, pc)

	s := newTestSession(t, dir)
	changes := diffProfileOverrides(s.cfg, &config.ProfileOverride{
		Model:   strPtr("alt-model"),
		LintCmd: strPtr("custom-lint"),
	})
	if len(changes) < 1 {
		t.Fatal("expected at least 1 diff entry")
	}
	foundModel := false
	for _, c := range changes {
		if c.key == "model" {
			foundModel = true
			if c.current != "base-model" || c.override != "alt-model" {
				t.Fatalf("model diff: current=%q override=%q", c.current, c.override)
			}
		}
	}
	if !foundModel {
		t.Fatal("expected model in diff entries")
	}
}

func TestProfileSwapRebuilds(t *testing.T) {
	dir := setupTestProfileEnv(t)
	pc := &config.PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://test/v1",
		Model:         "model-a",
		Profiles: map[string]config.ProfileOverride{
			"alt": {
				Model:      strPtr("model-b"),
				AutoCheckCmd: strPtr("make check"),
			},
		},
	}
	writePC(t, dir, pc)

	s := newTestSession(t, dir)
	if s.cfg.Model != "model-a" {
		t.Fatalf("initial model: got %q want model-a", s.cfg.Model)
	}

	ctx := context.Background()
	if err := s.profileSwap(ctx, "alt"); err != nil {
		t.Fatalf("profileSwap: %v", err)
	}
	if s.cfg.Model != "model-b" {
		t.Fatalf("after swap model: got %q want model-b", s.cfg.Model)
	}
	if s.cfg.AutoCheckCmd != "make check" {
		t.Fatalf("after swap autocheck: got %q want 'make check'", s.cfg.AutoCheckCmd)
	}
	if s.cfg.ActiveProfile != "alt" {
		t.Fatalf("after swap active: got %q want alt", s.cfg.ActiveProfile)
	}

	// Swap back to default.
	if err := s.profileSwap(ctx, ""); err != nil {
		t.Fatalf("profileSwap to default: %v", err)
	}
	if s.cfg.Model != "model-a" {
		t.Fatalf("after default swap model: got %q want model-a", s.cfg.Model)
	}
	if s.cfg.ActiveProfile != "" {
		t.Fatalf("after default swap active: got %q want empty", s.cfg.ActiveProfile)
	}
}

func TestProfileSaveInvalidName(t *testing.T) {
	dir := setupTestProfileEnv(t)
	pc := &config.PersistentConfig{SchemaVersion: 2, BaseURL: "http://test/v1"}
	writePC(t, dir, pc)

	s := newTestSession(t, dir)
	err := s.profileSave("INVALID", false)
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestBuildProfileDelta(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "http://custom/v1",
		APIKey:        "custom-key",
		Model:         "my-model",
		SandboxMode:   "off",
		GitAutoCommit: true,
		PlanTot:       false,
		Plain:         true,
	}
	delta := buildProfileDelta(cfg)

	if delta.BaseURL == nil || *delta.BaseURL != "http://custom/v1" {
		t.Fatalf("expected non-default base_url in delta")
	}
	if delta.APIKey == nil || *delta.APIKey != "custom-key" {
		t.Fatalf("expected non-default api_key in delta")
	}
	if delta.Model == nil || *delta.Model != "my-model" {
		t.Fatalf("expected model in delta")
	}
	// SandboxMode == "off" is the default, so should NOT be in delta.
	if delta.SandboxMode != nil {
		t.Fatalf("sandbox_mode 'off' should not be in delta (it's the default)")
	}
	// GitAutoCommit == true is default, should NOT be in delta.
	if delta.GitAutoCommit != nil {
		t.Fatalf("git_auto_commit true should not be in delta (it's the default)")
	}
	// PlanTot == false differs from default true, should be in delta.
	if delta.PlanTot == nil || *delta.PlanTot != false {
		t.Fatalf("plan_tot=false should be in delta")
	}
	if delta.Plain == nil || *delta.Plain != true {
		t.Fatalf("plain=true should be in delta")
	}
}
