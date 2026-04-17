package codientcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codient/internal/gitutil"
	"codient/internal/prompt"
	"codient/internal/tools"
)

func (s *session) captureGitSessionState(ws string) {
	s.gitMergeTargetBranch = ""
	s.gitCodientCreatedBranch = ""
	s.gitBranchEnsured = false
	if ws == "" || !gitutil.IsRepo(ws) {
		s.gitSessionStartCommit = ""
		s.gitSessionStartBranch = ""
		return
	}
	sha, err := gitutil.HeadSHA(ws)
	if err != nil {
		s.gitSessionStartCommit = ""
		s.gitSessionStartBranch = ""
		return
	}
	br, err := gitutil.CurrentBranch(ws)
	if err != nil {
		s.gitSessionStartCommit = ""
		s.gitSessionStartBranch = ""
		return
	}
	s.gitSessionStartCommit = sha
	s.gitSessionStartBranch = br
}

func (s *session) isProtectedBranch(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range s.cfg.GitProtectedBranches {
		if n == strings.ToLower(strings.TrimSpace(p)) {
			return true
		}
	}
	return false
}

func (s *session) ensureCodientBranch(ws string) error {
	if s.gitBranchEnsured {
		return nil
	}
	s.gitBranchEnsured = true

	cur, err := gitutil.CurrentBranch(ws)
	if err != nil {
		return err
	}
	if !s.isProtectedBranch(cur) {
		return nil
	}

	protectedLeft := cur
	s.gitMergeTargetBranch = protectedLeft

	slug := strings.TrimSpace(s.taskSlug)
	if slug == "" {
		slug = strings.TrimSpace(s.sessionID)
		if len(slug) > 12 {
			slug = slug[:12]
		}
		if slug == "" {
			slug = "session"
		}
	}
	base := "codient/" + slug
	for i := 0; i < 50; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		exists, err := gitutil.BranchExists(ws, name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := gitutil.CreateBranch(ws, name); err != nil {
			return err
		}
		s.gitCodientCreatedBranch = name
		fmt.Fprintf(os.Stderr, "codient: created branch %q (left protected branch %q)\n", name, protectedLeft)
		return nil
	}
	return fmt.Errorf("could not find a free codient/* branch name")
}

func (s *session) resolvePRBase(ws string) string {
	if b := strings.TrimSpace(s.gitMergeTargetBranch); b != "" {
		return b
	}
	return gitutil.DefaultRemoteBranch(ws)
}

func buildCodientCommitMessage(turn int, userLine string) (subject, body string) {
	subject = fmt.Sprintf("codient: turn %d", turn)
	body = strings.TrimSpace(userLine)
	if body == "" {
		return subject, ""
	}
	runes := []rune(body)
	if len(runes) > 200 {
		body = string(runes[:200]) + "…"
	}
	return subject, body
}

func (s *session) pushUndoIfChanged(preModified, preUntracked []string, histLen int, userLine string) {
	s.lastBuildTurnHadChanges = false
	s.lastTurnGitCommit = false

	postModified, postUntracked := s.captureSnapshot()
	entry := computeUndoEntry(preModified, preUntracked, postModified, postUntracked, histLen)
	if entry == nil {
		return
	}
	s.lastBuildTurnHadChanges = true

	ws := s.cfg.EffectiveWorkspace()
	if s.cfg.GitAutoCommit && s.mode == prompt.ModeBuild && ws != "" && gitutil.IsRepo(ws) {
		if err := s.ensureCodientBranch(ws); err != nil {
			fmt.Fprintf(os.Stderr, "codient: git branch: %v\n", err)
		} else {
			paths := append(append([]string{}, entry.modifiedFiles...), entry.createdFiles...)
			if err := gitutil.Add(ws, paths); err != nil {
				fmt.Fprintf(os.Stderr, "codient: git add: %v\n", err)
			} else {
				subj, body := buildCodientCommitMessage(s.turn, userLine)
				if err := gitutil.Commit(ws, subj, body); err != nil {
					fmt.Fprintf(os.Stderr, "codient: git commit: %v\n", err)
				} else {
					sha, err := gitutil.HeadSHA(ws)
					if err != nil {
						fmt.Fprintf(os.Stderr, "codient: git: %v\n", err)
					} else {
						entry.commitSHA = sha
						s.lastTurnGitCommit = true
					}
				}
			}
		}
	}
	s.undoStack = append(s.undoStack, *entry)
	s.maybeAutoCheckpointAfterBuildTurn(userLine)
}

func shortSHA(full string) string {
	full = strings.TrimSpace(full)
	if len(full) <= 12 {
		return full
	}
	return full[:12]
}

