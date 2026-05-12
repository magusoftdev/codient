package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionForMode_NoOverridesFallsThrough(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
	}
	for _, mode := range []string{"build", "ask", "plan", ""} {
		base, key, model := c.ConnectionForMode(mode)
		if base != c.BaseURL || key != c.APIKey || model != c.Model {
			t.Fatalf("%s: got %q %q %q (want %q %q %q)", mode, base, key, model, c.BaseURL, c.APIKey, c.Model)
		}
	}
}

func TestConnectionForMode_RoutesByReasoningTier(t *testing.T) {
	c := &Config{
		BaseURL:       "http://main/v1",
		APIKey:        "main-key",
		Model:         "main-model",
		LowReasoning:  ReasoningTier{BaseURL: "http://low/v1", APIKey: "low-k", Model: "low-m"},
		HighReasoning: ReasoningTier{Model: "high-m"},
	}
	for _, tc := range []struct {
		mode, wantBase, wantKey, wantModel string
	}{
		// build / ask resolve through the low tier (full override applied).
		{"build", "http://low/v1", "low-k", "low-m"},
		{"ask", "http://low/v1", "low-k", "low-m"},
		// plan resolves through the high tier; only Model is overridden so
		// base/key inherit the top-level connection.
		{"plan", "http://main/v1", "main-key", "high-m"},
	} {
		base, key, model := c.ConnectionForMode(tc.mode)
		if base != tc.wantBase || key != tc.wantKey || model != tc.wantModel {
			t.Errorf("mode=%s: got (%q,%q,%q) want (%q,%q,%q)",
				tc.mode, base, key, model, tc.wantBase, tc.wantKey, tc.wantModel)
		}
	}
}

func TestConnectionForMode_UnknownModeUsesTopLevel(t *testing.T) {
	c := &Config{
		BaseURL:      "http://main/v1",
		APIKey:       "k",
		Model:        "m",
		LowReasoning: ReasoningTier{Model: "low-m"},
	}
	base, key, model := c.ConnectionForMode("auto")
	if base != "http://main/v1" || key != "k" || model != "m" {
		t.Fatalf("unknown mode should fall back to top-level: %q %q %q", base, key, model)
	}
}

func TestConnectionForTier_NoOverridesFallsBackToTopLevel(t *testing.T) {
	c := &Config{BaseURL: "http://main/v1", APIKey: "k", Model: "m"}
	for _, tier := range []string{TierLow, TierHigh, "unknown"} {
		base, key, model := c.ConnectionForTier(tier)
		if base != "http://main/v1" || key != "k" || model != "m" {
			t.Fatalf("%s: got %q %q %q", tier, base, key, model)
		}
	}
}

func TestConnectionForTier_TierOverrides(t *testing.T) {
	c := &Config{
		BaseURL:       "http://main/v1",
		APIKey:        "k",
		Model:         "m",
		LowReasoning:  ReasoningTier{Model: "low-m", BaseURL: "http://low/v1", APIKey: "low-k"},
		HighReasoning: ReasoningTier{Model: "high-m"},
	}
	base, key, model := c.ConnectionForTier(TierLow)
	if base != "http://low/v1" || key != "low-k" || model != "low-m" {
		t.Fatalf("low tier: %q %q %q", base, key, model)
	}
	base, key, model = c.ConnectionForTier(TierHigh)
	if base != "http://main/v1" || key != "k" || model != "high-m" {
		t.Fatalf("high tier inherit: %q %q %q", base, key, model)
	}
}

func TestReasoningTier_PersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL:                         "http://main/v1",
		APIKey:                          "main-key",
		Model:                           "main-model",
		LowReasoningModel:               "low-m",
		LowReasoningBaseURL:             "http://low/v1",
		LowReasoningAPIKey:              "low-k",
		LowReasoningMaxCompletionTokens: 512,
		HighReasoningModel:              "high-m",
		HighReasoningBaseURL:            "http://high/v1",
		HighReasoningAPIKey:             "high-k",
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LowReasoningModel != "low-m" || loaded.LowReasoningBaseURL != "http://low/v1" || loaded.LowReasoningAPIKey != "low-k" {
		t.Fatalf("low round-trip: %+v", loaded)
	}
	if loaded.HighReasoningModel != "high-m" || loaded.HighReasoningBaseURL != "http://high/v1" || loaded.HighReasoningAPIKey != "high-k" {
		t.Fatalf("high round-trip: %+v", loaded)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LowReasoning.Model != "low-m" {
		t.Fatalf("cfg.LowReasoning.Model: %q", cfg.LowReasoning.Model)
	}
	if cfg.LowReasoning.MaxCompletionTokens != 512 {
		t.Fatalf("cfg.LowReasoning.MaxCompletionTokens: %d", cfg.LowReasoning.MaxCompletionTokens)
	}
	if cfg.HighReasoning.Model != "high-m" {
		t.Fatalf("cfg.HighReasoning.Model: %q", cfg.HighReasoning.Model)
	}
	pc2 := ConfigToPersistent(cfg)
	if pc2.LowReasoningModel != "low-m" || pc2.HighReasoningModel != "high-m" {
		t.Fatalf("ConfigToPersistent dropped tiers: %+v", pc2)
	}
	if pc2.LowReasoningMaxCompletionTokens != 512 {
		t.Fatalf("ConfigToPersistent dropped LowReasoningMaxCompletionTokens: %d", pc2.LowReasoningMaxCompletionTokens)
	}
}

