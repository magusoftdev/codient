package codientcli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/agent"
	"codient/internal/agentlog"
	"codient/internal/assistout"
	"codient/internal/astgrep"
	"codient/internal/clipboard"
	"codient/internal/codeindex"
	"codient/internal/config"
	"codient/internal/designstore"
	"codient/internal/fileref"
	"codient/internal/gitutil"
	"codient/internal/hooks"
	"codient/internal/imageutil"
	"codient/internal/mcpclient"
	"codient/internal/openaiclient"
	"codient/internal/planstore"
	"codient/internal/projectinfo"
	"codient/internal/prompt"
	"codient/internal/repomap"
	"codient/internal/selfupdate"
	"codient/internal/sessionstore"
	"codient/internal/skills"
	"codient/internal/slashcmd"
	"codient/internal/subagent"
	"codient/internal/tokentracker"
	"codient/internal/tools"
)

type session struct {
	cfg              *config.Config
	client           *openaiclient.Client
	registry         *tools.Registry
	agentLog         *agentlog.Logger
	progressOut      io.Writer
	mode             prompt.Mode
	systemPrompt     string
	richOutput       bool
	streamReply      bool
	designSaveDir    string
	goal             string
	taskFile         string
	userSystem       string
	repoInstructions string
	projectContext   string
	memory           string               // cross-session memory (global + workspace), loaded at startup
	skillsCatalog    string               // "## Agent skills" markdown; refreshed on /new and /create-skill
	memOpts          *tools.MemoryOptions // passed to build-mode registry for memory_update tool

	// REPL state
	history   []openai.ChatCompletionMessageParamUnion
	sessionID string
	turn      int
	lastReply string
	taskSlug  string

	undoStack []undoEntry // per-turn undo records (most recent at end)

	// Git workflow (build mode + git repo)
	gitSessionStartCommit   string // HEAD at session capture; undo-all resets here when git_auto_commit is on
	gitSessionStartBranch   string
	gitMergeTargetBranch    string // protected branch left when auto-creating codient/* (PR merge base)
	gitCodientCreatedBranch string // branch codient created from a protected branch, if any
	gitBranchEnsured        bool   // lazy auto-branch has run once this session
	lastBuildTurnHadChanges bool   // last pushUndoIfChanged saw file changes
	lastTurnGitCommit       bool   // last turn successfully created a codient auto-commit

	// stdinScanner is set for the interactive REPL; used for exec allow prompts.
	scanner   *bufio.Scanner
	execAllow *tools.SessionExecAllow // mutable run_command allowlist for this process; nil if exec disabled
	// execDeniedACP is used when stdin is not a TTY (ACP stdio): editor-driven permission via JSON-RPC.
	execDeniedACP func(context.Context, string, []string) tools.ExecPromptChoice
	// acpNoDelegate skips delegate_task and create_pull_request when true (ACP stub session).
	acpNoDelegate bool
	// acpCallClient issues JSON-RPC requests to the ACP client (e.g. Codient Unity). Nil outside -acp.
	acpCallClient func(context.Context, string, any) (json.RawMessage, error)

	fetchAllow    *tools.SessionFetchAllow // mutable fetch_url host approvals for this process; nil until first fetch
	fetchPromptMu sync.Mutex               // serializes fetch allow prompts and post-lock re-checks

	// replPromptMu serializes the REPL prompt (no trailing newline) and async stderr lines
	// (e.g. semantic index completion) so messages do not append to the same line as the prompt.
	replPromptMu sync.Mutex
	// promptMu guards s.systemPrompt and coordinated updates to s.registry so background
	// repo-map completion cannot race with executeTurn or context estimation.
	promptMu sync.RWMutex
	// replSkipFirstLoopPrompt is set when replAsyncStderrNote redraws the prompt before the first
	// readUserInput; the first loop iteration skips a duplicate "\n" + prompt pair.
	replSkipFirstLoopPrompt bool
	// replInputActive is true while the REPL is blocking on readUserInput. When set,
	// replAsyncStderrNote defers messages to pendingAsyncNotes instead of printing
	// immediately, avoiding visual corruption of the user's input line.
	replInputActive   bool
	pendingAsyncNotes []string

	// turnCancel cancels the context for the currently executing agent turn.
	// Non-nil only while executeTurn is in flight; reset to nil when the turn
	// completes. Protected by turnCancelMu.
	turnCancel   context.CancelFunc
	turnCancelMu sync.Mutex

	codeIndex *codeindex.Index // semantic search index; nil when embedding_model is not configured
	repoMap   *repomap.Map     // structural repo map; nil when repo_map_tokens is -1

	mcpMgr *mcpclient.Manager // MCP server connections; nil when no mcp_servers configured

	// acpRegistryMu guards stub.registry / stub.systemPrompt while async MCP connect finishes (-acp).
	acpRegistryMu sync.RWMutex

	// Plan lifecycle state (non-nil when a structured plan is active).
	currentPlan *planstore.Plan
	planPhase   planstore.Phase

	// Checkpoint tree: last created/restored snapshot id and logical branch label ("main", fork slugs).
	currentCheckpointID string
	convBranch          string

	// tokenTracker accumulates API-reported token usage for the REPL session.
	tokenTracker *tokentracker.Tracker

	// Headless (-print): single-turn automation; outputFormat is text|json|stream-json.
	printMode    bool
	outputFormat string
	autoApprove  AutoApprovePolicy
	maxTurns     int
	maxCostUSD   float64

	// singleTurnForceNew skips loading .codient/sessions when true (same as -new-session for REPL).
	singleTurnForceNew bool
	// singleTurnExplicitSessionID selects a session file by id; empty means load latest when not forcing new.
	singleTurnExplicitSessionID string

	// Multimodal: images from -image (first turn) or /image (next message).
	initialImages []imageutil.ImageAttachment
	pendingImages []imageutil.ImageAttachment

	// hooksMgr is loaded when hooks_enabled is true; nil otherwise.
	hooksMgr *hooks.Manager

	// tui is non-nil when the Bubble Tea split-screen TUI is active.
	// All stdout/stderr writes go through pipes into the TUI viewport,
	// and user input arrives through tui.inputCh instead of os.Stdin.
	tui *tuiSetup

	// chromeSink, when non-nil, replaces the default *tea.Program.Send for
	// chrome updates. Production code leaves this nil so messages flow into
	// the live TUI; tests inject a sink to observe footer refreshes without
	// running a Bubble Tea program.
	chromeSink func(tuiChromeMsg)

	// todos are session-scoped rows updated via todo_write (see sessionstore persistence).
	todos   []tools.TodoItem
	todosMu sync.Mutex

	// orchestratorForce auto-approves plan->build transitions for COMPLEX_TASK
	// turns when -force/-yes is set on the CLI.
	orchestratorForce bool

	// lastTurnMode is the resolved internal mode the most recent turn ran in
	// (build / ask / plan). The orchestrator restores s.mode = ModeAuto after
	// each turn so the next prompt is re-classified, but post-turn helpers
	// (design saving, plan parsing, "Ready to implement" detection) still
	// need to know which path the turn took. Set inside orchestratedTurn /
	// transitionToInternalMode; defaults to ModeBuild.
	lastTurnMode prompt.Mode
}

type undoEntry struct {
	modifiedFiles []string // tracked files modified during this turn (restore via git checkout)
	createdFiles  []string // untracked files created during this turn (delete)
	historyLen    int      // len(s.history) before this turn started
	commitSHA     string   // non-empty when git_auto_commit recorded this turn as a commit
}

// setMode updates the session mode and notifies the TUI if active.
// All mode assignments should go through this method to keep the TUI in sync.
func (s *session) setMode(m prompt.Mode) {
	s.mode = m
	s.sendTUIChrome()
}

// applyPostSetupReload re-derives runtime state after the interactive setup
// wizard (`/setup`) mutates s.cfg. It rebuilds the OpenAI client, reloads the
// skills catalog and tool registry against the (potentially new) workspace
// model, re-probes the context window, and pushes a fresh chrome message so
// the TUI footer below the input box reflects the updated model name and
// backend label. Without the final sendTUIChrome the footer would stay
// stale until something else (a turn completion, mode change, etc.) happened
// to push a new chrome update.
func (s *session) applyPostSetupReload(ctx context.Context) {
	s.client = openaiclient.New(s.cfg)
	s.loadSkillsCatalog()
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	s.cfg.ContextWindowTokens = 0
	s.probeAndSetContext(ctx)
	s.sendTUIChrome()
}

// sendTUIChrome pushes model, backend, and context usage into the TUI footer.
func (s *session) sendTUIChrome() {
	if s.chromeSink == nil && (s.tui == nil || s.tui.prog == nil) {
		return
	}
	base, _, model := s.cfg.ConnectionForMode(string(s.mode))
	tok, est := s.promptTokensForTUIChrome()
	msg := tuiChromeMsg{
		Mode:             string(s.mode),
		Model:            model,
		BackendLabel:     formatTUIBackendLabel(base),
		ContextWindow:    s.cfg.ContextWindowTokens,
		LastPromptTokens: tok,
		ContextEstimated: est,
	}
	if s.chromeSink != nil {
		s.chromeSink(msg)
		return
	}
	s.tui.prog.Send(msg)
}

// promptTokensForTUIChrome returns tokens for the context footer: API usage from the last
// completion when present, otherwise a heuristic estimate of the next request (system +
// tools + history) so local servers that omit usage still show context pressure.
func (s *session) promptTokensForTUIChrome() (tok int64, estimated bool) {
	if s.tokenTracker != nil {
		if t := s.tokenTracker.Last().PromptTokens; t > 0 {
			return t, false
		}
	}
	if s.registry == nil {
		return 0, true
	}
	return int64(s.estimateFullContextUsage()), true
}

func (s *session) loadSkillsCatalog() {
	s.skillsCatalog = ""
	sd, err := config.StateDir()
	if err != nil || sd == "" {
		return
	}
	cat, err := skills.LoadCatalogMarkdown(sd, s.cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: skills: %v\n", err)
		return
	}
	s.skillsCatalog = cat
}

func (s *session) setRegistryAndPrompt(reg *tools.Registry, mode prompt.Mode, rm *repomap.Map) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	s.registry = reg
	s.systemPrompt = buildAgentSystemPrompt(s.cfg, reg, mode, s.userSystem, s.repoInstructions, s.projectContext, s.memory, s.skillsCatalog, rm)
}

// installRegistry assigns a newly built registry and rebuilds the system prompt using s.mode and s.repoMap.
func (s *session) installRegistry(reg *tools.Registry) {
	s.setRegistryAndPrompt(reg, s.mode, s.repoMap)
}

func (s *session) rebuildSystemPromptWithRepoMap(rm *repomap.Map) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	s.systemPrompt = buildAgentSystemPrompt(s.cfg, s.registry, s.mode, s.userSystem, s.repoInstructions, s.projectContext, s.memory, s.skillsCatalog, rm)
}

func (s *session) rebuildSystemPrompt() {
	s.rebuildSystemPromptWithRepoMap(s.repoMap)
}

func (s *session) refreshSkillsCatalog() {
	s.loadSkillsCatalog()
	s.rebuildSystemPromptWithRepoMap(s.repoMap)
}

func (s *session) newRunner() *agent.Runner {
	s.client = openaiclient.NewForTier(s.cfg, tierForResolvedMode(s.mode))
	r := &agent.Runner{
		LLM: s.client, Cfg: s.cfg, Tools: s.registry,
		Log: s.agentLog, Progress: s.progressOut,
		ProgressPlain: s.cfg.Plain,
		ProgressMode:  string(s.mode),
		Tracker:       s.tokenTracker,
		Hooks:         s.hooksMgr,
	}
	if s.tui != nil {
		r.OnTranscriptEvent = func(ev agent.TranscriptEvent) {
			s.tui.prog.Send(tuiTranscriptMsg{ev: ev})
		}
		r.Progress = nil
	}
	if s.printMode {
		if s.maxTurns > 0 {
			r.MaxTurns = s.maxTurns
		}
		if s.maxCostUSD > 0 {
			r.MaxCostUSD = s.maxCostUSD
			r.EstimateSessionCost = func(u tokentracker.Usage) (float64, bool) {
				return s.estimateCostForUsage(u)
			}
		}
	}
	if s.mode == prompt.ModeBuild {
		steps := buildAutoCheckSteps(s.cfg)
		if len(steps) > 0 {
			sec := autoCheckTimeoutSec(s.cfg)
			r.AutoCheck = makeAutoCheckSequence(s.cfg.EffectiveWorkspace(), steps, time.Duration(sec)*time.Second, s.cfg.ExecMaxOutputBytes, s.progressOut)
		}
	}
	return r
}

