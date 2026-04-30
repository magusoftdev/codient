package codientcli

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"codient/internal/config"
	"codient/internal/prompt"
)

func TestBuildRegistry_ACPUnity_ReadToolsAllModes(t *testing.T) {
	call := func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}
	cfg := &config.Config{Workspace: t.TempDir()}
	s := &session{cfg: cfg, acpCallClient: call}
	wantTools := []string{
		"unity_query_scene_hierarchy",
		"unity_search_asset_database",
		"unity_inspect_component",
		"unity_list_loaded_scenes",
		"unity_query_prefab_hierarchy",
		"unity_get_console_errors",
		"unity_summarize_project_packages",
	}
	for _, mode := range []prompt.Mode{prompt.ModeAsk, prompt.ModePlan, prompt.ModeBuild} {
		reg := buildRegistry(cfg, mode, s, nil)
		names := reg.Names()
		for _, want := range wantTools {
			if !slices.Contains(names, want) {
				t.Fatalf("mode %s missing tool %q in %v", mode, want, names)
			}
		}
	}
}

func TestBuildRegistry_ACPUnity_ApplyToolBuildOnly(t *testing.T) {
	call := func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}
	cfg := &config.Config{Workspace: t.TempDir()}
	s := &session{cfg: cfg, acpCallClient: call}
	regAsk := buildRegistry(cfg, prompt.ModeAsk, s, nil)
	for _, n := range regAsk.Names() {
		if n == "unity_apply_actions" {
			t.Fatal("unity_apply_actions must not be registered in ask mode")
		}
	}
	regBuild := buildRegistry(cfg, prompt.ModeBuild, s, nil)
	found := false
	for _, n := range regBuild.Names() {
		if n == "unity_apply_actions" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected unity_apply_actions in build mode")
	}
}

func TestUnityQuerySceneTool_CallsClient(t *testing.T) {
	var gotMethod string
	var gotParams any
	call := func(_ context.Context, method string, params any) (json.RawMessage, error) {
		gotMethod = method
		gotParams = params
		return json.RawMessage(`{"nodes":[]}`), nil
	}
	cfg := &config.Config{Workspace: t.TempDir()}
	s := &session{cfg: cfg, acpCallClient: call}
	reg := buildRegistry(cfg, prompt.ModeAsk, s, nil)
	out, err := reg.Run(context.Background(), "unity_query_scene_hierarchy", []byte(`{"maxDepth":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "unity/query_scene_hierarchy" {
		t.Fatalf("method: got %q", gotMethod)
	}
	if out != `{"nodes":[]}` {
		t.Fatalf("output: %s", out)
	}
	if gotParams == nil {
		t.Fatal("expected params")
	}
}
