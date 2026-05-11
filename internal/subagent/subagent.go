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
	"codient/internal/sandbox"
	"codient/internal/tokentracker"
	"codient/internal/tools"
)

// tierForMode maps a sub-agent mode onto the reasoning tier used to resolve
// its inference connection. Plan mode (architectural design) requires the
// high-reasoning model; build / ask / unknown modes use the low-reasoning
// model so simple delegations stay cheap.
func tierForMode(m prompt.Mode) string {
	if m == prompt.ModePlan {
		return config.TierHigh
	}
	return config.TierLow
}

// Result is returned after a sub-agent completes.
type Result struct {
	Reply string
	Mode  string
	Model string
}

// RunParams configures a sub-agent invocation.
type RunParams struct {
	Cfg               *config.Config
	Mode              prompt.Mode
	Task              string
	Context           string       // optional extra context prepended to the task
	RepoMap           *repomap.Map // optional shared structural map (parent session); nil disables repo_map tool in child
	Log               *agentlog.Logger
	Progress          io.Writer // nested progress lines written here (already prefixed by caller); nil when using OnTranscriptEvent only
	OnTranscriptEvent func(agent.TranscriptEvent)
	Tracker           *tokentracker.Tracker
	// AutoCheck runs after successful mutating tools in this sub-agent (build mode only; nil skips).
	AutoCheck func(context.Context) agent.AutoCheckOutcome
	// SandboxRunnerOverride, when non-nil, replaces the default sandbox.Runner
	// built from cfg.SandboxMode for run_command in this sub-agent. Used by
	// delegate_task to inject a per-delegate container session or profile runner.
	SandboxRunnerOverride sandbox.Runner
	// AutoCheckMaxFixes caps fix-loop retries in this sub-agent (0 = single-shot).
	AutoCheckMaxFixes int
	// AutoCheckStopOnNoProgress aborts the fix loop when the failure signature
	// is unchanged between consecutive attempts.
	AutoCheckStopOnNoProgress bool
}

// newRunnerFromParams builds the agent.Runner used by Run.
func newRunnerFromParams(llm agent.ChatClient, p RunParams, reg *tools.Registry, log *agentlog.Logger) *agent.Runner {
	r := &agent.Runner{
		LLM:               llm,
		Cfg:               p.Cfg,
		Tools:             reg,
		Log:               log,
		Progress:          p.Progress,
		OnTranscriptEvent: p.OnTranscriptEvent,
		ProgressPlain:     p.Cfg.Plain,
		ProgressMode:      string(p.Mode),
		Tracker:           p.Tracker,
	}
	if p.AutoCheck != nil {
		r.AutoCheck = p.AutoCheck
		r.AutoCheckMaxFixes = p.AutoCheckMaxFixes
		r.AutoCheckStopOnNoProgress = p.AutoCheckStopOnNoProgress
	}
	return r
}

// Run executes a single sub-agent turn: builds a mode-specific Runner with the
// reasoning-tier client matching the requested mode (plan -> high, otherwise
// low), runs the task to completion, and returns the reply.
func Run(ctx context.Context, p RunParams) (Result, error) {
	client := openaiclient.NewForTier(p.Cfg, tierForMode(p.Mode))

	reg := agentfactory.RegistryForMode(p.Cfg, p.Mode, p.RepoMap, p.SandboxRunnerOverride)
	repoMapText := repomap.PromptText(p.Cfg.RepoMapTokens, p.RepoMap)
	sys := agentfactory.SystemPromptForMode(p.Cfg, reg, p.Mode, repoMapText, io.Discard)

	log := p.Log.WithSubAgent(string(p.Mode), client.Model())

	runner := newRunnerFromParams(client, p, reg, log)

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
