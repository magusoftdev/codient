package codientcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codient/internal/config"
	"codient/internal/skills"
)

func runSkillCommand(workspace string, args []string) int {
	if len(args) == 0 {
		printSkillHelp()
		return 0
	}
	switch args[0] {
	case "install":
		return runSkillInstall(args[1:])
	case "list":
		return runSkillList(workspace, args[1:])
	case "remove", "uninstall":
		return runSkillRemove(args[1:])
	case "help":
		printSkillHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown skill command: %s\n", args[0])
		printSkillHelp()
		return 1
	}
}

func printSkillHelp() {
	fmt.Fprintf(os.Stderr, "Usage: codient skill <command> [args]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  install <path|url>  Install a skill from a local path or git URL\n")
	fmt.Fprintf(os.Stderr, "  list                List all installed user and workspace skills\n")
	fmt.Fprintf(os.Stderr, "  remove <id>         Remove an installed user skill\n\n")
}

func runSkillList(workspace string, args []string) int {
	stateDir, err := config.StateDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: state dir: %v\n", err)
		return 1
	}
	// Note: runSkillList might be called without a full Config object if called directly from Run().
	// We'll try to resolve workspace from current dir if not provided.
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}
	
	entries, err := skills.Discover(stateDir, workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: skills: %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No skills found.\n")
		return 0
	}
	fmt.Printf("Discovered skills (%d):\n\n", len(entries))
	for _, e := range entries {
		mcpNote := ""
		if e.MCP != nil {
			mcpNote = " [MCP]"
		}
		fmt.Printf("  • %s [%s]%s\n    Path: %s\n    %s\n\n", e.Name, e.Scope, mcpNote, e.ReadPath, e.Description)
	}
	return 0
}

func runSkillInstall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: codient skill install <path|url>\n")
		return 1
	}
	source := args[0]
	stateDir, err := config.StateDir()
	if err != nil || stateDir == "" {
		fmt.Fprintf(os.Stderr, "codient: state dir: %v\n", err)
		return 1
	}
	userSkillsRoot := filepath.Join(stateDir, skills.UserSkillsSubdir)
	if err := os.MkdirAll(userSkillsRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "codient: mkdir: %v\n", err)
		return 1
	}

	// Simple heuristic for URL
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "git@") {
		return installFromGit(source, userSkillsRoot)
	}
	return installFromLocal(source, userSkillsRoot)
}

func installFromGit(url, destRoot string) int {
	// Extract a name from the URL
	u := strings.TrimSuffix(url, "/")
	u = strings.TrimSuffix(u, ".git")
	parts := strings.Split(u, "/")
	name := parts[len(parts)-1]
	if name == "" {
		fmt.Fprintf(os.Stderr, "codient: could not determine skill name from URL: %s\n", url)
		return 1
	}
	
	dest := filepath.Join(destRoot, name)
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(os.Stderr, "codient: skill %q already exists at %s\n", name, dest)
		return 1
	}

	fmt.Printf("Installing skill %q from %s...\n", name, url)
	cmd := exec.Command("git", "clone", "--depth", "1", url, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "codient: git clone failed: %v\n", err)
		return 1
	}
	
	// Validate installation
	if _, err := os.Stat(filepath.Join(dest, "skill.yaml")); err != nil {
		if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
			fmt.Fprintf(os.Stderr, "codient: warning: installed directory does not contain skill.yaml or SKILL.md\n")
		}
	}

	fmt.Printf("Successfully installed skill %q\n", name)
	return 0
}

func installFromLocal(path, destRoot string) int {
	absSrc, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: %v\n", err)
		return 1
	}
	fi, err := os.Stat(absSrc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: %v\n", err)
		return 1
	}
	if !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "codient: install source must be a directory\n")
		return 1
	}

	name := filepath.Base(absSrc)
	dest := filepath.Join(destRoot, name)
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(os.Stderr, "codient: skill %q already exists at %s\n", name, dest)
		return 1
	}

	fmt.Printf("Installing skill %q from %s...\n", name, absSrc)
	
	if err := copyDir(absSrc, dest); err != nil {
		fmt.Fprintf(os.Stderr, "codient: copy failed: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully installed skill %q\n", name)
	return 0
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func runSkillRemove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: codient skill remove <id>\n")
		return 1
	}
	id := args[0]
	stateDir, err := config.StateDir()
	if err != nil || stateDir == "" {
		fmt.Fprintf(os.Stderr, "codient: state dir: %v\n", err)
		return 1
	}
	
	dest := filepath.Join(stateDir, skills.UserSkillsSubdir, id)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "codient: user skill %q not found\n", id)
		return 1
	}

	fmt.Printf("Removing skill %q from %s...\n", id, dest)
	if err := os.RemoveAll(dest); err != nil {
		fmt.Fprintf(os.Stderr, "codient: remove failed: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully removed skill %q\n", id)
	return 0
}