// delegateTaskFn returns the callback used by the delegate_task tool to run sub-agents.
func (s *session) delegateTaskFn() tools.DelegateRunner {
	return func(ctx context.Context, modeStr, task, extraContext string) (string, error) {
		mode, err := prompt.ParseMode(modeStr)
		if err != nil {
			return "", err
		}
		var progress io.Writer
		var onTranscript func(agent.TranscriptEvent)
		if s.tui != nil {
			onTranscript = func(ev agent.TranscriptEvent) {
				s.tui.prog.Send(tuiTranscriptMsg{ev: ev, delegate: true})
			}
		} else {
			progress = s.progressOut
			if progress != nil {
				progress = newPrefixWriter([]byte("  │ "), progress)
			}
		}
		cfg := s.cfg
		repoMap := s.repoMap
		mainWS := s.cfg.EffectiveWorkspace()
		if s.cfg.DelegateGitWorktrees && mainWS != "" && gitutil.IsRepo(mainWS) {
			stateDir, serr := config.StateDir()
			if serr != nil {
				return "", fmt.Errorf("delegate_git_worktrees: state dir: %w", serr)
			}
			base := filepath.Join(stateDir, "delegate-worktrees")
			if err := os.MkdirAll(base, 0o755); err != nil {
				return "", fmt.Errorf("delegate_git_worktrees: mkdir: %w", err)
			}
			var idBuf [16]byte
			if _, err := rand.Read(idBuf[:]); err != nil {
				return "", fmt.Errorf("delegate_git_worktrees: %w", err)
			}
			wtPath := filepath.Join(base, hex.EncodeToString(idBuf[:]))
			if err := gitutil.AddDelegateWorktree(ctx, mainWS, wtPath); err != nil {
				return "", err
			}
			defer func() {
				rmCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				if rmErr := gitutil.RemoveDelegateWorktree(rmCtx, mainWS, wtPath); rmErr != nil {
					fmt.Fprintf(os.Stderr, "codient: delegate worktree cleanup: %v\n", rmErr)
				}
			}()
			c2 := *s.cfg
			c2.Workspace = wtPath
			cfg = &c2
			repoMap = nil
		}
		rp := subagent.RunParams{
			Cfg:               cfg,
			Mode:              mode,
			Task:              task,
			Context:           extraContext,
			RepoMap:           repoMap,
			Log:               s.agentLog,
			Progress:          progress,
			OnTranscriptEvent: onTranscript,
			Tracker:           s.tokenTracker,
		}
		if mode == prompt.ModeBuild {
			steps := buildAutoCheckSteps(cfg)
			if len(steps) > 0 {
				sec := autoCheckTimeoutSec(cfg)
				rp.AutoCheck = makeAutoCheckSequence(cfg.EffectiveWorkspace(), steps, time.Duration(sec)*time.Second, cfg.ExecMaxOutputBytes, progress)
			}
		}
		res, err := subagent.Run(ctx, rp)
		if err != nil {
			return "", err
		}
		return res.Reply, nil
	}
}

// interruptTurn cancels the currently executing agent turn, if any.
// Returns true if a turn was in progress and was cancelled.
func (s *session) interruptTurn() bool {
	s.turnCancelMu.Lock()
	defer s.turnCancelMu.Unlock()
	if s.turnCancel != nil {
		s.turnCancel()
		return true
	}
	return false
}

// isRunning reports whether an agent turn is currently in progress.
func (s *session) isRunning() bool {
	s.turnCancelMu.Lock()
	defer s.turnCancelMu.Unlock()
	return s.turnCancel != nil
}

// runTurn dispatches every user message through the Intent-Driven Orchestrator.
// userText is the raw prompt text used for supervisor classification.
//
// runner is unused today (the orchestrator builds its own runner per turn after
// resolving the mode) but is kept in the signature to mirror executeTurn so
// future plumbing (e.g. shared progress sinks) can be threaded uniformly.
func (s *session) runTurn(ctx context.Context, _ *agent.Runner, user openai.ChatCompletionMessageParamUnion, userText string) (string, error) {
	return s.orchestratedTurn(ctx, user, userText)
}

func (s *session) executeTurn(ctx context.Context, runner *agent.Runner, user openai.ChatCompletionMessageParamUnion) (reply string, err error) {
	if err := s.cfg.RequireModel(); err != nil {
		return "", err
	}

	turnCtx, turnCancel := context.WithCancel(ctx)
	defer func() {
		turnCancel()
		s.turnCancelMu.Lock()
		s.turnCancel = nil
		s.turnCancelMu.Unlock()
	}()
	s.turnCancelMu.Lock()
	s.turnCancel = turnCancel
	s.turnCancelMu.Unlock()
	ctx = turnCtx

	if s.hooksMgr != nil {
		s.hooksMgr.NextTurn()
		up, herr := s.hooksMgr.RunUserPromptSubmit(ctx, agent.UserMessageText(user))
		if herr != nil {
			return "", herr
		}
		if up.Blocked {
			return "", fmt.Errorf("%s", up.Reason)
		}
	}
	if s.tokenTracker != nil {
		s.tokenTracker.MarkTurnStart()
	}
	if !s.printMode {
		fmt.Fprint(os.Stderr, "\n")
	}
	if !s.printMode || s.outputFormat == "text" {
		writePlanDraftPreamble(os.Stdout, s.mode, s.lastReply)
	}
	stdoutTTY := assistout.StdoutIsInteractive()
	if s.printMode && s.outputFormat != "text" {
		stdoutTTY = false
	}
	streamTo := streamWriterForTurn(s.streamReply, stdoutTTY, s.mode, s.richOutput, s.lastReply)

	var spinMu sync.Mutex
	var curStopSpin func()

	startSpin := func() {
		spinMu.Lock()
		defer spinMu.Unlock()
		if curStopSpin != nil {
			curStopSpin()
		}
		if s.tui != nil {
			s.tui.prog.Send(tuiWorkingMsg(true))
		}
		curStopSpin = startWorkingSpinner(os.Stderr)
	}
	stopSpinFn := func() {
		spinMu.Lock()
		defer spinMu.Unlock()
		if curStopSpin != nil {
			curStopSpin()
			curStopSpin = nil
		}
		if s.tui != nil {
			s.tui.prog.Send(tuiWorkingMsg(false))
		}
	}

	startSpin()
	defer stopSpinFn()

	runner.OnWorkingChange = func(working bool) {
		if working {
			startSpin()
		} else {
			stopSpinFn()
		}
	}
	defer func() { runner.OnWorkingChange = nil }()

	if s.mode == prompt.ModeAsk {
		runner.PostReplyCheck = makePostReplyCheck(s, runner.Progress)
	}
	if streamTo != nil {
		streamTo = &spinStopWriter{w: streamTo, stop: stopSpinFn}
	}

	var newHist []openai.ChatCompletionMessageParamUnion
	var streamed bool
	var runErr error

	s.promptMu.RLock()
	sysPrompt := s.systemPrompt
	s.promptMu.RUnlock()

	usePlanTot := s.mode == prompt.ModePlan && s.cfg.PlanTot &&
		agent.PlanTotHeuristicMet(s.turn, s.lastReply)
	if usePlanTot {
		totClient := agent.NewPlanTotOpenAIClient(s.cfg)
		var used bool
		reply, newHist, streamed, used, runErr = agent.RunPlanModeTot(ctx, runner, totClient, sysPrompt, s.history, user, streamTo)
		if runErr != nil {
			return "", runErr
		}
		if !used {
			reply, newHist, streamed, runErr = runner.RunConversation(ctx, sysPrompt, s.history, user, streamTo)
		}
	} else {
		reply, newHist, streamed, runErr = runner.RunConversation(ctx, sysPrompt, s.history, user, streamTo)
	}
	if runErr != nil {
		return "", runErr
	}
	s.history = newHist
	out := io.Writer(os.Stdout)
	if s.printMode && (s.outputFormat == "json" || s.outputFormat == "stream-json") {
		out = io.Discard
	}
	if err := finishAssistantTurn(out, reply, s.richOutput, s.mode == prompt.ModePlan, streamed); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	s.printTurnTokenSummary()
	s.sendTUIChrome()
	return reply, nil
}

// postReplyVerificationPrompt is injected in Ask mode when the assistant reply looks like
// an actionable multi-item suggestion list. It asks for a quick tool-grounded pass without
// imposing a rigid output template.
const postReplyVerificationPrompt = `Your last reply looked like a list of suggestions or concrete changes. Before we treat it as final, do one short verification pass against this workspace using grep, read_file, or list_dir as needed:

- Drop or revise anything already addressed in the repo (say briefly what you checked).
- For anything still relevant, note the strongest evidence (tool name + what you searched/read + what you found). Keep it concise.

Answer in normal prose. Do not use a fixed "## Verified Suggestions" section or numbered report template unless you truly need it for clarity.`

// postReplyGateSystem asks for a single YES/NO: does the assistant reply warrant
// the post-reply verification pass (concrete codebase change proposals)?
const postReplyGateSystem = `You classify whether a follow-up verification step is needed.

Reply with exactly YES or NO as the first word of your response (then you may add a short phrase if needed).

Answer YES only if the assistant's reply primarily proposes or argues concrete changes to the user's own project or repository (edits, refactors, new files, config changes, what they should implement).

Answer NO if the reply is mainly: summarizing external pages or search results; listing numbered links or citations; quoting documentation; answering factual questions; describing third-party software; or checklist/status formatting without prescribing repo edits.`

// BuildPostReplyCheckForACP returns Ask-mode post-reply verification for the ACP stdio server (same logic as the REPL).
func BuildPostReplyCheckForACP(cfg *config.Config, client *openaiclient.Client, tracker *tokentracker.Tracker, mode prompt.Mode, progress io.Writer) func(context.Context, agent.PostReplyCheckInfo) string {
	s := &session{cfg: cfg, client: client, tokenTracker: tracker, mode: mode}
	return makePostReplyCheck(s, progress)
}

// makePostReplyCheck returns a PostReplyCheck function for Ask mode.
// It uses a cheap LLM gate after list-shaped heuristics: only when the model
// says the reply warrants verification do we inject the verification prompt.
func makePostReplyCheck(s *session, progress io.Writer) func(context.Context, agent.PostReplyCheckInfo) string {
	return func(ctx context.Context, info agent.PostReplyCheckInfo) string {
		if !looksLikeSuggestionList(info.Reply) {
			return ""
		}
		if skipSuggestionVerifyForResearchTurn(info) {
			return ""
		}
		if progress != nil {
			if line := agent.FormatStatusProgressLine(s.cfg.Plain, string(s.mode), "checking whether verification is needed…"); line != "" {
				fmt.Fprintf(progress, "%s\n", line)
			}
		}
		want, err := postReplyGateWantsVerification(ctx, s.client, s.tokenTracker, info)
		if err != nil || !want {
			return ""
		}
		return postReplyVerificationPrompt
	}
}

func postReplyGateWantsVerification(ctx context.Context, client *openaiclient.Client, tr *tokentracker.Tracker, info agent.PostReplyCheckInfo) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("nil client")
	}
	user := buildPostReplyGateUserMessage(info)
	params := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(client.Model()),
		Messages:            []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(postReplyGateSystem), openai.UserMessage(user)},
		Temperature:         openai.Float(0),
		MaxCompletionTokens: openai.Int(24),
	}
	res, err := client.ChatCompletion(ctx, params)
	if err != nil {
		return false, err
	}
	if tr != nil {
		tr.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	if len(res.Choices) == 0 {
		return false, nil
	}
	content := ""
	if c := res.Choices[0].Message.Content; c != "" {
		content = c
	}
	return parsePostReplyGateAnswer(content), nil
}

const postReplyGateMaxReplyChars = 12000

func buildPostReplyGateUserMessage(info agent.PostReplyCheckInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "User message (this turn):\n%s\n\n", strings.TrimSpace(info.User))
	if len(info.TurnTools) > 0 {
		fmt.Fprintf(&b, "Tools used this turn: %s\n\n", strings.Join(info.TurnTools, ", "))
	} else {
		fmt.Fprintf(&b, "Tools used this turn: (none)\n\n")
	}
	reply := strings.TrimSpace(info.Reply)
	if len(reply) > postReplyGateMaxReplyChars {
		reply = reply[:postReplyGateMaxReplyChars] + "\n…[truncated]"
	}
	fmt.Fprintf(&b, "Assistant reply:\n%s\n", reply)
	return b.String()
}

// parsePostReplyGateAnswer returns true when the gate model affirms verification.
// Only the first word of the first line is considered (strict YES/NO).
func parsePostReplyGateAnswer(content string) bool {
	line := strings.TrimSpace(strings.Split(content, "\n")[0])
	line = strings.Trim(line, "\"'`*_")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToUpper(strings.TrimRight(fields[0], ".!,:;"))
	if strings.HasPrefix(first, "YES") {
		return true
	}
	if strings.HasPrefix(first, "NO") {
		return false
	}
	return false
}

// skipSuggestionVerifyForResearchTurn skips the DISPROVE-suggestions pass when
// the turn was web research (web_search, no file mutations) and the user did
// not ask for codebase change suggestions — list-shaped answers are usually
// citations or summaries, not actionable repo proposals.
func skipSuggestionVerifyForResearchTurn(info agent.PostReplyCheckInfo) bool {
	if !slices.Contains(info.TurnTools, "web_search") {
		return false
	}
	for _, n := range info.TurnTools {
		if agent.ToolIsMutating(n) {
			return false
		}
	}
	return !userIntentSuggestsCodeChanges(info.User)
}

func userIntentSuggestsCodeChanges(u string) bool {
	u = strings.ToLower(u)
	phrases := []string{
		"suggest", "recommend", "refactor", "codebase", "our repo", "this repo",
		"our code", "this code", "this project", "should we ", "code review",
		"review our", "review the code", "improve our", "improve the code",
		"apply to", "change we ", "in this codebase",
	}
	for _, p := range phrases {
		if strings.Contains(u, p) {
			return true
		}
	}
	return false
}

// looksLikeSuggestionList returns true when reply contains 3+ lines that look
// like an actionable suggestion list. It avoids false positives from typical
// web-search formatting: `- [title](url)` link rows and `## Section` headers
// without numbering.
func looksLikeSuggestionList(s string) bool {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if suggestionListLine(trimmed) {
			count++
		}
		if count >= 3 {
			return true
		}
	}
	return false
}

