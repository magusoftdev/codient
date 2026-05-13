package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter_basic(t *testing.T) {
	raw := "---\nname: my-skill\ndescription: Does a thing.\n---\n\n# Body\n\nHello.\n"
	fm, body, err := ParseFrontmatter([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "my-skill" || fm.Description != "Does a thing." {
		t.Fatalf("fm=%+v", fm)
	}
	if !strings.Contains(body, "Hello.") {
		t.Fatalf("body=%q", body)
	}
}

func TestParseFrontmatter_multilineDescription(t *testing.T) {
	raw := "---\nname: x\ndescription: >-\n  Line one\n  Line two\n---\n\nOK\n"
	fm, _, err := ParseFrontmatter([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.Description, "Line one") {
		t.Fatalf("desc=%q", fm.Description)
	}
}

func TestParseFrontmatter_noBlock(t *testing.T) {
	raw := "# Just markdown\n\nNo yaml.\n"
	fm, body, err := ParseFrontmatter([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "" || fm.Description != "" {
		t.Fatalf("expected empty fm, got %+v", fm)
	}
	if !strings.Contains(body, "Just markdown") {
		t.Fatalf("body=%q", body)
	}
}

func TestDiscover_workspaceOverridesUserByName(t *testing.T) {
	stateDir := t.TempDir()
	ws := t.TempDir()
	// user skill
	uDir := filepath.Join(stateDir, UserSkillsSubdir, "u1")
	if err := os.MkdirAll(uDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userMD := "---\nname: shared-name\ndescription: from user\n---\n\n"
	if err := os.WriteFile(filepath.Join(uDir, "SKILL.md"), []byte(userMD), 0o644); err != nil {
		t.Fatal(err)
	}
	// workspace skill same logical name
	wDir := filepath.Join(ws, WorkspaceSkillsRelDir, "w1")
	if err := os.MkdirAll(wDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wsMD := "---\nname: shared-name\ndescription: from workspace\n---\n\n"
	if err := os.WriteFile(filepath.Join(wDir, "SKILL.md"), []byte(wsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	ent, err := Discover(stateDir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(ent) != 1 {
		t.Fatalf("got %d entries: %+v", len(ent), ent)
	}
	if ent[0].Scope != "workspace" || ent[0].Description != "from workspace" {
		t.Fatalf("wrong winner: %+v", ent[0])
	}
	wantPath := filepath.ToSlash(filepath.Join(WorkspaceSkillsRelDir, "w1", "SKILL.md"))
	if ent[0].ReadPath != wantPath {
		t.Fatalf("ReadPath=%q want %q", ent[0].ReadPath, wantPath)
	}
}

func TestDiscover_skillYAML(t *testing.T) {
	stateDir := t.TempDir()
	ws := t.TempDir()

	// 1. User skill with skill.yaml (modern)
	u1Dir := filepath.Join(stateDir, UserSkillsSubdir, "u1")
	if err := os.MkdirAll(u1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	u1YAML := `
name: yaml-skill
description: from yaml
instructions: custom.md
mcp:
  command: npx
  args: ["-y", "@mcp/server-test"]
  env:
    FOO: BAR
`
	if err := os.WriteFile(filepath.Join(u1Dir, "skill.yaml"), []byte(u1YAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Workspace skill with SKILL.md (legacy)
	w1Dir := filepath.Join(ws, WorkspaceSkillsRelDir, "w1")
	if err := os.MkdirAll(w1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w1MD := "---\nname: md-skill\ndescription: from md\n---\n\nBody"
	if err := os.WriteFile(filepath.Join(w1Dir, "SKILL.md"), []byte(w1MD), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Skill with BOTH (yaml should win)
	w2Dir := filepath.Join(ws, WorkspaceSkillsRelDir, "w2")
	if err := os.MkdirAll(w2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w2YAML := "name: win-yaml\ndescription: win"
	w2MD := "---\nname: lose-md\ndescription: lose\n---\n"
	if err := os.WriteFile(filepath.Join(w2Dir, "skill.yaml"), []byte(w2YAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w2Dir, "SKILL.md"), []byte(w2MD), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := Discover(stateDir, ws)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 3 skills (yaml-skill, md-skill, win-yaml)
	if len(entries) != 3 {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}

	// Check modern skill (u1)
	foundU1 := false
	for _, e := range entries {
		if e.Name == "yaml-skill" {
			foundU1 = true
			if e.Description != "from yaml" {
				t.Errorf("u1: wrong desc %q", e.Description)
			}
			if e.ReadPath != "u1/custom.md" {
				t.Errorf("u1: wrong readPath %q", e.ReadPath)
			}
			if e.MCP == nil || e.MCP.Command != "npx" || e.MCP.Env["FOO"] != "BAR" {
				t.Errorf("u1: wrong MCP %+v", e.MCP)
			}
		}
	}
	if !foundU1 {
		t.Error("yaml-skill not found")
	}

	// Check legacy skill (w1)
	foundW1 := false
	for _, e := range entries {
		if e.Name == "md-skill" {
			foundW1 = true
			if e.Description != "from md" {
				t.Errorf("w1: wrong desc %q", e.Description)
			}
			wantPath := filepath.ToSlash(filepath.Join(WorkspaceSkillsRelDir, "w1", "SKILL.md"))
			if e.ReadPath != wantPath {
				t.Errorf("w1: wrong readPath %q", e.ReadPath)
			}
		}
	}
	if !foundW1 {
		t.Error("md-skill not found")
	}

	// Check winner (w2)
	foundW2 := false
	for _, e := range entries {
		if e.Name == "win-yaml" {
			foundW2 = true
			if e.Description != "win" {
				t.Errorf("w2: wrong desc %q", e.Description)
			}
		}
	}
	if !foundW2 {
		t.Error("win-yaml not found")
	}
}

func TestFormatCatalog_truncation(t *testing.T) {
	var entries []Entry
	for i := 0; i < 500; i++ {
		entries = append(entries, Entry{
			ID:          "x",
			Name:        "n",
			Description: strings.Repeat("a", 200),
			Scope:       "workspace",
			ReadPath:    "p",
		})
	}
	s := FormatCatalog(entries)
	if len(s) > MaxCatalogBytes+200 {
		t.Fatalf("catalog too long: %d", len(s))
	}
	if !strings.Contains(s, "[truncated") {
		t.Fatal("expected truncation marker")
	}
}

func TestValidSkillID(t *testing.T) {
	if !ValidSkillID("foo-bar") || ValidSkillID("Foo") || ValidSkillID("foo_bar") {
		t.Fatal("ValidSkillID mismatch")
	}
}
