// Package subagent runs a child agent.Runner for a single delegated task.
// Sub-agents are isolated: they get a fresh conversation, their own tool registry
// (without delegate_task to prevent recursion), and optionally a different model
// via per-mode config.
package subagent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go/v3"

	"codient/internal/agent"
	"codient/internal/agentfactory"
	"codient/internal/agentlog"
	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/prompt"
	"codient/internal/repomap"
	"codient/internal/tokentracker"
)

// Result is returned after a sub-agent completes.
type Result struct {
	Reply string
	Mode  string
	Model string
}

// RunParams configures a sub-agent invocation.
type RunParams struct {
	Cfg      *config.Config
	Mode     prompt.Mode
	Task     string
	Context  string // optional extra context prepended to the task
	RepoMap  *repomap.Map // optional shared structural map (parent session); nil disables repo_map tool in child
	Log      *agentlog.Logger
	Progress io.Writer // nested progress lines written here (already prefixed by caller)
	Tracker  *tokentracker.Tracker
}

// Run executes a single sub-agent turn: builds a mode-specific Runner with per-mode
// model resolution, runs the task to completion, and returns the reply.
func Run(ctx context.Context, p RunParams) (Result, error) {
	client := openaiclient.NewForMode(p.Cfg, string(p.Mode))

	reg := agentfactory.RegistryForMode(p.Cfg, p.Mode, p.RepoMap)
	repoMapText := repomap.PromptText(p.Cfg.RepoMapTokens, p.RepoMap)
	sys := agentfactory.SystemPromptForMode(p.Cfg, reg, p.Mode, repoMapText, io.Discard)

	log := p.Log.WithSubAgent(string(p.Mode), client.Model())

	runner := &agent.Runner{
		LLM:           client,
		Cfg:           p.Cfg,
		Tools:         reg,
		Log:           log,
		Progress:      p.Progress,
		ProgressPlain: p.Cfg.Plain,
		ProgressMode:  string(p.Mode),
		Tracker:       p.Tracker,
	}

	userMsg := strings.TrimSpace(p.Task)
	if c := strings.TrimSpace(p.Context); c != "" {
		userMsg = fmt.Sprintf("[Context from parent agent]\n%s\n\n[Task]\n%s", c, userMsg)
	}

	reply, _, _, err := runner.RunConversation(ctx, sys, nil, openai.UserMessage(userMsg), nil)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Reply: reply,
		Mode:  string(p.Mode),
		Model: client.Model(),
	}, nil
}