func suggestionListLine(trimmed string) bool {
	if isMarkdownLinkBullet(trimmed) {
		return false
	}
	// Checklist / status bullets (common in web-search summaries) are not "action items".
	if strings.ContainsAny(trimmed, "✅✔☑✓") {
		return false
	}
	if trimmed[0] == '-' || trimmed[0] == '*' {
		return true
	}
	if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
		return isNumberedMarkdownHeading(trimmed)
	}
	if len(trimmed) >= 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && (trimmed[1] == '.' || (len(trimmed) >= 3 && trimmed[1] >= '0' && trimmed[1] <= '9' && trimmed[2] == '.')) {
		return true
	}
	return false
}

func isMarkdownLinkBullet(line string) bool {
	if len(line) < 2 {
		return false
	}
	if line[0] != '-' && line[0] != '*' {
		return false
	}
	rest := strings.TrimSpace(line[1:])
	return strings.HasPrefix(rest, "[")
}

// isNumberedMarkdownHeading is true for "## 1. Title" / "### 2) Foo" but not
// "## Background" or "### See also".
func isNumberedMarkdownHeading(line string) bool {
	var rest string
	switch {
	case strings.HasPrefix(line, "### "):
		rest = strings.TrimPrefix(line, "### ")
	case strings.HasPrefix(line, "## "):
		rest = strings.TrimPrefix(line, "## ")
	default:
		return false
	}
	rest = strings.TrimSpace(rest)
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	if i < len(rest) && (rest[i] == '.' || rest[i] == ')') {
		return true
	}
	return false
}

func (s *session) warnIfNotGitRepo() {
	if s.mode != prompt.ModeBuild {
		return
	}
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return
	}
	if !gitutil.IsRepo(ws) {
		fmt.Fprintf(os.Stderr, "codient: workspace is not a git repository — changes cannot be undone via git\n")
	}
}

func (s *session) captureSnapshot() (modified, untracked []string) {
	ws := s.cfg.EffectiveWorkspace()
	if s.mode != prompt.ModeBuild || ws == "" || !gitutil.IsRepo(ws) {
		return nil, nil
	}
	modified, _ = gitutil.DiffFiles(ws)
	untracked, _ = gitutil.UntrackedFiles(ws)
	return modified, untracked
}

// userMessageForTurn builds the API user message from text, optional @image: paths,
// and images queued via -image (first turn only), /image, /paste, or Ctrl+V.
func (s *session) userMessageForTurn(text string) (openai.ChatCompletionMessageParamUnion, string, error) {
	var attach []imageutil.ImageAttachment
	attach = append(attach, s.pendingImages...)
	s.pendingImages = nil
	if s.tui != nil {
		attach = append(attach, s.tui.drainClipImages()...)
	}
	if s.turn == 0 {
		attach = append(s.initialImages, attach...)
		s.initialImages = nil
	}
	msg, err := buildUserMessage(s.cfg.EffectiveWorkspace(), text, attach)
	if err != nil {
		return openai.ChatCompletionMessageParamUnion{}, "", err
	}
	line := agent.UserMessageText(msg)
	if strings.TrimSpace(line) == "" && len(attach) > 0 {
		line = "[image]"
	}
	return msg, line, nil
}

// countUserMessagesInOpenAIHistory counts user-role messages in the API history slice.
func countUserMessagesInOpenAIHistory(msgs []openai.ChatCompletionMessageParamUnion) int {
	n := 0
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		if sessionstore.MessageRole(json.RawMessage(b)) == "user" {
			n++
		}
	}
	return n
}

// applyStoredSessionState restores REPL-persisted fields from disk (single-shot / -print resume).
func (s *session) applyStoredSessionState(existing *sessionstore.SessionState) error {
	if existing == nil {
		return nil
	}
	ws := s.cfg.EffectiveWorkspace()
	if ws != "" && strings.TrimSpace(existing.Workspace) != "" {
		if filepath.Clean(strings.TrimSpace(existing.Workspace)) != filepath.Clean(ws) {
			return fmt.Errorf("session workspace %q does not match %q", existing.Workspace, ws)
		}
	}
	msgs, err := sessionstore.ToOpenAI(existing.Messages)
	if err != nil {
		return err
	}
	s.history = msgs
	s.sessionID = existing.ID
	// existing.Mode is retained in the persisted JSON for backwards
	// compatibility but is no longer authoritative: every session runs in
	// ModeAuto and the orchestrator decides per turn.
	if existing.PlanPhase != "" {
		s.planPhase = planstore.Phase(existing.PlanPhase)
	}
	if existing.CurrentCheckpointID != "" {
		s.currentCheckpointID = existing.CurrentCheckpointID
	}
	if b := strings.TrimSpace(existing.CurrentBranch); b != "" {
		s.convBranch = b
	} else {
		s.convBranch = "main"
	}
	s.loadPlanFromDisk()
	s.loadSkillsCatalog()
	s.todos = sessionTodosToTools(existing.Todos)
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	s.syncTodosToTUI()
	return nil
}

func sessionTodosToTools(in []sessionstore.TodoItem) []tools.TodoItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]tools.TodoItem, len(in))
	for i := range in {
		out[i] = tools.TodoItem{Content: in[i].Content, Status: in[i].Status, Priority: in[i].Priority}
	}
	return out
}

func toolsTodosToSession(in []tools.TodoItem) []sessionstore.TodoItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]sessionstore.TodoItem, len(in))
	for i := range in {
		out[i] = sessionstore.TodoItem{Content: in[i].Content, Status: in[i].Status, Priority: in[i].Priority}
	}
	return out
}

func (s *session) applyTodoWrite(items []tools.TodoItem) error {
	s.todosMu.Lock()
	s.todos = append([]tools.TodoItem(nil), items...)
	s.todosMu.Unlock()
	s.syncTodosToTUI()
	s.autoSave()
	return nil
}

func (s *session) snapshotTodos() []tools.TodoItem {
	s.todosMu.Lock()
	defer s.todosMu.Unlock()
	return append([]tools.TodoItem(nil), s.todos...)
}

func (s *session) syncTodosToTUI() {
	if s.tui == nil {
		return
	}
	items := s.snapshotTodos()
	s.tui.prog.Send(tuiTodosMsg{items: items})
}

func (s *session) runSingleTurn(ctx context.Context, user string, extra []imageutil.ImageAttachment) int {
	if s.mcpMgr != nil {
		defer s.mcpMgr.Close()
	}
	wsEarly := s.cfg.EffectiveWorkspace()

	if strings.TrimSpace(s.singleTurnExplicitSessionID) != "" && s.singleTurnForceNew {
		fmt.Fprintf(os.Stderr, "codient: ignoring -session-id because -new-session is set\n")
	}

	if wsEarly != "" && !s.singleTurnForceNew {
		var existing *sessionstore.SessionState
		var loadErr error
		if id := strings.TrimSpace(s.singleTurnExplicitSessionID); id != "" {
			existing, loadErr = sessionstore.LoadByWorkspaceAndID(wsEarly, id)
		} else {
			var err error
			existing, err = sessionstore.LoadLatest(wsEarly)
			if err != nil {
				loadErr = err
			}
		}
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "codient: session resume: %v\n", loadErr)
			if strings.TrimSpace(s.singleTurnExplicitSessionID) != "" {
				return 2
			}
		}
		if existing != nil {
			if err := s.applyStoredSessionState(existing); err != nil {
				fmt.Fprintf(os.Stderr, "codient: session resume: %v\n", err)
				return 2
			}
		}
	}
	if strings.TrimSpace(s.sessionID) == "" {
		s.sessionID = sessionstore.NewID(wsEarly)
	}
	if s.convBranch == "" {
		s.convBranch = "main"
	}

	if hm, herr := hooks.LoadForConfig(s.cfg.HooksEnabled, wsEarly, s.cfg.Model, s.sessionID); herr != nil {
		fmt.Fprintf(os.Stderr, "codient: hooks: %v\n", herr)
	} else {
		s.hooksMgr = hm
	}
	defer func() {
		if s.hooksMgr != nil {
			s.hooksMgr.RunSessionEnd(context.Background())
		}
	}()
	s.warnIfNotGitRepo()
	if !s.printMode {
		s.probeAndSetContext(ctx)
		assistout.WriteWelcome(os.Stderr, assistout.WelcomeParams{
			Plain:               s.cfg.Plain,
			Quiet:               s.cfg.Quiet,
			Repl:                false,
			Workspace:           s.cfg.EffectiveWorkspace(),
			Model:               s.cfg.Model,
			Version:             Version,
			ContextWindowTokens: s.cfg.ContextWindowTokens,
			EmbeddingModel:      s.cfg.EmbeddingModel,
		})
	}
	if s.cfg.Verbose {
		fmt.Fprintf(os.Stderr, "codient: workspace=%q mode=%s tools=%s\n", s.cfg.EffectiveWorkspace(), s.mode, strings.Join(s.registry.Names(), ", "))
	}
	if s.hooksMgr != nil {
		add, herr := s.hooksMgr.RunSessionStart(ctx, hooks.SessionStartup)
		if herr != nil {
			fmt.Fprintf(os.Stderr, "codient: hooks SessionStart: %v\n", herr)
		} else if strings.TrimSpace(add) != "" {
			s.promptMu.Lock()
			s.systemPrompt += "\n\n# Hook context (SessionStart)\n" + add
			s.promptMu.Unlock()
		}
	}
	rawUser := user
	priorUserTurns := countUserMessagesInOpenAIHistory(s.history)
	user, err := applyTaskToFirstTurnIfNeeded(priorUserTurns, user, s.goal, s.taskFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task: %v\n", err)
		return 2
	}
	msg, err := buildUserMessage(s.cfg.EffectiveWorkspace(), user, extra)
	if err != nil {
		fmt.Fprintf(os.Stderr, "image: %v\n", err)
		return 2
	}
	runner := s.newRunner()
	reply, err := s.runTurn(ctx, runner, msg, user)
	s.autoSave()
	if s.printMode {
		if err == nil {
			maybeSaveDesign(os.Stderr, s.cfg.EffectiveWorkspace(), s.designSaveDir, s.sessionID, s.lastTurnMode, reply, designstore.TaskSlug(s.goal, s.taskFile, rawUser), s.cfg.DesignSave)
			s.showGitDiffIfBuild()
		}
		return s.finishHeadlessTurn(reply, err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		return 1
	}
	maybeSaveDesign(os.Stderr, s.cfg.EffectiveWorkspace(), s.designSaveDir, s.sessionID, s.lastTurnMode, reply, designstore.TaskSlug(s.goal, s.taskFile, rawUser), s.cfg.DesignSave)
	s.showGitDiffIfBuild()
	return 0
}

// maybePromptUpdate checks for a newer release and interactively asks the user
// whether to install it. Skipped versions are persisted so the user is not
// asked again until an even newer release appears.
func (s *session) maybePromptUpdate(sc *bufio.Scanner) {
	if s.cfg.Quiet || !s.cfg.UpdateNotify {
		return
	}
	stateDir, _ := config.StateDir()
	tag, err := selfupdate.LatestVersion()
	if err != nil || !selfupdate.IsNewer(Version, tag) {
		return
	}
	if skipped := selfupdate.LoadSkippedVersion(stateDir); skipped == tag {
		return
	}
	newVer := strings.TrimPrefix(tag, "v")
	fmt.Fprintf(os.Stderr, "codient: update available %s -> %s\n", Version, newVer)
	fmt.Fprintf(os.Stderr, "Install now? [Y/n] ")
	answer := ""
	if sc.Scan() {
		answer = strings.TrimSpace(sc.Text())
	}
	if answer == "" || strings.HasPrefix(strings.ToLower(answer), "y") {
		fmt.Fprintf(os.Stderr, "codient: downloading %s...\n", newVer)
		if err := selfupdate.Apply(tag); err != nil {
			fmt.Fprintf(os.Stderr, "codient: update failed: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "codient: updated to %s — restarting...\n", newVer)
		if err := selfupdate.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "codient: restart failed: %v — please restart codient manually\n", err)
			os.Exit(0)
		}
	}
	if err := selfupdate.SaveSkippedVersion(stateDir, tag); err == nil {
		fmt.Fprintf(os.Stderr, "codient: skipped %s (won't ask again for this version)\n", newVer)
	}
}

