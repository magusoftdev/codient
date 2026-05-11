package prompt

import (
	"strings"
	"testing"

	"codient/internal/config"
	"codient/internal/tools"
)

func TestBuild_IncludesToolsAndUserSystem(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	reg := tools.Default("/tmp/w", "", nil, nil, nil, "", nil, nil, nil, nil, nil)
	s := Build(Params{Cfg: cfg, Reg: reg, Mode: ModeBuild, UserSystem: "custom", RepoInstructions: ""})
	if !strings.Contains(s, "echo") {
		t.Fatalf("missing tool name: %s", s)
	}
	if !strings.Contains(s, "custom") {
		t.Fatalf("missing user system: %s", s)
	}
	if !strings.Contains(s, "## Context") {
		t.Fatalf("missing persona section")
	}
}

func TestBuild_RepoInstructions(t *testing.T) {
	cfg := &config.Config{}
	reg := tools.NewRegistry()
	s := Build(Params{Cfg: cfg, Reg: reg, Mode: ModeBuild, RepoInstructions: "Use tabs."})
	if !strings.Contains(s, "Repository instructions") || !strings.Contains(s, "Use tabs.") {
		t.Fatalf("got %s", s)
	}
}

func TestBuild_SkillsCatalog(t *testing.T) {
	cfg := &config.Config{}
	reg := tools.NewRegistry()
	cat := "## Agent skills\n\n- **x**: do y — `read_file` path: `z`"
	s := Build(Params{Cfg: cfg, Reg: reg, Mode: ModeBuild, SkillsCatalog: cat})
	if !strings.Contains(s, "## Agent skills") || !strings.Contains(s, "do y") {
		t.Fatalf("missing skills section: %s", s)
	}
	// Skills block should appear after repository instructions when both set.
	s2 := Build(Params{Cfg: cfg, Reg: reg, Mode: ModeBuild, RepoInstructions: "repo", SkillsCatalog: cat})
	iRepo := strings.Index(s2, "Repository instructions")
	iSkill := strings.Index(s2, "## Agent skills")
	if iRepo < 0 || iSkill < 0 || iSkill < iRepo {
		t.Fatalf("expected repo instructions before skills: repo=%d skill=%d", iRepo, iSkill)
	}
}

func TestBuild_ModePlan_IncludesPlanSection(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	reg := tools.DefaultReadOnlyPlan("/tmp/w", "", nil, nil, "", nil, nil, nil, nil)
	s := Build(Params{Cfg: cfg, Reg: reg, Mode: ModePlan})
	if !strings.Contains(s, "## Plan mode") || !strings.Contains(s, "Blocking clarification") {
		t.Fatalf("missing plan section: %s", s)
	}
	if !strings.Contains(s, "Required written design") || !strings.Contains(s, "does not remember past runs") {
		t.Fatalf("missing required design guidance: %s", s)
	}
	if !strings.Contains(s, "Underspecified user goal") {
		t.Fatalf("missing question-first guidance: %s", s)
	}
	if !strings.Contains(s, "Ready to implement") {
		t.Fatalf("missing completion guidance: %s", s)
	}
	for _, n := range reg.Names() {
		if n == "echo" {
			t.Fatal("plan registry must omit echo")
		}
	}
	if !strings.Contains(s, "**Plan** mode") {
		t.Fatalf("missing session plan line: %s", s)
	}
}

// TestBuild_NoLegacyModeBuildReferences guards against regressions that
// reintroduce the long-removed `-mode build` / `--mode build` flag or the
// `/build` slash command into the system prompt. The orchestrator now drives
// plan -> build transitions implicitly; both surfaces have been removed.
func TestBuild_NoLegacyModeBuildReferences(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	for _, m := range []Mode{ModeAsk, ModePlan, ModeBuild} {
		var reg *tools.Registry
		switch m {
		case ModeAsk:
			reg = tools.DefaultReadOnly("/tmp/w", "", nil, nil, "", nil, nil, nil, nil)
		case ModePlan:
			reg = tools.DefaultReadOnlyPlan("/tmp/w", "", nil, nil, "", nil, nil, nil, nil)
		default:
			reg = tools.Default("/tmp/w", "", nil, nil, nil, "", nil, nil, nil, nil, nil)
		}
		s := Build(Params{Cfg: cfg, Reg: reg, Mode: m})
		for _, banned := range []string{"-mode build", "--mode build", "`/build`", "/build to "} {
			if strings.Contains(s, banned) {
				t.Errorf("mode=%s: system prompt should not reference %q", m, banned)
			}
		}
	}
}

func TestBuild_ModeAsk_ReadOnlySections(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	reg := tools.DefaultReadOnly("/tmp/w", "", nil, nil, "", nil, nil, nil, nil)
	s := Build(Params{Cfg: cfg, Reg: reg, Mode: ModeAsk})
	if !strings.Contains(s, "## Scope (read-only)") || !strings.Contains(s, "**Ask** mode") {
		t.Fatalf("missing ask/read-only: %s", s)
	}
	if strings.Contains(s, "## Plan mode") {
		t.Fatal("ask mode should not include Plan mode section")
	}
}

func TestBuild_AutoCheckNote(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	reg := tools.Default("/tmp/w", "", nil, nil, nil, "", nil, nil, nil, nil, nil)
	s := Build(Params{
		Cfg: cfg, Reg: reg, Mode: ModeBuild,
		AutoCheckBuildResolved: "go build ./...",
		AutoCheckLintResolved:  "golangci-lint run ./...",
		AutoCheckTestResolved:  "go test ./...",
	})
	if !strings.Contains(s, "Auto-check") || !strings.Contains(s, "go build ./...") || !strings.Contains(s, "[auto-check]") {
		t.Fatalf("expected auto-check note: %s", s)
	}
	if !strings.Contains(s, "golangci-lint") || !strings.Contains(s, "go test ./...") || !strings.Contains(s, "fail-fast") {
		t.Fatalf("expected lint/test in auto-check note: %s", s)
	}
	if strings.Contains(s, "retries") {
		t.Fatal("expected no fix-loop wording when AutoCheckFixMaxRetries is 0")
	}
}

func TestBuild_AutoCheckFixLoopNote(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/w"}
	reg := tools.Default("/tmp/w", "", nil, nil, nil, "", nil, nil, nil, nil, nil)
	s := Build(Params{
		Cfg: cfg, Reg: reg, Mode: ModeBuild,
		AutoCheckBuildResolved: "go build ./...",
		AutoCheckTestResolved:  "go test ./...",
		AutoCheckFixMaxRetries: 3,
	})
	if !strings.Contains(s, "retries up to **3** times") {
		t.Fatalf("expected fix-loop wording with cap 3: %s", s)
	}
	if !strings.Contains(s, "stop editing files") {
		t.Fatalf("expected exhaustion guidance in auto-check note: %s", s)
	}
}