func (s *session) undoLast(ws string) error {
	if len(s.undoStack) == 0 {
		fmt.Fprintf(os.Stderr, "codient: nothing to undo — no build-mode turns in this session\n")
		return nil
	}

	entry := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]

	if s.cfg.GitAutoCommit && entry.commitSHA != "" {
		head, err := gitutil.HeadSHA(ws)
		if err != nil {
			s.undoStack = append(s.undoStack, entry)
			return err
		}
		if head != entry.commitSHA {
			fmt.Fprintf(os.Stderr, "codient: HEAD is not the expected commit — refusing undo (expected %s, got %s)\n", shortSHA(entry.commitSHA), shortSHA(head))
			s.undoStack = append(s.undoStack, entry)
			return nil
		}
		if err := gitutil.ResetHardParent(ws); err != nil {
			s.undoStack = append(s.undoStack, entry)
			return err
		}
	} else {
		nMod := len(entry.modifiedFiles)
		nNew := len(entry.createdFiles)
		for _, f := range entry.modifiedFiles {
			fmt.Fprintf(os.Stderr, "  restore: %s\n", f)
		}
		for _, f := range entry.createdFiles {
			fmt.Fprintf(os.Stderr, "  remove:  %s\n", f)
		}

		if nMod > 0 {
			if err := gitutil.RestoreFiles(ws, entry.modifiedFiles); err != nil {
				return err
			}
		}
		for _, f := range entry.createdFiles {
			_ = os.Remove(filepath.Join(ws, f))
		}
		fmt.Fprintf(os.Stderr, "codient: undid last turn (%d files restored, %d files removed)\n", nMod, nNew)
	}

	msgsTrimmed := len(s.history) - entry.historyLen
	s.history = s.history[:entry.historyLen]
	if s.turn > 0 {
		s.turn--
	}

	s.lastReply = ""
	for i := len(s.history) - 1; i >= 0; i-- {
		b, _ := json.Marshal(s.history[i])
		raw := string(b)
		if strings.Contains(raw, `"role":"assistant"`) {
			s.lastReply = raw
			break
		}
	}

	s.autoSave()
	if entry.commitSHA != "" && s.cfg.GitAutoCommit {
		fmt.Fprintf(os.Stderr, "codient: undid last turn (removed commit %s, %d messages trimmed)\n", shortSHA(entry.commitSHA), msgsTrimmed)
	} else {
		fmt.Fprintf(os.Stderr, "codient: undid last turn (%d messages trimmed)\n", msgsTrimmed)
	}
	return nil
}

func (s *session) undoAll(ws string) error {
	if s.cfg.GitAutoCommit && s.gitSessionStartCommit != "" && gitutil.IsRepo(ws) {
		head, _ := gitutil.HeadSHA(ws)
		if head == s.gitSessionStartCommit {
			untracked, _ := gitutil.UntrackedFiles(ws)
			if len(untracked) == 0 {
				fmt.Fprintf(os.Stderr, "codient: no changes to undo\n")
				return nil
			}
			fmt.Fprintf(os.Stderr, "codient: removing %d untracked file(s)\n", len(untracked))
			if err := gitutil.CleanUntracked(ws); err != nil {
				return err
			}
			s.undoStack = nil
			fmt.Fprintf(os.Stderr, "codient: removed untracked files\n")
			return nil
		}
		if err := gitutil.ResetHard(ws, s.gitSessionStartCommit); err != nil {
			return err
		}
		if err := gitutil.CleanUntracked(ws); err != nil {
			return err
		}
		s.undoStack = nil
		fmt.Fprintf(os.Stderr, "codient: reset workspace to session start (%s)\n", shortSHA(s.gitSessionStartCommit))
		return nil
	}

	before, _ := gitutil.DiffSummary(ws)
	untracked, _ := gitutil.UntrackedFiles(ws)
	if before == "" && len(untracked) == 0 {
		fmt.Fprintf(os.Stderr, "codient: no changes to undo\n")
		return nil
	}
	if before != "" {
		fmt.Fprintf(os.Stderr, "codient: reverting changes:\n%s\n", before)
	}
	if len(untracked) > 0 {
		fmt.Fprintf(os.Stderr, "codient: removing %d untracked file(s)\n", len(untracked))
	}

	if err := gitutil.RestoreAll(ws); err != nil {
		return err
	}
	if err := gitutil.CleanUntracked(ws); err != nil {
		return err
	}

	s.undoStack = nil
	fmt.Fprintf(os.Stderr, "codient: all changes reverted\n")
	return nil
}

