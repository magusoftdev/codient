package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func intPtr(i int) *int       { return &i }

func writeTestConfig(t *testing.T, dir string, pc *PersistentConfig) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProfileResolutionOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top-level/v1",
		Model:         "top-model",
		Profiles: map[string]ProfileOverride{
			"local": {
				Model:   strPtr("local-model"),
				BaseURL: strPtr("http://local/v1"),
			},
		},
	}
	writeTestConfig(t, dir, pc)

	// No profile selected: top-level used.
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "top-model" {
		t.Fatalf("expected top-model, got %q", c.Model)
	}
	if c.ActiveProfile != "" {
		t.Fatalf("expected empty active profile, got %q", c.ActiveProfile)
	}

	// active_profile selected.
	pc.ActiveProfile = "local"
	writeTestConfig(t, dir, pc)
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "local-model" {
		t.Fatalf("expected local-model from active_profile, got %q", c.Model)
	}
	if c.ActiveProfile != "local" {
		t.Fatalf("expected active profile 'local', got %q", c.ActiveProfile)
	}

	// CODIENT_PROFILE overrides active_profile.
	pc.ActiveProfile = ""
	pc.Profiles["env-prof"] = ProfileOverride{Model: strPtr("env-model")}
	writeTestConfig(t, dir, pc)
	t.Setenv("CODIENT_PROFILE", "env-prof")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "env-model" {
		t.Fatalf("expected env-model from CODIENT_PROFILE, got %q", c.Model)
	}

	// -profile (LoadWithProfile) overrides both.
	c, err = LoadWithProfile("local")
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "local-model" {
		t.Fatalf("expected local-model from explicit flag, got %q", c.Model)
	}
}

func TestProfileNameValidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{SchemaVersion: 2}
	writeTestConfig(t, dir, pc)

	// Invalid name format.
	_, err := LoadWithProfile("UPPER_CASE")
	if err == nil {
		t.Fatal("expected error for uppercase profile name")
	}
	_, err = LoadWithProfile("has space")
	if err == nil {
		t.Fatal("expected error for name with space")
	}

	// Valid names.
	if !ProfileNameRe.MatchString("my-profile") {
		t.Fatal("my-profile should be valid")
	}
	if !ProfileNameRe.MatchString("ci_strict") {
		t.Fatal("ci_strict should be valid")
	}
	if !ProfileNameRe.MatchString("local123") {
		t.Fatal("local123 should be valid")
	}
}

func TestProfileMissingFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	// active_profile points at a non-existent profile: should warn and fall back.
	pc := &PersistentConfig{
		SchemaVersion: 2,
		Model:         "top-model",
		ActiveProfile: "missing",
		Profiles: map[string]ProfileOverride{
			"real": {Model: strPtr("real-model")},
		},
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "top-model" {
		t.Fatalf("expected fallback to top-model, got %q", c.Model)
	}
	if c.ActiveProfile != "" {
		t.Fatalf("expected empty active profile on fallback, got %q", c.ActiveProfile)
	}

	// Explicit flag with missing profile: hard error.
	_, err = LoadWithProfile("nope")
	if err == nil {
		t.Fatal("expected error for explicit missing profile")
	}
}

func TestProfileSparseMerge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top/v1",
		APIKey:        "top-key",
		Model:         "top-model",
		AutoCheckCmd:  "go build ./...",
		Profiles: map[string]ProfileOverride{
			"sparse": {
				Model:   strPtr("sparse-model"),
				LintCmd: strPtr("golangci-lint run"),
			},
		},
		ActiveProfile: "sparse",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	// Overridden fields.
	if c.Model != "sparse-model" {
		t.Fatalf("Model: got %q want sparse-model", c.Model)
	}
	if c.LintCmd != "golangci-lint run" {
		t.Fatalf("LintCmd: got %q want golangci-lint run", c.LintCmd)
	}
	// Inherited fields.
	if c.BaseURL != "http://top/v1" {
		t.Fatalf("BaseURL should be inherited: got %q", c.BaseURL)
	}
	if c.APIKey != "top-key" {
		t.Fatalf("APIKey should be inherited: got %q", c.APIKey)
	}
	if c.AutoCheckCmd != "go build ./..." {
		t.Fatalf("AutoCheckCmd should be inherited: got %q", c.AutoCheckCmd)
	}
}

func TestProfileBoolOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top/v1",
		Profiles: map[string]ProfileOverride{
			"strict": {
				GitAutoCommit:     boolPtr(false),
				PlanTot:           boolPtr(false),
				PlanReflection:    boolPtr(false),
				BuildSelfCritique: boolPtr(false),
				Plain:             boolPtr(true),
			},
		},
		ActiveProfile: "strict",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.GitAutoCommit {
		t.Fatal("expected GitAutoCommit=false from profile")
	}
	if c.PlanTot {
		t.Fatal("expected PlanTot=false from profile")
	}
	if c.PlanReflection {
		t.Fatal("expected PlanReflection=false from profile")
	}
	if c.BuildSelfCritique {
		t.Fatal("expected BuildSelfCritique=false from profile")
	}
	if !c.Plain {
		t.Fatal("expected Plain=true from profile")
	}
}

func TestProfileOverride_DisableIntentHeuristic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	tru := true
	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top/v1",
		// Top-level default is false (heuristic enabled).
		Profiles: map[string]ProfileOverride{
			"llm-only": {DisableIntentHeuristic: &tru},
		},
		ActiveProfile: "llm-only",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.DisableIntentHeuristic {
		t.Fatal("expected DisableIntentHeuristic=true from profile override")
	}
}

func TestProfileIntOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top/v1",
		Profiles: map[string]ProfileOverride{
			"custom": {
				ExecTimeoutSec: intPtr(60),
				ContextWindow:  intPtr(128000),
				MaxLLMRetries:  intPtr(5),
			},
		},
		ActiveProfile: "custom",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ExecTimeoutSeconds != 60 {
		t.Fatalf("ExecTimeoutSeconds: got %d want 60", c.ExecTimeoutSeconds)
	}
	if c.ContextWindowTokens != 128000 {
		t.Fatalf("ContextWindowTokens: got %d want 128000", c.ContextWindowTokens)
	}
	if c.MaxLLMRetries != 5 {
		t.Fatalf("MaxLLMRetries: got %d want 5", c.MaxLLMRetries)
	}
}

func TestSchemaMigrationV1ToV2(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	// Write a v1 config (no profiles).
	pc := &PersistentConfig{
		SchemaVersion: 1,
		BaseURL:       "http://old/v1",
		Model:         "old-model",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "old-model" {
		t.Fatalf("Model: got %q want old-model", c.Model)
	}
	if c.ActiveProfile != "" {
		t.Fatalf("expected empty profile for v1 config, got %q", c.ActiveProfile)
	}
	if len(c.Profiles) != 0 {
		t.Fatalf("expected no profiles for v1 config, got %d", len(c.Profiles))
	}

	// Reload persistent config and check migration.
	loaded, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 2 {
		t.Fatalf("expected schema version 2 after migration, got %d", loaded.SchemaVersion)
	}
}

func TestProfileNamesList(t *testing.T) {
	profiles := map[string]ProfileOverride{
		"beta":  {},
		"alpha": {},
		"gamma": {},
	}
	names := ProfileNamesList(profiles)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Fatalf("expected sorted names, got %v", names)
	}

	empty := ProfileNamesList(nil)
	if len(empty) != 0 {
		t.Fatalf("expected nil, got %v", empty)
	}
}

func TestProfilePreservesProfilesMap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_PROFILE", "")

	pc := &PersistentConfig{
		SchemaVersion: 2,
		BaseURL:       "http://top/v1",
		Profiles: map[string]ProfileOverride{
			"a": {Model: strPtr("model-a")},
			"b": {Model: strPtr("model-b")},
		},
		ActiveProfile: "a",
	}
	writeTestConfig(t, dir, pc)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 2 {
		t.Fatalf("expected 2 profiles on Config, got %d", len(c.Profiles))
	}
	if _, ok := c.Profiles["b"]; !ok {
		t.Fatal("profile 'b' should be present on Config")
	}
}

func TestProfileOverride_AutoCheckFixLoop(t *testing.T) {
	maxRetries := 5
	stopNoProg := false
	pc := &PersistentConfig{
		BaseURL: "http://test/v1",
		Model:   "m",
	}
	prof := &ProfileOverride{
		AutoCheckFixMaxRetries:   &maxRetries,
		AutoCheckFixStopOnNoProg: &stopNoProg,
	}
	merged := mergeProfileIntoPersistent(pc, prof)
	if merged.AutoCheckFixMaxRetries != 5 {
		t.Fatalf("expected AutoCheckFixMaxRetries=5 after merge, got %d", merged.AutoCheckFixMaxRetries)
	}
	if merged.AutoCheckFixStopOnNoProgress == nil || *merged.AutoCheckFixStopOnNoProgress != false {
		t.Fatal("expected AutoCheckFixStopOnNoProgress=false after merge")
	}
}
