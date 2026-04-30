package codientcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codient/internal/config"
	"codient/internal/skills"
)

func (s *session) runListSkillsCommand() {
	sd, err := config.StateDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: state dir: %v\n", err)
		return
	}
	entries, err := skills.Discover(sd, s.cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: skills: %v\n", err)
		return
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No skills found. User: <state-dir>/%s/<id>/SKILL.md  Workspace: %s/<id>/SKILL.md\n",
			skills.UserSkillsSubdir, skills.WorkspaceSkillsRelDir)
		fmt.Fprintf(os.Stderr, "Create one with /create-skill\n")
		return
	}
	fmt.Fprintf(os.Stderr, "Discovered skills (%d):\n\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(os.Stderr, "  • %s [%s]\n    read_file: %s\n    %s\n\n", e.Name, e.Scope, e.ReadPath, e.Description)
	}
}

func (s *session) runCreateSkillWizard(sc *bufio.Scanner) {
	fmt.Fprintf(os.Stderr, "\n  ** Create a codient skill **\n\n")
	fmt.Fprintf(os.Stderr, "  Skills are folders with SKILL.md (YAML frontmatter + markdown). See codient docs for details.\n\n")

	purpose := strings.TrimSpace(promptWithDefault(sc, "  Purpose / when the agent should use this (fills ## Instructions starter)", ""))
	if purpose == "" {
		fmt.Fprintf(os.Stderr, "  Aborted (empty purpose).\n\n")
		return
	}

	ws := strings.TrimSpace(s.cfg.EffectiveWorkspace())
	defScope := "1"
	if ws == "" {
		defScope = "2"
	}
	fmt.Fprintf(os.Stderr, "\n  Scope — where to save the skill?\n")
	if ws != "" {
		fmt.Fprintf(os.Stderr, "    1) Workspace — %s/<id>/ (default when workspace is set)\n", skills.WorkspaceSkillsRelDir)
	} else {
		fmt.Fprintf(os.Stderr, "    1) Workspace — (unavailable: no workspace set)\n")
	}
	fmt.Fprintf(os.Stderr, "    2) User — <state-dir>/%s/<id>/ (available across projects)\n\n", skills.UserSkillsSubdir)

	scope := strings.TrimSpace(strings.ToLower(promptWithDefault(sc, "  Enter 1 or 2", defScope)))
	var useWorkspace bool
	switch scope {
	case "1", "w", "workspace":
		if ws == "" {
			fmt.Fprintf(os.Stderr, "  No workspace configured; use /config workspace <path> or choose 2.\n\n")
			return
		}
		useWorkspace = true
	case "2", "u", "user":
		useWorkspace = false
	default:
		fmt.Fprintf(os.Stderr, "  Invalid choice.\n\n")
		return
	}

	var skillID string
	for {
		skillID = strings.TrimSpace(promptWithDefault(sc, "  Skill folder id (lowercase, digits, hyphens; max 64 chars)", ""))
		if skillID == "" {
			fmt.Fprintf(os.Stderr, "  Aborted.\n\n")
			return
		}
		if !skills.ValidSkillID(skillID) {
			fmt.Fprintf(os.Stderr, "  Invalid id. Use only a-z, 0-9, and single hyphens between segments.\n")
			continue
		}
		break
	}

	var rootDir string
	if useWorkspace {
		absWs, err := filepath.Abs(ws)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  workspace: %v\n\n", err)
			return
		}
		rootDir = filepath.Join(absWs, skills.WorkspaceSkillsRelDir, skillID)
	} else {
		sd, err := config.StateDir()
		if err != nil || sd == "" {
			fmt.Fprintf(os.Stderr, "  state dir: %v\n\n", err)
			return
		}
		rootDir = filepath.Join(sd, skills.UserSkillsSubdir, skillID)
	}

	skillPath := filepath.Join(rootDir, "SKILL.md")
	if _, err := os.Stat(skillPath); err == nil {
		fmt.Fprintf(os.Stderr, "  Already exists: %s — pick another id.\n\n", skillPath)
		return
	}

	desc := strings.TrimSpace(promptWithDefault(sc, "  YAML description (third person; say what it does and when to use it)", ""))
	if desc == "" {
		desc = fmt.Sprintf("Guidance for tasks related to %s.", skillID)
	}

	dmiAns := strings.ToLower(strings.TrimSpace(promptWithDefault(sc, "  Set disable-model-invocation in frontmatter? (y/N)", "n")))
	disableMI := dmiAns == "y" || dmiAns == "yes"

	var fm strings.Builder
	fm.WriteString("---\n")
	fmt.Fprintf(&fm, "name: %s\n", skillID)
	fm.WriteString("description: |\n")
	for _, line := range strings.Split(desc, "\n") {
		fm.WriteString("  ")
		fm.WriteString(strings.TrimRight(line, "\r"))
		fm.WriteString("\n")
	}
	if disableMI {
		fm.WriteString("disable-model-invocation: true\n")
	}
	fm.WriteString("---\n\n")
	fmt.Fprintf(&fm, "# %s\n\n## Instructions\n\n%s\n\n## Examples\n\n- \n", skillID, purpose)

	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  mkdir: %v\n\n", err)
		return
	}
	if err := os.WriteFile(skillPath, []byte(fm.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  write: %v\n\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "\n  Wrote %s\n", skillPath)
	fmt.Fprintf(os.Stderr, "  Edit SKILL.md to refine. The agent sees your skill in **Agent skills** and should read_file this path when relevant.\n\n")

	s.refreshSkillsCatalog()
}