func (s *session) showGitDiffIfBuild() {
	if s.mode != prompt.ModeBuild || !s.lastBuildTurnHadChanges {
		return
	}
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" || !gitutil.IsRepo(ws) {
		return
	}
	defer func() {
		s.lastBuildTurnHadChanges = false
		s.lastTurnGitCommit = false
	}()

	if s.lastTurnGitCommit && s.cfg.GitAutoCommit {
		stat, err := gitutil.DiffStatLastCommit(ws)
		if err != nil || strings.TrimSpace(stat) == "" {
			stat, _ = gitutil.DiffSummary(ws)
		}
		if stat != "" {
			fmt.Fprintf(os.Stderr, "\ncodient: last commit — files changed:\n%s\n", stat)
		}
		return
	}

	stat, _ := gitutil.DiffStatHead(ws)
	untracked, _ := gitutil.UntrackedFiles(ws)
	if strings.TrimSpace(stat) == "" && len(untracked) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\ncodient: working tree — files changed:\n")
	if strings.TrimSpace(stat) != "" {
		fmt.Fprintf(os.Stderr, "%s\n", stat)
	}
	if len(untracked) > 0 {
		const maxNewFiles = 20
		fmt.Fprintf(os.Stderr, "\nnew files:\n")
		for i, f := range untracked {
			if i >= maxNewFiles {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(untracked)-maxNewFiles)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
	}
}

func (s *session) handleDiff(args string) error {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return fmt.Errorf("no workspace set")
	}
	if !gitutil.IsRepo(ws) {
		return fmt.Errorf("workspace is not a git repository")
	}
	path := strings.TrimSpace(args)
	var out string
	var err error
	if path == "" {
		out, err = gitutil.DiffUnifiedWorkspace(ws, 0)
	} else {
		out, err = gitutil.DiffUnifiedFile(ws, path, 0)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		fmt.Fprintf(os.Stderr, "codient: no diff\n")
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s\n", out)
	return nil
}

func (s *session) handleBranch(args string) error {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return fmt.Errorf("no workspace set")
	}
	if !gitutil.IsRepo(ws) {
		return fmt.Errorf("workspace is not a git repository")
	}
	name := strings.TrimSpace(args)
	if name == "" {
		cur, err := gitutil.CurrentBranch(ws)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "codient: current branch: %s\n", cur)
		return nil
	}
	exists, err := gitutil.BranchExists(ws, name)
	if err != nil {
		return err
	}
	if exists {
		return gitutil.CheckoutBranch(ws, name)
	}
	return gitutil.CreateBranch(ws, name)
}

func (s *session) handlePR(args string) error {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return fmt.Errorf("no workspace set")
	}
	if !gitutil.IsRepo(ws) {
		return fmt.Errorf("workspace is not a git repository")
	}
	if !gitutil.GhAvailable() {
		return fmt.Errorf("gh CLI not found on PATH — install GitHub CLI to open pull requests")
	}
	if err := gitutil.PushUpstream(ws); err != nil {
		return err
	}
	head, err := gitutil.CurrentBranch(ws)
	if err != nil {
		return err
	}
	base := s.resolvePRBase(ws)
	title := s.prTitle()
	body := s.prBody(ws)
	draft := strings.EqualFold(strings.TrimSpace(args), "draft")
	ghArgs := []string{
		"pr", "create",
		"--base", base,
		"--head", head,
		"--title", title,
		"--body", body,
	}
	if draft {
		ghArgs = append(ghArgs, "--draft")
	}
	url, err := gitutil.GhPRCreate(ws, ghArgs)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s\n", url)
	return nil
}

func (s *session) prTitle() string {
	if slug := strings.TrimSpace(s.taskSlug); slug != "" {
		return slug
	}
	return "codient session"
}

func (s *session) prBody(ws string) string {
	base := strings.TrimSpace(s.gitSessionStartCommit)
	if base == "" {
		return ""
	}
	lines, err := gitutil.LogOneLine(ws, base+"..HEAD", 30)
	if err != nil || len(lines) == 0 {
		return "Pull request opened by codient."
	}
	return strings.Join(lines, "\n")
}

func (s *session) gitPullRequestContextFn() tools.GitPullRequestContextFn {
	return func() (tools.GitPullRequestContext, error) {
		ws := s.cfg.EffectiveWorkspace()
		if ws == "" {
			return tools.GitPullRequestContext{}, fmt.Errorf("no workspace set")
		}
		if !gitutil.IsRepo(ws) {
			return tools.GitPullRequestContext{}, fmt.Errorf("workspace is not a git repository")
		}
		return tools.GitPullRequestContext{
			Workspace:  ws,
			BaseBranch: s.resolvePRBase(ws),
		}, nil
	}
}