func TestLowReasoningMaxCompletionTokens_Clamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL:                         "http://main/v1",
		APIKey:                          "k",
		Model:                           "m",
		LowReasoningMaxCompletionTokens: 10_000, // way above MaxSupervisorMaxCompletionTokens
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LowReasoning.MaxCompletionTokens != MaxSupervisorMaxCompletionTokens {
		t.Fatalf("clamp: got %d, want %d", cfg.LowReasoning.MaxCompletionTokens, MaxSupervisorMaxCompletionTokens)
	}
}

// TestDisableIntentHeuristic_PersistRoundTrip ensures the new config knob
// persists across SavePersistentConfig → Load → ConfigToPersistent.
func TestDisableIntentHeuristic_PersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL:                "http://main/v1",
		APIKey:                 "k",
		Model:                  "m",
		DisableIntentHeuristic: true,
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DisableIntentHeuristic {
		t.Fatalf("cfg.DisableIntentHeuristic: got false, want true")
	}
	pc2 := ConfigToPersistent(cfg)
	if !pc2.DisableIntentHeuristic {
		t.Fatalf("ConfigToPersistent dropped DisableIntentHeuristic")
	}
}

// TestDisableIntentHeuristic_DefaultsToFalse confirms the in-code default
// is false (heuristic enabled). A user with no override gets the fast path.
func TestDisableIntentHeuristic_DefaultsToFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL: "http://main/v1",
		APIKey:  "k",
		Model:   "m",
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisableIntentHeuristic {
		t.Fatalf("cfg.DisableIntentHeuristic: got true, want false (default)")
	}
}

func TestLowReasoningMaxCompletionTokens_NegativeFallsToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL:                         "http://main/v1",
		APIKey:                          "k",
		Model:                           "m",
		LowReasoningMaxCompletionTokens: -1,
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LowReasoning.MaxCompletionTokens != 0 {
		t.Fatalf("negative value should sanitize to 0, got %d", cfg.LowReasoning.MaxCompletionTokens)
	}
}

// TestLegacyModeModels_StillParsedForDeprecationWarning verifies that an old
// `models` block in config.json is still parsed (so config.Load can warn the
// user) but is not surfaced on the runtime Config or honored by
// ConnectionForMode.
func TestLegacyModeModels_StillParsedForDeprecationWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	raw := []byte(`{
  "base_url": "http://main/v1",
  "api_key": "main-key",
  "model": "main-model",
  "models": {
    "plan": {"model": "legacy-plan-model"}
  }
}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Models["plan"].Model != "legacy-plan-model" {
		t.Fatalf("PersistentConfig should still parse legacy models block, got %+v", loaded.Models)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Legacy ModeModels must not influence runtime resolution: plan -> high
	// tier -> top-level model, not the legacy "legacy-plan-model".
	_, _, model := cfg.ConnectionForMode("plan")
	if model != "main-model" {
		t.Fatalf("legacy models block should be ignored at runtime, got %q", model)
	}
}

func TestLegacyModeField_IgnoredAtRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL: "http://main/v1",
		APIKey:  "k",
		Model:   "m",
		Mode:    "plan",
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyMode != "plan" {
		t.Fatalf("LegacyMode should preserve raw value, got %q", cfg.LegacyMode)
	}
	// The runtime mode is decided by the orchestrator per turn; cfg has no
	// authoritative Mode field. The persisted JSON should retain the legacy
	// key only if explicitly set (we re-marshal verbatim).
	pcOut := ConfigToPersistent(cfg)
	if pcOut.Mode != "" {
		t.Fatalf("ConfigToPersistent should drop legacy Mode, got %q", pcOut.Mode)
	}
	// Sanity: the JSON we saved still parses without error.
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["mode"]; !ok {
		t.Fatal("expected legacy mode key on disk")
	}
}