// runSession is the main persistent REPL loop with slash commands and session persistence.
func (s *session) runSession(ctx context.Context, initialPrompt string, newSession bool) int {
	ws := s.cfg.EffectiveWorkspace()

	var resumeSummary string
	// Load or create session. Persisted `Mode` is intentionally ignored — every
	// session runs the Intent-Driven Orchestrator (ModeAuto) and the supervisor
	// picks an internal mode per turn. The field is kept in the JSON for
	// backwards compatibility with older session files.
	if !newSession && ws != "" {
		if existing, err := sessionstore.LoadLatest(ws); err == nil && existing != nil {
			msgs, err := sessionstore.ToOpenAI(existing.Messages)
			if err == nil {
				s.history = msgs
				s.sessionID = existing.ID
				if existing.PlanPhase != "" {
					s.planPhase = planstore.Phase(existing.PlanPhase)
				}
				if existing.CurrentCheckpointID != "" {
					s.currentCheckpointID = existing.CurrentCheckpointID
				}
				if b := strings.TrimSpace(existing.CurrentBranch); b != "" {
					s.convBranch = b
				} else {
					s.convBranch = "main"
				}
				s.loadPlanFromDisk()
				resumeSummary = sessionstore.ResumeSummaryLine(s.sessionID, existing.Messages)
				if s.currentPlan != nil && s.planPhase != "" && s.planPhase != planstore.PhaseDone {
					resumeSummary += " · plan: " + string(s.planPhase)
				}
			}
		}
	}
	if s.sessionID == "" {
		s.sessionID = sessionstore.NewID(ws)
	}
	if s.convBranch == "" {
		s.convBranch = "main"
	}

	if hm, herr := hooks.LoadForConfig(s.cfg.HooksEnabled, ws, s.cfg.Model, s.sessionID); herr != nil {
		fmt.Fprintf(os.Stderr, "codient: hooks: %v\n", herr)
	} else {
		s.hooksMgr = hm
	}
	defer func() {
		if s.hooksMgr != nil {
			s.hooksMgr.RunSessionEnd(context.Background())
		}
	}()

	// Clean up stale clipboard temp images from previous sessions.
	go clipboard.Cleanup(clipboard.ClipboardDir(ws), 1*time.Hour)

	s.captureGitSessionState(ws)
	s.warnIfNotGitRepo()

	var sc *bufio.Scanner
	if s.tui != nil {
		sc = bufio.NewScanner(&chanReader{ch: s.tui.input.ch})
		s.tui.clipWorkspace = ws
	} else {
		sc = bufio.NewScanner(os.Stdin)
		enableBracketedPaste()
		defer disableBracketedPaste()
	}
	s.scanner = sc
	resolveAstGrep(s.cfg, sc)
	s.loadSkillsCatalog()
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))

	if strings.TrimSpace(s.cfg.Model) == "" {
		s.runSetupWizard(ctx, sc)
		s.client = openaiclient.New(s.cfg)
		s.loadSkillsCatalog()
		s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	}

	s.probeAndSetContext(ctx)

	if s.hooksMgr != nil {
		src := hooks.SessionStartup
		if strings.TrimSpace(resumeSummary) != "" {
			src = hooks.SessionResume
		}
		add, herr := s.hooksMgr.RunSessionStart(ctx, src)
		if herr != nil {
			fmt.Fprintf(os.Stderr, "codient: hooks SessionStart: %v\n", herr)
		} else if strings.TrimSpace(add) != "" {
			s.promptMu.Lock()
			s.systemPrompt += "\n\n# Hook context (SessionStart)\n" + add
			s.promptMu.Unlock()
		}
	}

	assistout.WriteWelcome(os.Stderr, assistout.WelcomeParams{
		Plain:               s.cfg.Plain,
		Quiet:               s.cfg.Quiet,
		Repl:                true,
		Workspace:           ws,
		Model:               s.cfg.Model,
		ResumeSummary:       resumeSummary,
		Version:             Version,
		ContextWindowTokens: s.cfg.ContextWindowTokens,
		EmbeddingModel:      s.cfg.EmbeddingModel,
	})
	if s.cfg.Verbose {
		fmt.Fprintf(os.Stderr, "codient: workspace=%q tools=%s\n", ws, strings.Join(s.registry.Names(), ", "))
	}

	if s.mcpMgr != nil {
		defer s.mcpMgr.Close()
	}

	// Print before startCodeIndex: the index goroutine may redraw the REPL prompt via
	// replAsyncStderrNote; any later stderr line without a leading newline would append
	// to that prompt line (e.g. "codient: type /help" glued to "[ask] > ").
	fmt.Fprintf(os.Stderr, "codient: type /help for commands, /exit to quit\n")

	s.startCodeIndex(ctx)
	s.startRepoMap(ctx)

	if s.currentPlan != nil && s.planPhase != "" && s.planPhase != planstore.PhaseDone {
		s.handlePlanResume(ctx, sc)
	}

	s.maybePromptUpdate(sc)

	// Register slash commands.
	cmds := s.buildSlashCommands(ctx, sc)

	// Send slash commands to TUI for autocomplete.
	if s.tui != nil && s.tui.prog != nil {
		s.tui.prog.Send(slashCmdsMsg(cmds))
		s.sendTUIChrome()
	}

	runner := s.newRunner()

	// Execute initial prompt if provided.
	if seed := strings.TrimSpace(initialPrompt); seed != "" {
		if s.taskSlug == "" {
			s.taskSlug = designstore.TaskSlug(s.goal, s.taskFile, seed)
		}
		user, err := applyTaskToFirstTurnIfNeeded(s.turn, seed, s.goal, s.taskFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task: %v\n", err)
			return 2
		}
		userMsg, commitLine, err := s.userMessageForTurn(user)
		if err != nil {
			fmt.Fprintf(os.Stderr, "image: %v\n", err)
			return 2
		}
		preModified, preUntracked := s.captureSnapshot()
		histLen := len(s.history)
		s.turn++
		reply, err := s.runTurn(ctx, runner, userMsg, user)
		if err != nil {
			if isInterruptErr(err) {
				fmt.Fprintf(os.Stderr, "\ncodient: interrupted\n")
			} else {
				fmt.Fprintf(os.Stderr, "agent: %v\n", err)
				return 1
			}
		}
		if err == nil {
			s.pushUndoIfChanged(preModified, preUntracked, histLen, commitLine)
			maybeSaveDesign(os.Stderr, ws, s.designSaveDir, s.sessionID, s.lastTurnMode, reply, s.taskSlug, s.cfg.DesignSave)
			s.lastReply = assistout.PrepareAssistantText(reply, s.lastTurnMode == prompt.ModePlan)
		}
		s.autoSave()
		s.maybeAutoCompact(ctx)
		s.showGitDiffIfBuild()
	}
	done := false
	firstReplTurn := true
	for !done {
		if s.tui == nil {
			s.replPrintPromptForTurn(firstReplTurn)
			s.replSetInputActive()
		}
		firstReplTurn = false
		line, ok := readUserInput(sc)
		if s.tui == nil {
			s.replFlushPendingNotes()
		}
		if !ok {
			break
		}
		if line == "" {
			continue
		}

		// Check for slash command. Slash commands are not echoed into the
		// transcript: they take an action (mode switch, new session, model
		// change, …) and any user-facing feedback comes from the command
		// itself, not from showing the typed input back to the user.
		if cmd, args, ok := cmds.Parse(line); ok {
			if cmd == nil {
				fmt.Fprintf(os.Stderr, "codient: unknown command %q — type /help for available commands\n", strings.SplitN(line, " ", 2)[0])
				continue
			}
			if err := cmd.Run(args); err != nil {
				fmt.Fprintf(os.Stderr, "codient: %s: %v\n", cmd.Name, err)
			}
			// Rebuild the runner after mode/config changes.
			runner = s.newRunner()
			if cmd.Name == "exit" {
				done = true
			}
			continue
		}

		// Auto-detect dragged/pasted file paths and rewrite with @ prefixes.
		if rewritten, ok := fileref.DetectPastedPaths(line); ok {
			fmt.Fprintf(os.Stderr, "codient: detected file path(s), loading as context\n")
			line = rewritten
		}

		if s.tui != nil {
			s.tui.prog.Send(tuiUserPromptMsg(line))
		}

		// Normal user message -> execute turn.
		user, err := applyTaskToFirstTurnIfNeeded(s.turn, line, s.goal, s.taskFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task: %v\n", err)
			return 2
		}
		if s.taskSlug == "" {
			s.taskSlug = designstore.TaskSlug(s.goal, s.taskFile, line)
		}
		userMsg, commitLine, err := s.userMessageForTurn(user)
		if err != nil {
			fmt.Fprintf(os.Stderr, "image: %v\n", err)
			return 2
		}
		preModified, preUntracked := s.captureSnapshot()
		histLen := len(s.history)
		s.turn++
		reply, err := s.runTurn(ctx, runner, userMsg, user)
		if err != nil {
			if isInterruptErr(err) {
				fmt.Fprintf(os.Stderr, "\ncodient: interrupted\n")
			} else if errors.Is(err, context.Canceled) {
				// Parent context cancelled (e.g. second Ctrl+C in plain mode) — exit.
				return 0
			} else {
				fmt.Fprintf(os.Stderr, "agent: %v\n", err)
				return 1
			}
		}
		if err == nil {
			s.pushUndoIfChanged(preModified, preUntracked, histLen, commitLine)
			maybeSaveDesign(os.Stderr, ws, s.designSaveDir, s.sessionID, s.lastTurnMode, reply, s.taskSlug, s.cfg.DesignSave)
			s.lastReply = assistout.PrepareAssistantText(reply, s.lastTurnMode == prompt.ModePlan)
		}
		s.autoSave()
		s.maybeAutoCompact(ctx)
		s.showGitDiffIfBuild()

		if err == nil && s.lastTurnMode == prompt.ModePlan {
			designText := assistout.PrepareAssistantText(reply, true)
			s.updatePlanFromReply(designText, line)
		}
	}

	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "stdin: %v\n", err)
		return 2
	}
	return 0
}

// writeREPLPromptUnlocked writes the current REPL prompt (or plan answer prefix) to stderr.
// Caller must hold replPromptMu when used together with replAsyncStderrNote.
func (s *session) writeREPLPromptUnlocked() {
	if s.lastTurnMode == prompt.ModePlan && assistout.ReplySignalsPlanWait(s.lastReply) {
		fmt.Fprint(os.Stderr, assistout.PlanAnswerPrefix(s.cfg.Plain))
	} else {
		fmt.Fprint(os.Stderr, assistout.UserPrompt(s.cfg.Plain))
	}
}

// replPrintPromptForTurn prints the REPL prompt line. isFirstTurn is true only for the first
// iteration of the main REPL loop; async stderr notes may set replSkipFirstLoopPrompt so we
// do not repeat a prompt already drawn before the first readUserInput.
func (s *session) replPrintPromptForTurn(isFirstTurn bool) {
	s.replPromptMu.Lock()
	defer s.replPromptMu.Unlock()
	if isFirstTurn && s.replSkipFirstLoopPrompt {
		s.replSkipFirstLoopPrompt = false
		return
	}
	if !isFirstTurn {
		s.replSkipFirstLoopPrompt = false // drop flag if async set it after the first prompt
		fmt.Fprint(os.Stderr, "\n")
	}
	s.writeREPLPromptUnlocked()
}

// replAsyncStderrNote prints a full line (or lines) to stderr while the REPL may be showing
// a prompt without a trailing newline. When the REPL is blocking on user input
// (replInputActive), the message is deferred to avoid corrupting the input line;
// otherwise it moves to a new line, prints msg, then redraws the prompt.
func (s *session) replAsyncStderrNote(msg string) {
	if msg == "" {
		return
	}
	// In TUI mode the viewport handles display; just print the message.
	if s.tui != nil {
		fmt.Fprint(os.Stderr, msg)
		if !strings.HasSuffix(msg, "\n") {
			fmt.Fprint(os.Stderr, "\n")
		}
		return
	}
	s.replPromptMu.Lock()
	defer s.replPromptMu.Unlock()
	if s.replInputActive {
		s.pendingAsyncNotes = append(s.pendingAsyncNotes, msg)
		return
	}
	fmt.Fprint(os.Stderr, "\n")
	fmt.Fprint(os.Stderr, msg)
	if !strings.HasSuffix(msg, "\n") {
		fmt.Fprint(os.Stderr, "\n")
	}
	s.writeREPLPromptUnlocked()
	s.replSkipFirstLoopPrompt = true
}

// replSetInputActive marks the REPL as blocking on stdin so that async notes
// are deferred rather than printed immediately.
func (s *session) replSetInputActive() {
	s.replPromptMu.Lock()
	s.replInputActive = true
	s.replPromptMu.Unlock()
}

// replFlushPendingNotes clears the input-active flag and prints any async notes
// that were deferred while the user was typing. Safe to call even when no notes
// are pending.
func (s *session) replFlushPendingNotes() {
	s.replPromptMu.Lock()
	defer s.replPromptMu.Unlock()
	s.replInputActive = false
	for _, msg := range s.pendingAsyncNotes {
		fmt.Fprint(os.Stderr, msg)
		if !strings.HasSuffix(msg, "\n") {
			fmt.Fprint(os.Stderr, "\n")
		}
	}
	s.pendingAsyncNotes = nil
}

func (s *session) autoSave() {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return
	}
	state := &sessionstore.SessionState{
		ID:        s.sessionID,
		Workspace: ws,
		Mode:      string(s.mode),
		Model:     s.cfg.Model,
		Messages:  sessionstore.FromOpenAI(s.history),
		Todos:     toolsTodosToSession(s.snapshotTodos()),
	}
	if s.currentCheckpointID != "" {
		state.CurrentCheckpointID = s.currentCheckpointID
	}
	if s.convBranch != "" {
		state.CurrentBranch = s.convBranch
	}
	if s.planPhase != "" {
		state.PlanPhase = string(s.planPhase)
		state.PlanPath = planstore.Path(ws, s.sessionID)
	}
	if err := sessionstore.Save(state); err != nil {
		fmt.Fprintf(os.Stderr, "codient: session save: %v\n", err)
	}
}

