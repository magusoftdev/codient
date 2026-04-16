package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codient/internal/gitutil"

	"github.com/openai/openai-go/v3/shared"
)

// GitPullRequestContext supplies workspace and default PR base branch for create_pull_request.
type GitPullRequestContext struct {
	Workspace  string
	BaseBranch string
}

// GitPullRequestContextFn returns context for opening a PR; empty BaseBranch means use origin default.
type GitPullRequestContextFn func() (GitPullRequestContext, error)

// RegisterCreatePullRequest adds a tool that opens a GitHub PR via gh pr create.
// ctxFn is called at tool execution time (interactive session only).
func RegisterCreatePullRequest(r *Registry, ctxFn GitPullRequestContextFn) {
	if ctxFn == nil {
		return
	}
	r.Register(Tool{
		Name: "create_pull_request",
		Description: "Opens a pull request on GitHub for the current branch using the gh CLI. " +
			"Pushes the branch to origin if needed. Requires gh on PATH and a configured remote.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "PR title (required).",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "PR description in Markdown (required).",
				},
				"base": map[string]any{
					"type":        "string",
					"description": "Target branch name (optional; defaults to the session merge base or origin's default branch).",
				},
				"draft": map[string]any{
					"type":        "boolean",
					"description": "If true, open as a draft PR.",
				},
			},
			"required":             []string{"title", "body"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Title string `json:"title"`
				Body  string `json:"body"`
				Base  string `json:"base"`
				Draft bool   `json:"draft"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			p.Title = strings.TrimSpace(p.Title)
			p.Body = strings.TrimSpace(p.Body)
			if p.Title == "" {
				return "", fmt.Errorf("title is required")
			}
			gctx, err := ctxFn()
			if err != nil {
				return "", err
			}
			ws := strings.TrimSpace(gctx.Workspace)
			if ws == "" {
				return "", fmt.Errorf("no workspace")
			}
			if !gitutil.IsRepo(ws) {
				return "", fmt.Errorf("workspace is not a git repository")
			}
			if !gitutil.GhAvailable() {
				return "", fmt.Errorf("gh CLI not found on PATH — install GitHub CLI to create pull requests")
			}
			base := strings.TrimSpace(p.Base)
			if base == "" {
				base = strings.TrimSpace(gctx.BaseBranch)
			}
			if base == "" {
				base = gitutil.DefaultRemoteBranch(ws)
			}
			if err := gitutil.PushUpstream(ws); err != nil {
				return "", err
			}
			head, err := gitutil.CurrentBranch(ws)
			if err != nil {
				return "", err
			}
			ghArgs := []string{
				"pr", "create",
				"--base", base,
				"--head", head,
				"--title", p.Title,
				"--body", p.Body,
			}
			if p.Draft {
				ghArgs = append(ghArgs, "--draft")
			}
			url, err := gitutil.GhPRCreate(ws, ghArgs)
			if err != nil {
				return "", err
			}
			return "opened pull request: " + url, nil
		},
	})
}
