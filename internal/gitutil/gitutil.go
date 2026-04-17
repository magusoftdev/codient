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

// DiffShortStat returns a one-line summary like "3 files changed, 10 insertions(+), 2 deletions(-)" for the working tree.
func DiffShortStat(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "diff", "--shortstat", "HEAD")
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

// WorkingTreeDirty returns true when the working tree differs from HEAD or has untracked files.
func WorkingTreeDirty(dir string) bool {
	short, err := DiffShortStat(dir)
	if err == nil && strings.TrimSpace(short) != "" {
		return true
	}
	u, err := UntrackedFiles(dir)
	return err == nil && len(u) > 0
}

// StashPush runs `git stash push -u -m <message>` to stash tracked and untracked changes.
// If there is nothing to stash, it returns nil without error.
func StashPush(dir, message string) error {
	if !WorkingTreeDirty(dir) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if strings.TrimSpace(message) == "" {
		message = "codient stash"
	}
	cmd := exec.CommandContext(ctx, "git", "stash", "push", "-u", "-m", message)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git stash: %w: %s", err, strings.TrimSpace(string(out)))
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

// HeadSHA returns the full SHA of HEAD.
func HeadSHA(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the short name of the current branch, or detached HEAD short SHA.
func CurrentBranch(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchExists reports whether a local branch named name exists.
func BranchExists(dir, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// CreateBranch creates and checks out a new branch from the current HEAD.
func CreateBranch(dir, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", name)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CheckoutBranch switches to an existing local branch.
func CheckoutBranch(dir, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "checkout", name)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RenameBranch renames the current branch to newName.
func RenameBranch(dir, newName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "branch", "-m", newName)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -m: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Add stages paths (workspace-relative) for commit.
func Add(dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := append([]string{"add", "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Commit creates a commit with the given subject and optional body (already staged).
func Commit(dir, subject, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	body = strings.TrimSpace(body)
	if body == "" {
		cmd = exec.CommandContext(ctx, "git", "commit", "-m", subject)
	} else {
		cmd = exec.CommandContext(ctx, "git", "commit", "-m", subject, "-m", body)
	}
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ResetHard resets the branch to rev (e.g. commit SHA).
func ResetHard(dir, rev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", rev)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ResetHardParent moves HEAD back one commit and resets the working tree.
func ResetHardParent(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", "HEAD~1")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard HEAD~1: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// LogOneLine returns the first line of git log for rev..HEAD (exclusive rev, inclusive HEAD).
func LogOneLine(dir, revRange string, max int) ([]string, error) {
	if max < 1 {
		max = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "-n", fmt.Sprintf("%d", max), revRange)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(string(out)), nil
}

// DiffStatCommits returns `git diff --stat older..newer` (e.g. older=HEAD~1, newer=HEAD for the last commit).
func DiffStatCommits(dir, older, newer string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rng := older + ".." + newer
	cmd := exec.CommandContext(ctx, "git", "diff", "--stat", rng)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RefExists returns whether rev resolves to an object in the repo.
func RefExists(dir, rev string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", rev)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	return cmd.Run() == nil
}

// EmptyTreeSHA is the hash of Git's empty tree object (used to diff the first commit).
const EmptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// DiffStatLastCommit returns --stat for the latest commit (handles root commits).
func DiffStatLastCommit(dir string) (string, error) {
	if RefExists(dir, "HEAD~1") {
		return DiffStatCommits(dir, "HEAD~1", "HEAD")
	}
	return DiffStatCommits(dir, EmptyTreeSHA, "HEAD")
}

// ShowPatch returns colored unified diff for commit, truncated to maxLines (0 = no limit).
func ShowPatch(dir, commit string, maxLines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "show", "--color=always", "--format=", commit)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	s := string(out)
	if maxLines <= 0 {
		return strings.TrimRight(s, "\n"), nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return strings.TrimRight(s, "\n"), nil
	}
	trunc := strings.Join(lines[:maxLines], "\n")
	rest := len(lines) - maxLines
	return trunc + fmt.Sprintf("\n\n... %d more lines (truncated)", rest), nil
}

// DiffUnifiedWorkspace returns colored diff of working tree vs HEAD (staged and unstaged).
func DiffUnifiedWorkspace(dir string, maxLines int) (string, error) {
	return diffUnified(dir, []string{"diff", "--color=always", "HEAD"}, maxLines)
}

// DiffUnifiedFile returns colored diff for a single path (workspace-relative) vs HEAD.
func DiffUnifiedFile(dir, relPath string, maxLines int) (string, error) {
	return diffUnified(dir, []string{"diff", "--color=always", "HEAD", "--", relPath}, maxLines)
}

func diffUnified(dir string, gitArgs []string, maxLines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimRight(string(out), "\n")
	if maxLines <= 0 {
		return s, nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s, nil
	}
	trunc := strings.Join(lines[:maxLines], "\n")
	rest := len(lines) - maxLines
	return trunc + fmt.Sprintf("\n\n... %d more lines (truncated)", rest), nil
}

// DefaultRemoteBranch tries to resolve the default branch name (e.g. main) from origin/HEAD.
func DefaultRemoteBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	ref := strings.TrimSpace(string(out))
	// refs/remotes/origin/main -> main
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return "main"
}

// PushUpstream pushes the current branch to origin with -u.
func PushUpstream(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "push", "-u", "origin", "HEAD")
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GhAvailable reports whether the gh CLI is on PATH.
func GhAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// GhPRCreate runs `gh pr create` and returns the PR URL from stdout.
func GhPRCreate(dir string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = minimalGitEnv()
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("gh: %w: %s", err, s)
	}
	return s, nil
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
