package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionForMode_NoOverrides(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
	}
	base, key, model := c.ConnectionForMode("plan")
	if base != c.BaseURL {
		t.Fatalf("base: got %q want %q", base, c.BaseURL)
	}
	if key != c.APIKey {
		t.Fatalf("key: got %q want %q", key, c.APIKey)
	}
	if model != c.Model {
		t.Fatalf("model: got %q want %q", model, c.Model)
	}
}

func TestConnectionForMode_FullOverride(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
		ModeModels: map[string]ModeConnectionOverride{
			"plan": {
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-plan",
				Model:   "gpt-4.1",
			},
		},
	}
	base, key, model := c.ConnectionForMode("plan")
	if base != "https://api.openai.com/v1" {
		t.Fatalf("base: got %q", base)
	}
	if key != "sk-plan" {
		t.Fatalf("key: got %q", key)
	}
	if model != "gpt-4.1" {
		t.Fatalf("model: got %q", model)
	}
}

func TestConnectionForMode_PartialOverride(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
		ModeModels: map[string]ModeConnectionOverride{
			"plan": {
				Model: "gpt-4.1",
			},
		},
	}
	base, key, model := c.ConnectionForMode("plan")
	if base != c.BaseURL {
		t.Fatalf("base should inherit: got %q", base)
	}
	if key != c.APIKey {
		t.Fatalf("key should inherit: got %q", key)
	}
	if model != "gpt-4.1" {
		t.Fatalf("model: got %q", model)
	}
}

func TestConnectionForMode_UnknownModeFallsBack(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
		ModeModels: map[string]ModeConnectionOverride{
			"plan": {Model: "gpt-4.1"},
		},
	}
	base, key, model := c.ConnectionForMode("build")
	if base != c.BaseURL || key != c.APIKey || model != c.Model {
		t.Fatalf("unmatched mode should fall back to defaults: got %q %q %q", base, key, model)
	}
}

func TestConnectionForMode_WhitespaceStripped(t *testing.T) {
	c := &Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "key",
		Model:   "default-model",
		ModeModels: map[string]ModeConnectionOverride{
			"ask": {
				BaseURL: "  https://ask.example.com/v1  ",
				Model:   "  ask-model  ",
			},
		},
	}
	base, _, model := c.ConnectionForMode("ask")
	if base != "https://ask.example.com/v1" {
		t.Fatalf("base not trimmed: got %q", base)
	}
	if model != "ask-model" {
		t.Fatalf("model not trimmed: got %q", model)
	}
}

func TestConnectionForMode_AllThreeModes(t *testing.T) {
	c := &Config{
		BaseURL: "http://default/v1",
		APIKey:  "default-key",
		Model:   "default-model",
		ModeModels: map[string]ModeConnectionOverride{
			"build": {Model: "build-model"},
			"ask":   {Model: "ask-model", APIKey: "ask-key"},
			"plan":  {BaseURL: "http://plan/v1", Model: "plan-model"},
		},
	}
	for _, tc := range []struct {
		mode, wantBase, wantKey, wantModel string
	}{
		{"build", "http://default/v1", "default-key", "build-model"},
		{"ask", "http://default/v1", "ask-key", "ask-model"},
		{"plan", "http://plan/v1", "default-key", "plan-model"},
	} {
		base, key, model := c.ConnectionForMode(tc.mode)
		if base != tc.wantBase {
			t.Errorf("%s: base got %q want %q", tc.mode, base, tc.wantBase)
		}
		if key != tc.wantKey {
			t.Errorf("%s: key got %q want %q", tc.mode, key, tc.wantKey)
		}
		if model != tc.wantModel {
			t.Errorf("%s: model got %q want %q", tc.mode, model, tc.wantModel)
		}
	}
}

func TestModeModels_PersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL: "http://main/v1",
		APIKey:  "main-key",
		Model:   "main-model",
		Models: map[string]ModeConnectionOverride{
			"plan": {
				BaseURL: "http://plan/v1",
				APIKey:  "plan-key",
				Model:   "plan-model",
			},
			"ask": {
				Model: "ask-model",
			},
		},
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	models, ok := raw["models"]
	if !ok {
		t.Fatal("models key missing from persisted JSON")
	}
	modelsMap, ok := models.(map[string]any)
	if !ok || len(modelsMap) != 2 {
		t.Fatalf("expected 2 mode overrides, got %v", models)
	}

	loaded, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 2 {
		t.Fatalf("loaded Models len: %d", len(loaded.Models))
	}
	if loaded.Models["plan"].BaseURL != "http://plan/v1" {
		t.Fatalf("plan base_url: %q", loaded.Models["plan"].BaseURL)
	}
	if loaded.Models["plan"].APIKey != "plan-key" {
		t.Fatalf("plan api_key: %q", loaded.Models["plan"].APIKey)
	}
	if loaded.Models["plan"].Model != "plan-model" {
		t.Fatalf("plan model: %q", loaded.Models["plan"].Model)
	}
	if loaded.Models["ask"].Model != "ask-model" {
		t.Fatalf("ask model: %q", loaded.Models["ask"].Model)
	}
	if loaded.Models["ask"].BaseURL != "" {
		t.Fatalf("ask base_url should be empty: %q", loaded.Models["ask"].BaseURL)
	}
}

func TestModeModels_LoadedIntoConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL: "http://main/v1",
		APIKey:  "main-key",
		Model:   "main-model",
		Models: map[string]ModeConnectionOverride{
			"plan": {Model: "plan-model"},
		},
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModeModels == nil {
		t.Fatal("ModeModels is nil after Load")
	}
	if cfg.ModeModels["plan"].Model != "plan-model" {
		t.Fatalf("plan model: %q", cfg.ModeModels["plan"].Model)
	}
	base, _, model := cfg.ConnectionForMode("plan")
	if base != "http://main/v1" {
		t.Fatalf("plan base should inherit: %q", base)
	}
	if model != "plan-model" {
		t.Fatalf("plan model from ConnectionForMode: %q", model)
	}
}

func TestConfigToPersistent_ModeModels(t *testing.T) {
	cfg := &Config{
		BaseURL: "http://main/v1",
		APIKey:  "key",
		Model:   "main-model",
		ModeModels: map[string]ModeConnectionOverride{
			"build": {Model: "build-model"},
		},
	}
	pc := ConfigToPersistent(cfg)
	if pc.Models == nil {
		t.Fatal("Models is nil")
	}
	if pc.Models["build"].Model != "build-model" {
		t.Fatalf("build model: %q", pc.Models["build"].Model)
	}
}
