// Package gitutil provides lightweight git helpers (shells out to git, no library dependency).
package gitutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsRepo checks whether dir is inside a git working tree.
// Returns false gracefully when git is not on PATH.
func IsRepo(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// DiffSummary returns the output of `git diff --stat` in dir.
// Returns empty string if there are no changes or git is unavailable.
func DiffSummary(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", "--stat")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DiffFiles returns workspace-relative paths of tracked files with unstaged modifications.
func DiffFiles(dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(string(out)), nil
}

// UntrackedFiles returns workspace-relative paths of untracked files not covered by .gitignore.
func UntrackedFiles(dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(string(out)), nil
}

// RestoreFiles runs `git checkout -- <files...>` to restore specific tracked files.
func RestoreFiles(dir string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{"checkout", "--"}, files...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanUntracked runs `git clean -fd` to remove untracked files and directories.
func CleanUntracked(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clean", "-fd")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RestoreAll runs `git checkout -- .` to discard all unstaged changes in the working tree.
func RestoreAll(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "checkout", "--", ".")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func minimalGitEnv() []string {
	env := os.Environ()
	home, _ := os.UserHomeDir()
	if home != "" {
		env = append(env, "HOME="+home)
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot != "" {
		env = append(env, "SystemRoot="+systemRoot)
		env = append(env, "PATH="+filepath.Join(systemRoot, "system32")+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}