func (s *session) buildSlashCommands(ctx context.Context, sc *bufio.Scanner) *slashcmd.Registry {
	cmds := &slashcmd.Registry{}

	cmds.Register(slashcmd.Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Description: "show available commands",
		Run: func(string) error {
			fmt.Fprint(os.Stderr, cmds.Help())
			fmt.Fprint(os.Stderr, "\nTip: end a line with \\ for multiline input. Pasting multiline text is also supported.\n")
			fmt.Fprint(os.Stderr, "Ctrl+C while agent is working cancels the current turn; Ctrl+C at the prompt exits.\n")
			fmt.Fprint(os.Stderr, "Attachments: @path/to/file.go inlines a file's contents; /image path.png attaches an image; /paste attaches from clipboard; @image:path for inline image refs; drag files onto the terminal to auto-detect.\n")
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "hooks",
		Description: "list configured lifecycle hooks (requires hooks_enabled)",
		Run: func(string) error {
			if s.hooksMgr == nil || s.hooksMgr.IsEmpty() {
				fmt.Fprintf(os.Stderr, "No hooks loaded. Set hooks_enabled=true in /config and add ~/.codient/hooks.json or <workspace>/.codient/hooks.json\n")
				return nil
			}
			desc := s.hooksMgr.ListDescriptors()
			if len(desc) == 0 {
				fmt.Fprintf(os.Stderr, "hooks.json loaded but no command hooks are configured.\n")
				return nil
			}
			var cur string
			for _, d := range desc {
				if d.Event != cur {
					if cur != "" {
						fmt.Fprint(os.Stderr, "\n")
					}
					cur = d.Event
					fmt.Fprintf(os.Stderr, "[%s]\n", d.Event)
				}
				m := d.Matcher
				if strings.TrimSpace(m) == "" {
					m = "(all)"
				}
				src := d.SourcePath
				if src == "" {
					src = "?"
				}
				fmt.Fprintf(os.Stderr, "  matcher %q  timeout %ds  %s\n    %s\n", m, d.TimeoutSec, filepath.Base(src), d.Command)
			}
			fmt.Fprint(os.Stderr, "\n")
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "image",
		Usage:       "/image <path>",
		Description: "attach an image (PNG, JPEG, GIF, WebP) to your next message",
		Run: func(args string) error {
			path := strings.TrimSpace(args)
			if path == "" {
				return fmt.Errorf("usage: /image <path-to-image>")
			}
			a, err := imageutil.LoadImage(path, imageutil.DefaultMaxBytes)
			if err != nil {
				return err
			}
			if a.OrigBytes >= imageutil.WarnLargeBytes {
				fmt.Fprintf(os.Stderr, "codient: warning: large image %q (%d bytes)\n", path, a.OrigBytes)
			}
			s.pendingImages = append(s.pendingImages, a)
			fmt.Fprintf(os.Stderr, "codient: attached %q for next message (%d image(s) pending)\n", filepath.Base(path), len(s.pendingImages))
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "paste",
		Usage:       "/paste",
		Description: "attach an image from the clipboard to your next message",
		Run: func(string) error {
			ws := s.cfg.EffectiveWorkspace()
			clipDir := clipboard.ClipboardDir(ws)
			clipCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			path, err := clipboard.SaveImage(clipCtx, clipboard.DefaultExecutor(), clipDir)
			if err != nil {
				return err
			}
			a, err := imageutil.LoadImage(path, imageutil.DefaultMaxBytes)
			if err != nil {
				return err
			}
			if a.OrigBytes >= imageutil.WarnLargeBytes {
				fmt.Fprintf(os.Stderr, "codient: warning: large image %q (%d bytes)\n", path, a.OrigBytes)
			}
			s.pendingImages = append(s.pendingImages, a)
			fmt.Fprintf(os.Stderr, "codient: pasted clipboard image for next message (%d image(s) pending)\n", len(s.pendingImages))
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "config",
		Usage:       "/config [key] [value]",
		Description: "view or set configuration (no args = show all, key = show one, key value = set and save)",
		Run:         func(args string) error { return s.handleConfig(ctx, args) },
	})
	cmds.Register(slashcmd.Command{
		Name:        "setup",
		Description: "guided setup wizard for API connection, chat model, and optional embedding model for semantic search",
		Run: func(string) error {
			s.runSetupWizard(ctx, sc)
			s.applyPostSetupReload(ctx)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "create-skill",
		Usage:       "/create-skill",
		Description: "guided wizard to create a SKILL.md agent skill (workspace or user scope)",
		Run: func(string) error {
			s.runCreateSkillWizard(sc)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "create-rule",
		Usage:       "/create-rule",
		Description: "guided wizard to create a Cursor-style rule (.mdc under .cursor/rules in the workspace)",
		Run: func(string) error {
			s.runCreateRuleWizard(sc)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "skills",
		Description: "list discovered agent skills (name, scope, read_file path)",
		Run: func(string) error {
			s.runListSkillsCommand()
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "exit",
		Aliases:     []string{"quit", "q"},
		Description: "quit the session",
		Run:         func(string) error { return nil },
	})
	cmds.Register(slashcmd.Command{
		Name:        "clear",
		Description: "reset conversation history (same session)",
		Run: func(string) error {
			s.history = nil
			s.lastReply = ""
			s.turn = 0
			s.undoStack = nil
			s.currentCheckpointID = ""
			s.convBranch = "main"
			s.currentPlan = nil
			s.planPhase = ""
			s.pendingImages = nil
			if s.tokenTracker != nil {
				s.tokenTracker.Reset()
			}
			s.todosMu.Lock()
			s.todos = nil
			s.todosMu.Unlock()
			s.syncTodosToTUI()
			s.sendTUIChrome()
			fmt.Fprintf(os.Stderr, "codient: history cleared\n")
			s.autoSave()
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "new",
		Aliases:     []string{"n"},
		Description: "start a brand new session (fresh ID, history, and saved-design namespace)",
		Run: func(string) error {
			ws := s.cfg.EffectiveWorkspace()
			s.sessionID = sessionstore.NewID(ws)
			s.history = nil
			s.lastReply = ""
			s.turn = 0
			s.taskSlug = ""
			s.undoStack = nil
			s.currentCheckpointID = ""
			s.convBranch = "main"
			s.currentPlan = nil
			s.planPhase = ""
			s.pendingImages = nil
			if s.tokenTracker != nil {
				s.tokenTracker.Reset()
			}
			s.todosMu.Lock()
			s.todos = nil
			s.todosMu.Unlock()
			s.syncTodosToTUI()
			s.loadSkillsCatalog()
			if len(s.cfg.ExecAllowlist) > 0 {
				s.execAllow = tools.NewSessionExecAllow(s.cfg.ExecAllowlist)
				s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
			} else {
				s.rebuildSystemPrompt()
			}
			s.captureGitSessionState(s.cfg.EffectiveWorkspace())
			fmt.Fprintf(os.Stderr, "codient: new session %s\n", s.sessionID)
			s.sendTUIChrome()
			s.autoSave()
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "status",
		Description: "show current session state",
		Run: func(string) error {
			fmt.Fprintf(os.Stderr, "  session:   %s\n", s.sessionID)
			fmt.Fprintf(os.Stderr, "  mode:      %s\n", s.mode)
			fmt.Fprintf(os.Stderr, "  model:     %s\n", s.cfg.Model)
			fmt.Fprintf(os.Stderr, "  workspace: %s\n", s.cfg.EffectiveWorkspace())
			fmt.Fprintf(os.Stderr, "  turns:     %d\n", s.turn)
			usage := s.estimateFullContextUsage()
			if s.cfg.ContextWindowTokens > 0 {
				pct := usage * 100 / s.cfg.ContextWindowTokens
				fmt.Fprintf(os.Stderr, "  context:   ~%d / %d tokens (%d%%)\n", usage, s.cfg.ContextWindowTokens, pct)
			} else {
				fmt.Fprintf(os.Stderr, "  context:   ~%d tokens (no window limit set)\n", usage)
			}
			fmt.Fprintf(os.Stderr, "  messages:  %d\n", len(s.history))
			if b := effectiveAutoCheckCmd(s.cfg); b != "" {
				fmt.Fprintf(os.Stderr, "  auto-check (build): %s\n", b)
			} else {
				fmt.Fprintf(os.Stderr, "  auto-check (build): off\n")
			}
			if l := effectiveLintCmd(s.cfg); l != "" {
				fmt.Fprintf(os.Stderr, "  auto-check (lint):  %s\n", l)
			} else {
				fmt.Fprintf(os.Stderr, "  auto-check (lint):  off\n")
			}
			if t := effectiveTestCmd(s.cfg); t != "" {
				fmt.Fprintf(os.Stderr, "  auto-check (test):  %s\n", t)
			} else {
				fmt.Fprintf(os.Stderr, "  auto-check (test):  off\n")
			}
			if s.execAllow != nil {
				if s.execAllow.AllowAll() {
					fmt.Fprintf(os.Stderr, "  exec:      all commands allowed for this session\n")
				}
			}
			if ps := s.planStatusLine(); ps != "" {
				fmt.Fprintf(os.Stderr, "  plan:      %s\n", ps)
			}
			if extra := s.formatCostStatusLine(); extra != "" {
				fmt.Fprint(os.Stderr, extra)
			}
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "cost",
		Aliases:     []string{"tokens"},
		Description: "show session token usage and estimated cost",
		Run: func(string) error {
			s.printCostCommand()
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "tools",
		Description: "list tools currently registered with the agent",
		Run: func(string) error {
			names := s.registry.Names()
			fmt.Fprintf(os.Stderr, "Tools: %s\n", strings.Join(names, ", "))
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "mcp",
		Usage:       "/mcp [server]",
		Description: "list MCP servers and tools (no args = all servers, server = tools for that server)",
		Run: func(args string) error {
			if s.mcpMgr == nil {
				fmt.Fprintf(os.Stderr, "No MCP servers configured. Add mcp_servers to ~/.codient/config.json.\n")
				return nil
			}
			args = strings.TrimSpace(args)
			if args == "" {
				ids := s.mcpMgr.ServerIDs()
				if len(ids) == 0 {
					fmt.Fprintf(os.Stderr, "No MCP servers connected.\n")
					return nil
				}
				fmt.Fprintf(os.Stderr, "MCP servers (%d connected):\n", len(ids))
				for _, id := range ids {
					tt := s.mcpMgr.ServerTools(id)
					fmt.Fprintf(os.Stderr, "  %s (%d tools)\n", id, len(tt))
				}
				return nil
			}
			tt := s.mcpMgr.ServerTools(args)
			if tt == nil {
				fmt.Fprintf(os.Stderr, "MCP server %q not connected.\n", args)
				return nil
			}
			fmt.Fprintf(os.Stderr, "MCP %s (%d tools):\n", args, len(tt))
			for _, t := range tt {
				fmt.Fprintf(os.Stderr, "  %-30s %s\n", t.Name, t.Description)
			}
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "plan-status",
		Aliases:     []string{"ps"},
		Description: "show current plan phase, steps, and approval state",
		Run: func(string) error {
			if s.currentPlan == nil {
				fmt.Fprintf(os.Stderr, "codient: no active plan\n")
				return nil
			}
			fmt.Fprint(os.Stderr, planstore.RenderMarkdown(s.currentPlan))
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "edit-plan",
		Aliases:     []string{"ep"},
		Description: "open the active plan in $EDITOR (or $VISUAL); re-parses on save and bumps revision",
		Run: func(string) error {
			if s.currentPlan == nil {
				return fmt.Errorf("no active plan to edit")
			}
			changed, err := s.editPlanInExternalEditor(s.currentPlan)
			if err != nil {
				return err
			}
			if !changed {
				fmt.Fprintf(os.Stderr, "codient: no changes detected\n")
				return nil
			}
			fmt.Fprintf(os.Stderr, "codient: plan updated to revision %d\n", s.currentPlan.Revision)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "compact",
		Description: "summarize conversation history to save context space",
		Run: func(string) error {
			return s.compactHistory(ctx)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "model",
		Usage:       "/model <name>",
		Description: "switch to a different model",
		Run: func(args string) error {
			name := strings.TrimSpace(args)
			if name == "" {
				fmt.Fprintf(os.Stderr, "current model: %s\n", s.cfg.Model)
				return nil
			}
			return s.handleConfig(ctx, "model "+name)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "workspace",
		Usage:       "/workspace <path>",
		Description: "change the workspace directory",
		Run: func(args string) error {
			path := strings.TrimSpace(args)
			if path == "" {
				fmt.Fprintf(os.Stderr, "current workspace: %s\n", s.cfg.EffectiveWorkspace())
				return nil
			}
			s.cfg.Workspace = path
			s.projectContext = projectinfo.Detect(s.cfg.EffectiveWorkspace())
			s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
			s.captureGitSessionState(s.cfg.EffectiveWorkspace())
			s.warnIfNotGitRepo()
			fmt.Fprintf(os.Stderr, "codient: workspace set to %s\n", path)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "log",
		Usage:       "/log [path]",
		Description: "show or change the log file path",
		Run: func(args string) error {
			path := strings.TrimSpace(args)
			if path == "" {
				if s.agentLog != nil {
					fmt.Fprintf(os.Stderr, "logging is active\n")
				} else {
					fmt.Fprintf(os.Stderr, "logging is off (use /log <path> to enable)\n")
				}
				return nil
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open log: %w", err)
			}
			s.agentLog = agentlog.New(f)
			fmt.Fprintf(os.Stderr, "codient: logging to %s\n", path)
			return nil
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "undo",
		Usage:       "/undo [all]",
		Description: "undo last build turn (or /undo all to revert everything)",
		Run: func(args string) error {
			ws := s.cfg.EffectiveWorkspace()
			if ws == "" {
				return fmt.Errorf("no workspace set")
			}
			if !gitutil.IsRepo(ws) {
				return fmt.Errorf("workspace is not a git repository")
			}

			if strings.TrimSpace(args) == "all" {
				return s.undoAll(ws)
			}
			return s.undoLast(ws)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "diff",
		Usage:       "/diff [path]",
		Description: "show colored git diff vs HEAD (optional file path under workspace)",
		Run: func(args string) error {
			return s.handleDiff(args)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "branch",
		Usage:       "/branch [name]",
		Description: "show current branch, switch to an existing branch, or create and checkout a new branch",
		Run: func(args string) error {
			return s.handleBranch(args)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "pr",
		Usage:       "/pr [draft]",
		Description: "push current branch and open a GitHub pull request (requires gh CLI)",
		Run: func(args string) error {
			return s.handlePR(args)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "checkpoint",
		Aliases:     []string{"cp"},
		Usage:       "/checkpoint [name]",
		Description: "save a named snapshot of conversation + workspace (default name turn-N)",
		Run: func(args string) error {
			return s.createCheckpoint(strings.TrimSpace(args), args)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "checkpoints",
		Aliases:     []string{"cps"},
		Description: "list checkpoints for this session (tree view)",
		Run: func(string) error {
			return s.listCheckpoints()
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "rollback",
		Aliases:     []string{"rb"},
		Usage:       "/rollback <name|id|turn>",
		Description: "restore conversation and workspace to a checkpoint",
		Run: func(args string) error {
			q := strings.TrimSpace(args)
			if q == "" {
				return fmt.Errorf("usage: /rollback <name|id|turn>")
			}
			cp, err := s.resolveCheckpointQuery(q)
			if err != nil {
				return err
			}
			return s.rollbackToCheckpoint(cp)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "fork",
		Usage:       "/fork <name|id|turn> [branch-name]",
		Description: "rollback to a checkpoint and start a new git branch + conversation branch",
		Run: func(args string) error {
			parts := strings.Fields(strings.TrimSpace(args))
			if len(parts) < 1 {
				return fmt.Errorf("usage: /fork <name|id|turn> [branch-name]")
			}
			branch := ""
			if len(parts) > 1 {
				branch = strings.Join(parts[1:], " ")
			}
			return s.forkFromCheckpoint(parts[0], branch)
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "branches",
		Aliases:     []string{"cbranch"},
		Description: "list logical conversation branches (checkpoint forks)",
		Run: func(string) error {
			return s.listConvBranches()
		},
	})
	cmds.Register(slashcmd.Command{
		Name:        "memory",
		Aliases:     []string{"mem"},
		Usage:       "/memory [show|edit|clear [global|workspace]]",
		Description: "view, edit, or clear cross-session memory files",
		Run: func(args string) error {
			return s.handleMemory(args)
		},
	})

	return cmds
}

func (s *session) handleMemory(args string) error {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	sub := strings.ToLower(parts[0])
	subArg := ""
	if len(parts) > 1 {
		subArg = strings.TrimSpace(parts[1])
	}

	stateDir := ""
	if s.memOpts != nil {
		stateDir = s.memOpts.StateDir
	}
	ws := s.cfg.EffectiveWorkspace()

	switch sub {
	case "", "show":
		if s.memory == "" {
			fmt.Fprintf(os.Stderr, "codient: no cross-session memory loaded\n")
			if stateDir != "" {
				fmt.Fprintf(os.Stderr, "  global:    %s\n", prompt.GlobalMemoryPath(stateDir))
			}
			if ws != "" {
				fmt.Fprintf(os.Stderr, "  workspace: %s\n", prompt.WorkspaceMemoryPath(ws))
			}
			return nil
		}
		fmt.Fprintf(os.Stderr, "%s\n", s.memory)
		return nil

	case "edit":
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		scope := strings.ToLower(subArg)
		if scope == "" {
			scope = "workspace"
		}
		var path string
		switch scope {
		case "global":
			if stateDir == "" {
				return fmt.Errorf("global state directory not configured")
			}
			path = prompt.GlobalMemoryPath(stateDir)
		case "workspace":
			if ws == "" {
				return fmt.Errorf("no workspace set")
			}
			path = prompt.WorkspaceMemoryPath(ws)
		default:
			return fmt.Errorf("unknown scope %q; use \"global\" or \"workspace\"", scope)
		}
		if editor == "" {
			fmt.Fprintf(os.Stderr, "codient: $EDITOR not set; edit manually:\n  %s\n", path)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "codient: opening %s in %s\n", path, editor)
		return s.runEditor(editor, path)

	case "clear":
		scope := strings.ToLower(subArg)
		if scope == "" {
			return fmt.Errorf("specify scope: /memory clear global or /memory clear workspace")
		}
		var path string
		switch scope {
		case "global":
			if stateDir == "" {
				return fmt.Errorf("global state directory not configured")
			}
			path = prompt.GlobalMemoryPath(stateDir)
		case "workspace":
			if ws == "" {
				return fmt.Errorf("no workspace set")
			}
			path = prompt.WorkspaceMemoryPath(ws)
		default:
			return fmt.Errorf("unknown scope %q; use \"global\" or \"workspace\"", scope)
		}
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "codient: %s does not exist\n", path)
				return nil
			}
			return err
		}
		fmt.Fprintf(os.Stderr, "codient: removed %s\n", path)
		s.reloadMemory()
		return nil

	case "reload":
		s.reloadMemory()
		fmt.Fprintf(os.Stderr, "codient: memory reloaded\n")
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q; use show, edit, clear, or reload", sub)
	}
}

func (s *session) reloadMemory() {
	stateDir := ""
	if s.memOpts != nil {
		stateDir = s.memOpts.StateDir
	}
	mem, err := prompt.LoadMemory(stateDir, s.cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: reload memory: %v\n", err)
		return
	}
	s.memory = mem
	s.rebuildSystemPrompt()
}

func (s *session) runEditor(editor, path string) error {
	argv := []string{editor, path}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	s.reloadMemory()
	return nil
}

func (s *session) handleConfig(ctx context.Context, args string) error {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	key := strings.ToLower(parts[0])
	value := ""
	if len(parts) > 1 {
		value = strings.TrimSpace(parts[1])
	}

	if key == "" {
		s.printAllConfig()
		return nil
	}

	if value == "" {
		return s.printOneConfig(key)
	}

	if err := s.setConfig(key, value); err != nil {
		return err
	}

	if err := saveCurrentConfig(s.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "codient: %s set to %q (saved)\n", key, value)

	switch key {
	case "sandbox_mode", "sandbox_ro_paths", "sandbox_container_image", "exec_env_passthrough":
		s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	case "model", "base_url", "api_key", "max_concurrent":
		s.client = openaiclient.New(s.cfg)
		s.rebuildSystemPrompt()
		if key == "model" || key == "base_url" {
			s.cfg.ContextWindowTokens = 0
			s.probeAndSetContext(ctx)
		}
	case "fetch_allow_hosts", "fetch_preapproved", "fetch_max_bytes", "fetch_timeout_sec",
		"fetch_web_rate_per_sec", "fetch_web_rate_burst",
		"search_max_results":
		s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	case "autocheck_cmd", "lint_cmd", "test_cmd":
		s.rebuildSystemPrompt()
	case "embedding_model", "embedding_base_url", "embedding_api_key":
		s.startCodeIndex(ctx)
	case "repo_map_tokens":
		s.startRepoMap(ctx)
	case "hooks_enabled":
		ws := s.cfg.EffectiveWorkspace()
		if strings.TrimSpace(s.sessionID) == "" {
			s.sessionID = sessionstore.NewID(ws)
		}
		if hm, herr := hooks.LoadForConfig(s.cfg.HooksEnabled, ws, s.cfg.Model, s.sessionID); herr != nil {
			fmt.Fprintf(os.Stderr, "codient: hooks reload: %v\n", herr)
		} else {
			s.hooksMgr = hm
		}
	}

	if isReasoningTierConfigKey(key) {
		s.client = openaiclient.NewForTier(s.cfg, config.TierLow)
		s.rebuildSystemPrompt()
	}
	s.sendTUIChrome()
	return nil
}

// isReasoningTierConfigKey reports whether key targets one of the
// orchestrator's reasoning-tier connection settings.
func isReasoningTierConfigKey(key string) bool {
	switch key {
	case "low_reasoning_base_url", "low_reasoning_api_key", "low_reasoning_model",
		"high_reasoning_base_url", "high_reasoning_api_key", "high_reasoning_model":
		return true
	}
	return false
}

func (s *session) printAllConfig() {
	masked := s.cfg.APIKey
	if len(masked) > 4 {
		masked = masked[:4] + strings.Repeat("*", len(masked)-4)
	}
	w := os.Stderr
	fmt.Fprintf(w, "  -- Connection --\n")
	fmt.Fprintf(w, "  base_url:              %s\n", s.cfg.BaseURL)
	fmt.Fprintf(w, "  api_key:               %s\n", masked)
	fmt.Fprintf(w, "  model:                 %s\n", s.cfg.Model)
	if lr := strings.TrimSpace(s.cfg.LowReasoning.Model); lr != "" {
		fmt.Fprintf(w, "  low_reasoning_model:   %s\n", lr)
	}
	if hr := strings.TrimSpace(s.cfg.HighReasoning.Model); hr != "" {
		fmt.Fprintf(w, "  high_reasoning_model:  %s\n", hr)
	}
	fmt.Fprintf(w, "\n  -- Defaults --\n")
	fmt.Fprintf(w, "  workspace:             %s\n", s.cfg.Workspace)
	fmt.Fprintf(w, "\n  -- Agent limits --\n")
	fmt.Fprintf(w, "  max_concurrent:        %d\n", s.cfg.MaxConcurrent)
	fmt.Fprintf(w, "\n  -- Exec --\n")
	fmt.Fprintf(w, "  exec_allowlist:        %s\n", strings.Join(s.cfg.ExecAllowlist, ","))
	fmt.Fprintf(w, "  exec_env_passthrough:  %s\n", strings.Join(s.cfg.ExecEnvPassthrough, ","))
	fmt.Fprintf(w, "  exec_timeout_sec:      %d\n", s.cfg.ExecTimeoutSeconds)
	fmt.Fprintf(w, "  exec_max_output_bytes: %d\n", s.cfg.ExecMaxOutputBytes)
	fmt.Fprintf(w, "  sandbox_mode:          %s\n", s.cfg.SandboxMode)
	fmt.Fprintf(w, "  sandbox_ro_paths:      %s\n", strings.Join(s.cfg.SandboxReadOnlyPaths, ","))
	fmt.Fprintf(w, "  sandbox_container_image: %s\n", s.cfg.SandboxContainerImage)
	fmt.Fprintf(w, "\n  -- Context --\n")
	fmt.Fprintf(w, "  context_window:        %d\n", s.cfg.ContextWindowTokens)
	fmt.Fprintf(w, "  context_reserve:       %d\n", s.cfg.ContextReserveTokens)
	fmt.Fprintf(w, "\n  -- LLM --\n")
	fmt.Fprintf(w, "  max_llm_retries:       %d\n", s.cfg.MaxLLMRetries)
	fmt.Fprintf(w, "  stream_with_tools:     %v\n", s.cfg.StreamWithTools)
	fmt.Fprintf(w, "\n  -- Fetch --\n")
	fmt.Fprintf(w, "  fetch_allow_hosts:     %s\n", strings.Join(s.cfg.FetchAllowHosts, ","))
	fmt.Fprintf(w, "  fetch_preapproved:     %v\n", s.cfg.FetchPreapproved)
	fmt.Fprintf(w, "  fetch_max_bytes:       %d\n", s.cfg.FetchMaxBytes)
	fmt.Fprintf(w, "  fetch_timeout_sec:     %d\n", s.cfg.FetchTimeoutSec)
	fmt.Fprintf(w, "  fetch_web_rate_per_sec: %d\n", s.cfg.FetchWebRatePerSec)
	fmt.Fprintf(w, "  fetch_web_rate_burst:   %d\n", s.cfg.FetchWebRateBurst)
	fmt.Fprintf(w, "\n  -- Search --\n")
	fmt.Fprintf(w, "  search_max_results:    %d\n", s.cfg.SearchMaxResults)
	fmt.Fprintf(w, "\n  -- Auto --\n")
	fmt.Fprintf(w, "  autocompact_threshold: %d\n", s.cfg.AutoCompactPct)
	fmt.Fprintf(w, "  autocheck_cmd:         %s\n", s.cfg.AutoCheckCmd)
	fmt.Fprintf(w, "  lint_cmd:              %s\n", s.cfg.LintCmd)
	fmt.Fprintf(w, "  test_cmd:              %s\n", s.cfg.TestCmd)
	fmt.Fprintf(w, "\n  -- Git (build mode) --\n")
	fmt.Fprintf(w, "  git_auto_commit:       %v\n", s.cfg.GitAutoCommit)
	fmt.Fprintf(w, "  delegate_git_worktrees: %v\n", s.cfg.DelegateGitWorktrees)
	fmt.Fprintf(w, "  git_protected_branches: %s\n", strings.Join(s.cfg.GitProtectedBranches, ","))
	fmt.Fprintf(w, "  checkpoint_auto:       %s (plan|all|off)\n", s.cfg.CheckpointAuto)
	fmt.Fprintf(w, "\n  -- UI/Output --\n")
	fmt.Fprintf(w, "  plain:                 %v\n", s.cfg.Plain)
	fmt.Fprintf(w, "  quiet:                 %v\n", s.cfg.Quiet)
	fmt.Fprintf(w, "  verbose:               %v\n", s.cfg.Verbose)
	fmt.Fprintf(w, "  mouse_enabled:         %v\n", s.cfg.MouseEnabled)
	fmt.Fprintf(w, "  log:                   %s\n", s.cfg.LogPath)
	fmt.Fprintf(w, "  stream_reply:          %v\n", s.cfg.StreamReply)
	fmt.Fprintf(w, "  progress:              %v\n", s.cfg.Progress)
	fmt.Fprintf(w, "\n  -- Plan --\n")
	fmt.Fprintf(w, "  design_save_dir:       %s\n", s.cfg.DesignSaveDir)
	fmt.Fprintf(w, "  design_save:           %v\n", s.cfg.DesignSave)
	fmt.Fprintf(w, "  plan_tot:              %v\n", s.cfg.PlanTot)
	fmt.Fprintf(w, "\n  -- Project --\n")
	fmt.Fprintf(w, "  project_context:       %s\n", s.cfg.ProjectContext)
	fmt.Fprintf(w, "\n  -- Tools --\n")
	astGrepDisplay := s.cfg.AstGrep
	if astGrepDisplay == "" {
		astGrepDisplay = "(not installed)"
	}
	fmt.Fprintf(w, "  ast_grep:              %s\n", astGrepDisplay)
	embModel := s.cfg.EmbeddingModel
	if embModel == "" {
		embModel = "(not configured)"
	}
	fmt.Fprintf(w, "  embedding_model:       %s\n", embModel)
	embBase := s.cfg.EmbeddingBaseURL
	if embBase == "" {
		embBase = "(inherit base_url)"
	}
	fmt.Fprintf(w, "  embedding_base_url:    %s\n", embBase)
	embKey := s.cfg.EmbeddingAPIKey
	if embKey == "" {
		embKey = "(inherit api_key)"
	} else if len(embKey) > 4 {
		embKey = embKey[:4] + strings.Repeat("*", len(embKey)-4)
	}
	fmt.Fprintf(w, "  embedding_api_key:     %s\n", embKey)
	fmt.Fprintf(w, "  hooks_enabled:         %v\n", s.cfg.HooksEnabled)
	fmt.Fprintf(w, "\n  -- Cost estimate --\n")
	if s.cfg.CostPerMTok != nil {
		fmt.Fprintf(w, "  cost_per_mtok:         %g %g (input output USD per 1M)\n", s.cfg.CostPerMTok.Input, s.cfg.CostPerMTok.Output)
	} else {
		fmt.Fprintf(w, "  cost_per_mtok:         (built-in table; set two numbers to override)\n")
	}
	fmt.Fprintf(w, "\n  -- Reasoning tiers --\n")
	for _, tier := range []struct{ label, name string }{{"low", config.TierLow}, {"high", config.TierHigh}} {
		base, key, model := s.cfg.ConnectionForTier(tier.name)
		maskedKey := key
		if len(maskedKey) > 4 {
			maskedKey = maskedKey[:4] + strings.Repeat("*", len(maskedKey)-4)
		}
		fmt.Fprintf(w, "  %s_reasoning_base_url:  %s\n", tier.label, base)
		fmt.Fprintf(w, "  %s_reasoning_api_key:   %s\n", tier.label, maskedKey)
		fmt.Fprintf(w, "  %s_reasoning_model:     %s\n", tier.label, model)
	}
	fmt.Fprintf(w, "\nSet a value: /config <key> <value>\n")
}

func (s *session) printOneConfig(key string) error {
	v, ok := s.getConfigValue(key)
	if !ok {
		return fmt.Errorf("unknown config key %q", key)
	}
	fmt.Fprintf(os.Stderr, "  %s: %s\n", key, v)
	return nil
}

func (s *session) getConfigValue(key string) (string, bool) {
	switch key {
	case "base_url":
		return s.cfg.BaseURL, true
	case "api_key":
		masked := s.cfg.APIKey
		if len(masked) > 4 {
			masked = masked[:4] + strings.Repeat("*", len(masked)-4)
		}
		return masked, true
	case "model":
		return s.cfg.Model, true
	case "workspace":
		return s.cfg.Workspace, true
	case "max_concurrent":
		return strconv.Itoa(s.cfg.MaxConcurrent), true
	case "exec_allowlist":
		return strings.Join(s.cfg.ExecAllowlist, ","), true
	case "exec_env_passthrough":
		return strings.Join(s.cfg.ExecEnvPassthrough, ","), true
	case "sandbox_mode":
		return s.cfg.SandboxMode, true
	case "sandbox_ro_paths":
		return strings.Join(s.cfg.SandboxReadOnlyPaths, ","), true
	case "sandbox_container_image":
		return s.cfg.SandboxContainerImage, true
	case "exec_timeout_sec":
		return strconv.Itoa(s.cfg.ExecTimeoutSeconds), true
	case "exec_max_output_bytes":
		return strconv.Itoa(s.cfg.ExecMaxOutputBytes), true
	case "context_window":
		return strconv.Itoa(s.cfg.ContextWindowTokens), true
	case "context_reserve":
		return strconv.Itoa(s.cfg.ContextReserveTokens), true
	case "max_llm_retries":
		return strconv.Itoa(s.cfg.MaxLLMRetries), true
	case "stream_with_tools":
		return strconv.FormatBool(s.cfg.StreamWithTools), true
	case "fetch_allow_hosts":
		return strings.Join(s.cfg.FetchAllowHosts, ","), true
	case "fetch_preapproved":
		return strconv.FormatBool(s.cfg.FetchPreapproved), true
	case "fetch_max_bytes":
		return strconv.Itoa(s.cfg.FetchMaxBytes), true
	case "fetch_timeout_sec":
		return strconv.Itoa(s.cfg.FetchTimeoutSec), true
	case "fetch_web_rate_per_sec":
		return strconv.Itoa(s.cfg.FetchWebRatePerSec), true
	case "fetch_web_rate_burst":
		return strconv.Itoa(s.cfg.FetchWebRateBurst), true
	case "search_max_results":
		return strconv.Itoa(s.cfg.SearchMaxResults), true
	case "autocompact_threshold":
		return strconv.Itoa(s.cfg.AutoCompactPct), true
	case "autocheck_cmd":
		return s.cfg.AutoCheckCmd, true
	case "lint_cmd":
		return s.cfg.LintCmd, true
	case "test_cmd":
		return s.cfg.TestCmd, true
	case "plain":
		return strconv.FormatBool(s.cfg.Plain), true
	case "quiet":
		return strconv.FormatBool(s.cfg.Quiet), true
	case "verbose":
		return strconv.FormatBool(s.cfg.Verbose), true
	case "mouse_enabled":
		return strconv.FormatBool(s.cfg.MouseEnabled), true
	case "log":
		return s.cfg.LogPath, true
	case "stream_reply":
		return strconv.FormatBool(s.cfg.StreamReply), true
	case "progress":
		return strconv.FormatBool(s.cfg.Progress), true
	case "design_save_dir":
		return s.cfg.DesignSaveDir, true
	case "design_save":
		return strconv.FormatBool(s.cfg.DesignSave), true
	case "plan_tot":
		return strconv.FormatBool(s.cfg.PlanTot), true
	case "project_context":
		return s.cfg.ProjectContext, true
	case "ast_grep":
		return s.cfg.AstGrep, true
	case "embedding_model":
		return s.cfg.EmbeddingModel, true
	case "embedding_base_url":
		return s.cfg.EmbeddingBaseURL, true
	case "embedding_api_key":
		masked := s.cfg.EmbeddingAPIKey
		if len(masked) > 4 {
			masked = masked[:4] + strings.Repeat("*", len(masked)-4)
		}
		return masked, true
	case "hooks_enabled":
		return strconv.FormatBool(s.cfg.HooksEnabled), true
	case "cost_per_mtok":
		if s.cfg.CostPerMTok == nil {
			return "(not set — built-in pricing table when available)", true
		}
		return fmt.Sprintf("%g %g (input output USD per 1M tokens)", s.cfg.CostPerMTok.Input, s.cfg.CostPerMTok.Output), true
	case "git_auto_commit":
		return strconv.FormatBool(s.cfg.GitAutoCommit), true
	case "delegate_git_worktrees":
		return strconv.FormatBool(s.cfg.DelegateGitWorktrees), true
	case "git_protected_branches":
		return strings.Join(s.cfg.GitProtectedBranches, ","), true
	case "checkpoint_auto":
		return s.cfg.CheckpointAuto, true
	}

	if base, apiKey, model, ok := s.reasoningTierConfigValue(key); ok {
		switch {
		case strings.HasSuffix(key, "_base_url"):
			return base, true
		case strings.HasSuffix(key, "_api_key"):
			masked := apiKey
			if len(masked) > 4 {
				masked = masked[:4] + strings.Repeat("*", len(masked)-4)
			}
			return masked, true
		case strings.HasSuffix(key, "_model"):
			return model, true
		}
	}
	return "", false
}

// reasoningTierConfigValue returns the resolved (base, key, model) for a
// reasoning-tier config key like "low_reasoning_model" or
// "high_reasoning_base_url". ok is false for keys that are not tier-prefixed.
func (s *session) reasoningTierConfigValue(key string) (base, apiKey, model string, ok bool) {
	switch {
	case strings.HasPrefix(key, "low_reasoning_"):
		base, apiKey, model = s.cfg.ConnectionForTier(config.TierLow)
		return base, apiKey, model, true
	case strings.HasPrefix(key, "high_reasoning_"):
		base, apiKey, model = s.cfg.ConnectionForTier(config.TierHigh)
		return base, apiKey, model, true
	}
	return "", "", "", false
}

func (s *session) setConfig(key, value string) error {
	parseInt := func(v string) (int, error) { return strconv.Atoi(v) }
	parseBool := func(v string) (bool, error) { return strconv.ParseBool(v) }

	switch key {
	case "base_url":
		s.cfg.BaseURL = strings.TrimRight(value, "/")
	case "api_key":
		s.cfg.APIKey = value
	case "model":
		s.cfg.Model = value
	case "workspace":
		s.cfg.Workspace = value
	case "max_concurrent":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("max_concurrent must be a positive integer")
		}
		s.cfg.MaxConcurrent = n
	case "exec_allowlist":
		s.cfg.ExecAllowlist = config.ParseExecAllowlistString(value)
	case "exec_env_passthrough":
		s.cfg.ExecEnvPassthrough = config.ParseExecEnvPassthroughString(value)
	case "sandbox_mode":
		s.cfg.SandboxMode = strings.TrimSpace(strings.ToLower(value))
		if err := config.ValidateSandbox(s.cfg); err != nil {
			return err
		}
	case "sandbox_ro_paths":
		s.cfg.SandboxReadOnlyPaths = config.ParseSandboxReadOnlyPathsString(value)
	case "sandbox_container_image":
		s.cfg.SandboxContainerImage = strings.TrimSpace(value)
		if err := config.ValidateSandbox(s.cfg); err != nil {
			return err
		}
	case "exec_timeout_sec":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("exec_timeout_sec must be a positive integer")
		}
		s.cfg.ExecTimeoutSeconds = n
	case "exec_max_output_bytes":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("exec_max_output_bytes must be a positive integer")
		}
		s.cfg.ExecMaxOutputBytes = n
	case "context_window":
		n, err := parseInt(value)
		if err != nil || n < 0 {
			return fmt.Errorf("context_window must be a non-negative integer")
		}
		s.cfg.ContextWindowTokens = n
	case "context_reserve":
		n, err := parseInt(value)
		if err != nil || n < 0 {
			return fmt.Errorf("context_reserve must be a non-negative integer")
		}
		s.cfg.ContextReserveTokens = n
	case "max_llm_retries":
		n, err := parseInt(value)
		if err != nil || n < 0 {
			return fmt.Errorf("max_llm_retries must be a non-negative integer")
		}
		s.cfg.MaxLLMRetries = n
	case "stream_with_tools":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("stream_with_tools must be true or false")
		}
		s.cfg.StreamWithTools = b
	case "fetch_allow_hosts":
		s.cfg.FetchAllowHosts = config.ParseFetchAllowHostsString(value)
	case "fetch_preapproved":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("fetch_preapproved must be true or false")
		}
		s.cfg.FetchPreapproved = b
	case "fetch_max_bytes":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("fetch_max_bytes must be a positive integer")
		}
		s.cfg.FetchMaxBytes = n
	case "fetch_timeout_sec":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("fetch_timeout_sec must be a positive integer")
		}
		s.cfg.FetchTimeoutSec = n
	case "fetch_web_rate_per_sec":
		n, err := parseInt(value)
		if err != nil || n < 0 {
			return fmt.Errorf("fetch_web_rate_per_sec must be a non-negative integer (0 disables)")
		}
		if n > config.MaxFetchWebRatePerSec {
			n = config.MaxFetchWebRatePerSec
		}
		s.cfg.FetchWebRatePerSec = n
		if n == 0 {
			s.cfg.FetchWebRateBurst = 0
		} else if s.cfg.FetchWebRateBurst < 1 {
			b := n
			if b > config.MaxFetchWebRateBurst {
				b = config.MaxFetchWebRateBurst
			}
			s.cfg.FetchWebRateBurst = b
		}
	case "fetch_web_rate_burst":
		n, err := parseInt(value)
		if err != nil || n < 0 {
			return fmt.Errorf("fetch_web_rate_burst must be a non-negative integer")
		}
		if n > config.MaxFetchWebRateBurst {
			n = config.MaxFetchWebRateBurst
		}
		s.cfg.FetchWebRateBurst = n
	case "search_max_results":
		n, err := parseInt(value)
		if err != nil || n < 1 {
			return fmt.Errorf("search_max_results must be a positive integer")
		}
		s.cfg.SearchMaxResults = n
	case "autocompact_threshold":
		n, err := parseInt(value)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("autocompact_threshold must be 0-100")
		}
		s.cfg.AutoCompactPct = n
	case "autocheck_cmd":
		s.cfg.AutoCheckCmd = value
	case "lint_cmd":
		s.cfg.LintCmd = value
	case "test_cmd":
		s.cfg.TestCmd = value
	case "plain":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("plain must be true or false")
		}
		s.cfg.Plain = b
	case "quiet":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("quiet must be true or false")
		}
		s.cfg.Quiet = b
	case "verbose":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("verbose must be true or false")
		}
		s.cfg.Verbose = b
	case "mouse_enabled":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("mouse_enabled must be true or false")
		}
		s.cfg.MouseEnabled = b
	case "log":
		s.cfg.LogPath = value
	case "stream_reply":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("stream_reply must be true or false")
		}
		s.cfg.StreamReply = b
	case "progress":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("progress must be true or false")
		}
		s.cfg.Progress = b
	case "design_save_dir":
		s.cfg.DesignSaveDir = value
	case "design_save":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("design_save must be true or false")
		}
		s.cfg.DesignSave = b
	case "plan_tot":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("plan_tot must be true or false")
		}
		s.cfg.PlanTot = b
	case "project_context":
		s.cfg.ProjectContext = value
	case "ast_grep":
		s.cfg.AstGrep = value
		s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	case "embedding_model":
		s.cfg.EmbeddingModel = value
	case "embedding_base_url":
		s.cfg.EmbeddingBaseURL = strings.TrimRight(strings.TrimSpace(value), "/")
	case "embedding_api_key":
		s.cfg.EmbeddingAPIKey = strings.TrimSpace(value)
	case "repo_map_tokens":
		n, err := parseInt(value)
		if err != nil || n < -1 {
			return fmt.Errorf("repo_map_tokens must be an integer >= -1 (0=auto, -1=off)")
		}
		s.cfg.RepoMapTokens = n
	case "hooks_enabled":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("hooks_enabled must be true or false")
		}
		s.cfg.HooksEnabled = b
	case "cost_per_mtok":
		fields := strings.Fields(value)
		if len(fields) == 0 || strings.EqualFold(fields[0], "off") || strings.EqualFold(fields[0], "clear") {
			s.cfg.CostPerMTok = nil
			return nil
		}
		if len(fields) != 2 {
			return fmt.Errorf("cost_per_mtok expects two numbers (USD per 1M input and output tokens), or \"off\"")
		}
		in, err1 := strconv.ParseFloat(fields[0], 64)
		out, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("cost_per_mtok: invalid number")
		}
		if in < 0 || out < 0 {
			return fmt.Errorf("cost_per_mtok: rates must be non-negative")
		}
		s.cfg.CostPerMTok = &config.CostPerMTok{Input: in, Output: out}
	case "git_auto_commit":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("git_auto_commit must be true or false")
		}
		s.cfg.GitAutoCommit = b
	case "delegate_git_worktrees":
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("delegate_git_worktrees must be true or false")
		}
		s.cfg.DelegateGitWorktrees = b
	case "git_protected_branches":
		s.cfg.GitProtectedBranches = config.ParseGitProtectedBranches(value)
		if len(s.cfg.GitProtectedBranches) == 0 {
			s.cfg.GitProtectedBranches = []string{"main", "master", "develop"}
		}
	case "checkpoint_auto":
		v := strings.TrimSpace(strings.ToLower(value))
		if v == "" {
			v = "plan"
		}
		if v != "plan" && v != "all" && v != "off" {
			return fmt.Errorf("checkpoint_auto must be plan, all, or off")
		}
		s.cfg.CheckpointAuto = v
	case "low_reasoning_base_url":
		s.cfg.LowReasoning.BaseURL = strings.TrimRight(value, "/")
	case "low_reasoning_api_key":
		s.cfg.LowReasoning.APIKey = value
	case "low_reasoning_model":
		s.cfg.LowReasoning.Model = value
	case "high_reasoning_base_url":
		s.cfg.HighReasoning.BaseURL = strings.TrimRight(value, "/")
	case "high_reasoning_api_key":
		s.cfg.HighReasoning.APIKey = value
	case "high_reasoning_model":
		s.cfg.HighReasoning.Model = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

// resolveAstGrep resolves the ast-grep binary path into cfg.AstGrep.
// If the binary is not found and a scanner is available (interactive mode),
// the user is prompted to download it. Non-interactive sessions silently skip.
func resolveAstGrep(cfg *config.Config, sc *bufio.Scanner) {
	v := strings.TrimSpace(strings.ToLower(cfg.AstGrep))
	if v == "off" {
		cfg.AstGrep = ""
		return
	}
	if v != "" && v != "auto" {
		if _, err := os.Stat(cfg.AstGrep); err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "codient: configured ast-grep path %q not found, falling back to auto-detect\n", cfg.AstGrep)
	}

	if p := astgrep.Resolve(); p != "" {
		cfg.AstGrep = p
		return
	}

	if sc == nil {
		cfg.AstGrep = ""
		return
	}

	fmt.Fprintf(os.Stderr, "codient: ast-grep not found. Install it for structural code search (find_references)? [Y/n] ")
	if !sc.Scan() {
		cfg.AstGrep = ""
		return
	}
	answer := strings.ToLower(strings.TrimSpace(sc.Text()))
	if answer != "" && answer != "y" && answer != "yes" {
		cfg.AstGrep = ""
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Fprintf(os.Stderr, "codient: downloading ast-grep...\n")
	destDir, err := astgrep.BinDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: ast-grep setup: %v\n", err)
		cfg.AstGrep = ""
		return
	}
	path, err := astgrep.Download(ctx, destDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: ast-grep download failed: %v\n", err)
		cfg.AstGrep = ""
		return
	}
	cfg.AstGrep = path
	fmt.Fprintf(os.Stderr, "codient: ast-grep installed to %s\n", path)
}

// probeAndSetContext tries to detect the server's context window for the current model.
// If cfg.ContextWindowTokens is already set in config, this is a no-op.
func (s *session) probeAndSetContext(ctx context.Context) {
	if s.cfg.ContextWindowTokens > 0 {
		return
	}
	model := strings.TrimSpace(s.cfg.Model)
	if model == "" {
		return
	}
	c := openaiclient.New(s.cfg)
	n, err := c.ProbeContextWindow(ctx, model)
	if err != nil || n <= 0 {
		return
	}
	s.cfg.ContextWindowTokens = n
}

func messageTextForEstimate(m openai.ChatCompletionMessageParamUnion) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// computeUndoEntry calculates the set of files changed by a single turn by
// diffing the pre-turn and post-turn git state. Returns nil if nothing changed.
func computeUndoEntry(preModified, preUntracked, postModified, postUntracked []string, histLen int) *undoEntry {
	modified := setDiff(postModified, preModified)
	created := setDiff(postUntracked, preUntracked)
	if len(modified) == 0 && len(created) == 0 {
		return nil
	}
	return &undoEntry{
		modifiedFiles: modified,
		createdFiles:  created,
		historyLen:    histLen,
	}
}

// startCodeIndex launches background indexing if an embedding model is configured.
// semantic_search is registered with the new index; Query blocks until the initial build finishes.
//
// Embedding requests use a dedicated client built from EmbeddingBaseURL / EmbeddingAPIKey
// (falling back to the chat connection) so users can route /v1/embeddings to a local server
// while chat targets a hosted API that does not implement embeddings.
func (s *session) startCodeIndex(ctx context.Context) {
	model := strings.TrimSpace(s.cfg.EmbeddingModel)
	ws := s.cfg.EffectiveWorkspace()
	if model == "" || ws == "" {
		return
	}
	embClient := openaiclient.NewForEmbedding(s.cfg)
	s.codeIndex = codeindex.New(ws, embClient, model)
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	fmt.Fprintf(os.Stderr, "codient: indexing workspace for semantic search...\n")
	go func() {
		s.codeIndex.BuildOrUpdate(ctx)
		n := s.codeIndex.Len()
		if err := s.codeIndex.BuildErr(); err != nil {
			s.replAsyncStderrNote(fmt.Sprintf("codient: semantic index: %v\n", err))
		} else if n > 0 {
			s.replAsyncStderrNote(fmt.Sprintf("codient: semantic index ready (%d files)\n", n))
		}
	}()
}

// startRepoMap builds a structural symbol map in the background (or clears it when disabled).
// When complete, the registry and system prompt are rebuilt to include repo_map and the map text.
func (s *session) startRepoMap(ctx context.Context) {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return
	}
	if s.cfg.RepoMapTokens < 0 {
		s.repoMap = nil
		s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
		return
	}

	s.repoMap = repomap.New(ws)
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))

	fmt.Fprintf(os.Stderr, "codient: building repository map...\n")
	go func() {
		s.repoMap.Build(ctx)
		nf := s.repoMap.FileCount()
		nt := s.repoMap.TagCount()
		if err := s.repoMap.BuildErr(); err != nil {
			s.replAsyncStderrNote(fmt.Sprintf("codient: repo map: %v\n", err))
		} else if nf > 0 {
			s.replAsyncStderrNote(fmt.Sprintf("codient: repo map ready (%d files, %d symbols)\n", nf, nt))
		}
		reg := buildRegistry(s.cfg, s.mode, s, s.memOpts)
		s.installRegistry(reg)
	}()
}

// updatePlanFromReply parses the agent's plan-mode markdown into a structured
// plan and persists it. If a plan already exists, the parsed content is merged
// (keeping the existing session/revision metadata).
func (s *session) updatePlanFromReply(markdown, userRequest string) {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" {
		return
	}
	parsed := planstore.ParseFromMarkdown(markdown, userRequest)
	if s.currentPlan == nil {
		parsed.SessionID = s.sessionID
		parsed.Workspace = ws
		parsed.Revision = 1
		s.currentPlan = parsed
	} else {
		s.currentPlan.Summary = parsed.Summary
		s.currentPlan.Steps = parsed.Steps
		s.currentPlan.Assumptions = parsed.Assumptions
		s.currentPlan.OpenQuestions = parsed.OpenQuestions
		s.currentPlan.FilesToModify = parsed.FilesToModify
		s.currentPlan.Verification = parsed.Verification
		s.currentPlan.RawMarkdown = parsed.RawMarkdown
		if s.currentPlan.UserRequest == "" {
			s.currentPlan.UserRequest = parsed.UserRequest
		}
	}
	s.currentPlan.Phase = planstore.PhaseDraft
	s.planPhase = planstore.PhaseDraft
	if err := planstore.Save(s.currentPlan); err != nil {
		fmt.Fprintf(os.Stderr, "codient: plan save: %v\n", err)
	}
}

// loadPlanFromDisk loads a previously saved plan for the current session.
func (s *session) loadPlanFromDisk() {
	ws := s.cfg.EffectiveWorkspace()
	if ws == "" || s.sessionID == "" {
		return
	}
	plan, err := planstore.Load(ws, s.sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codient: plan load: %v\n", err)
		return
	}
	if plan == nil {
		return
	}
	s.currentPlan = plan
	s.planPhase = plan.Phase
}

// handlePlanResume is called on session resume when an active plan exists.
// It shows the plan status and offers resume options.
func (s *session) handlePlanResume(ctx context.Context, sc *bufio.Scanner) {
	plan := s.currentPlan
	done, total := 0, len(plan.Steps)
	for _, st := range plan.Steps {
		if st.Status == planstore.StepDone || st.Status == planstore.StepSkipped {
			done++
		}
	}
	fmt.Fprintf(os.Stderr, "\ncodient: resuming plan (rev %d, phase %s, steps %d/%d)\n", plan.Revision, plan.Phase, done, total)

	switch s.planPhase {
	case planstore.PhaseDraft, planstore.PhaseAwaitingApproval:
		fmt.Fprintf(os.Stderr, "codient: plan is in %s phase — describe what you'd like to refine and the orchestrator will route to plan mode\n", s.planPhase)

	case planstore.PhaseApproved, planstore.PhaseExecuting:
		fmt.Fprintf(os.Stderr, "\n  [r] Resume execution from current step\n")
		fmt.Fprintf(os.Stderr, "  [p] Re-plan (reset plan to draft)\n")
		fmt.Fprintf(os.Stderr, "  [i] Ignore plan and start fresh\n")
		fmt.Fprintf(os.Stderr, "\ncodient: choose action: ")
		if !sc.Scan() {
			return
		}
		choice := strings.ToLower(strings.TrimSpace(sc.Text()))
		switch choice {
		case "r", "resume":
			if err := s.executeFromPlan(ctx, plan); err != nil {
				fmt.Fprintf(os.Stderr, "agent: %v\n", err)
			}
		case "p", "replan":
			plan.Phase = planstore.PhaseDraft
			s.planPhase = planstore.PhaseDraft
			planstore.IncrementRevision(plan)
			if err := planstore.Save(plan); err != nil {
				fmt.Fprintf(os.Stderr, "codient: plan save: %v\n", err)
			}
		default:
			s.currentPlan = nil
			s.planPhase = ""
			s.autoSave()
		}

	case planstore.PhaseReview:
		fmt.Fprintf(os.Stderr, "codient: plan was in review phase — re-running verification\n")
		if s.scanner == nil {
			s.scanner = sc
		}
		passed, err := s.runVerification(ctx, sc, plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "codient: verification error: %v\n", err)
		}
		if passed {
			fmt.Fprintf(os.Stderr, "codient: verification passed\n")
		}
	}
}

// planStatusLine returns a one-line summary of the current plan for /status.
func (s *session) planStatusLine() string {
	if s.currentPlan == nil {
		return ""
	}
	p := s.currentPlan
	done, total := 0, len(p.Steps)
	for _, st := range p.Steps {
		if st.Status == planstore.StepDone || st.Status == planstore.StepSkipped {
			done++
		}
	}
	return fmt.Sprintf("rev %d, phase %s, steps %d/%d", p.Revision, p.Phase, done, total)
}

// isInterruptErr reports whether err indicates a user-initiated turn cancellation
// (Ctrl+C) rather than a timeout or other context error from the parent session.
func isInterruptErr(err error) bool {
	return errors.Is(err, context.Canceled)
}

func setDiff(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := set[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
