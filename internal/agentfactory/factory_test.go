package agentfactory

import (
	"io"
	"strings"
	"testing"

	"codient/internal/config"
	"codient/internal/prompt"
)

func TestRegistryForMode_NoDelegateTask(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		MaxConcurrent: 1,
	}
	for _, mode := range []prompt.Mode{prompt.ModeBuild, prompt.ModeAsk, prompt.ModePlan} {
		reg := RegistryForMode(cfg, mode, nil)
		for _, name := range reg.Names() {
			if name == "delegate_task" {
				t.Fatalf("sub-agent registry for mode %q should NOT contain delegate_task", mode)
			}
		}
	}
}

func TestRegistryForMode_AskHasReadOnlyTools(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		ExecAllowlist: []string{"go"},
		MaxConcurrent: 1,
	}
	reg := RegistryForMode(cfg, prompt.ModeAsk, nil)
	names := reg.Names()
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["read_file"] {
		t.Fatal("ask mode should have read_file")
	}
	if !nameSet["grep"] {
		t.Fatal("ask mode should have grep")
	}
	if nameSet["write_file"] {
		t.Fatal("ask mode should NOT have write_file")
	}
	if nameSet["run_command"] {
		t.Fatal("ask mode should NOT have run_command")
	}
}

func TestRegistryForMode_BuildHasMutatingTools(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		ExecAllowlist: []string{"go"},
		MaxConcurrent: 1,
	}
	reg := RegistryForMode(cfg, prompt.ModeBuild, nil)
	names := reg.Names()
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["write_file"] {
		t.Fatal("build mode should have write_file")
	}
	if !nameSet["run_command"] {
		t.Fatal("build mode should have run_command")
	}
}

func TestRegistryForMode_PlanNoEcho(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		MaxConcurrent: 1,
	}
	reg := RegistryForMode(cfg, prompt.ModePlan, nil)
	for _, name := range reg.Names() {
		if name == "echo" {
			t.Fatal("plan mode should NOT have echo")
		}
	}
}

func TestSystemPromptForMode_ContainsModeSection(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		MaxConcurrent: 1,
	}
	for _, tc := range []struct {
		mode prompt.Mode
		want string
	}{
		{prompt.ModeBuild, "Build"},
		{prompt.ModeAsk, "Ask"},
		{prompt.ModePlan, "Plan"},
	} {
		reg := RegistryForMode(cfg, tc.mode, nil)
		sys := SystemPromptForMode(cfg, reg, tc.mode, "", io.Discard)
		if !strings.Contains(sys, tc.want) {
			t.Errorf("mode %s: system prompt should contain %q", tc.mode, tc.want)
		}
	}
}

func TestSystemPromptForMode_NoDelegationSection(t *testing.T) {
	cfg := &config.Config{
		Workspace:     t.TempDir(),
		MaxConcurrent: 1,
	}
	reg := RegistryForMode(cfg, prompt.ModeBuild, nil)
	sys := SystemPromptForMode(cfg, reg, prompt.ModeBuild, "", io.Discard)
	if strings.Contains(sys, "Task delegation") {
		t.Fatal("sub-agent system prompt should NOT contain Task delegation (no delegate_task tool)")
	}
}
