// Command codient is a CLI coding agent using an OpenAI-compatible chat API (openai-go client).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/agentlog"
	"codient/internal/assistout"
	"codient/internal/config"
	"codient/internal/designstore"
	"codient/internal/openaiclient"
	"codient/internal/projectinfo"
	"codient/internal/prompt"
	"codient/internal/tools"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		system        = flag.String("system", "", "optional system prompt (merged into default tool-capabilities prompt)")
		promptFlag    = flag.String("prompt", "", "user message: without REPL, stdin is used if flag empty; with REPL, non-empty -prompt is the first turn")
		stream        = flag.Bool("stream", false, "single-turn streamed completion without tools (writes to stdout)")
		listModels    = flag.Bool("list-models", false, "print model ids from GET /v1/models and exit")
		listTools     = flag.Bool("list-tools", false, "print registered tool names for current env and exit")
		ping          = flag.Bool("ping", false, "check GET /v1/models and exit")
		timeout       = flag.Duration("timeout", 10*time.Minute, "per-invocation context timeout")
		goal          = flag.String("goal", "", "optional high-level objective; merged into task directive on first turn only")
		taskFile      = flag.String("task-file", "", "optional path to a task description file (capped at 32KiB); merged into task directive on first turn only")
		repl          = flag.Bool("repl", false, "multi-turn REPL (default when stdin is a TTY; kept for backward compatibility)")
		newSession    = flag.Bool("new-session", false, "start a fresh session instead of resuming the latest")
		logPath       = flag.String("log", "", "append JSONL agent events to this file (overrides CODIENT_LOG if set)")
		progress      = flag.Bool("progress", false, "print agent progress to stderr")
		modeFlag      = flag.String("mode", "", "build|ask|plan: tool + prompt policy (default build; when empty, use CODIENT_MODE)")
		plainOut      = flag.Bool("plain", false, "print assistant replies as raw text (no markdown/ANSI); or set CODIENT_PLAIN=1")
		streamReply   = flag.Bool("stream-reply", true, "stream assistant tokens to stdout")
		designSaveDir = flag.String("design-save-dir", "", "directory for saved implementation designs (default: <workspace>/.codient/designs)")
		workspace     = flag.String("workspace", "", "root directory for workspace tools (overrides CODIENT_WORKSPACE and cwd default)")
		a2aFlag       = flag.Bool("a2a", false, "start an A2A (Agent-to-Agent) protocol server instead of the CLI")
		a2aAddr       = flag.String("a2a-addr", ":8080", "listen address for the A2A server")
	)
	flag.Parse()

	agentMode, err := prompt.ResolveMode(*modeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mode: %v\n", err)
		return 2
	}

	effectiveLog := strings.TrimSpace(*logPath)
	if effectiveLog == "" {
		effectiveLog = strings.TrimSpace(os.Getenv("CODIENT_LOG"))
	}
	progressOut := resolveProgressOut(*progress, effectiveLog != "")

	cfg, err := config.Load()
	if err == nil && strings.TrimSpace(*workspace) != "" {
		cfg.Workspace = strings.TrimSpace(*workspace)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}
	// For quick commands and single-turn mode, use a wall-clock timeout.
	// For the REPL session, use a signal-based context so the user can
	// step away without hitting "context deadline exceeded".
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := openaiclient.New(cfg)

	// Quick commands that don't need a full session.
	if *ping {
		if err := client.PingModels(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "ping: %v\n", err)
			return 1
		}
		fmt.Println("ok")
		return 0
	}
	if *listModels {
		ids, err := client.ListModels(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "models: %v\n", err)
			return 1
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return 0
	}
	if *listTools {
		reg := buildRegistry(cfg, agentMode, nil)
		for _, n := range reg.Names() {
			fmt.Println(n)
		}
		return 0
	}
	if *a2aFlag {
		cancel()
		a2aCtx, a2aCancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer a2aCancel()
		var agentLog *agentlog.Logger
		if effectiveLog != "" {
			logFile, err := os.OpenFile(effectiveLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "log: %v\n", err)
				return 2
			}
			defer logFile.Close()
			agentLog = agentlog.New(logFile)
		}
		return runA2AServer(a2aCtx, cfg, *a2aAddr, agentLog)
	}

	if *stream {
		user, err := resolvePrompt(*promptFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt: %v\n", err)
			return 2
		}
		if strings.TrimSpace(user) == "" {
			fmt.Fprintf(os.Stderr, "provide -prompt or pipe a message on stdin\n")
			return 2
		}
		return runBareStream(ctx, client, *system, user)
	}

	var logFile *os.File
	var agentLog *agentlog.Logger
	if effectiveLog != "" {
		var err error
		logFile, err = os.OpenFile(effectiveLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log: %v\n", err)
			return 2
		}
		defer logFile.Close()
		agentLog = agentlog.New(logFile)
	}

	// Build the full agent session.
	repoInstr, err := prompt.LoadRepoInstructions(cfg.EffectiveWorkspace())
	if err != nil {
		fmt.Fprintf(os.Stderr, "repo instructions: %v\n", err)
		return 2
	}
	projectCtx := projectinfo.Detect(cfg.EffectiveWorkspace())
	var execAllow *tools.SessionExecAllow
	if len(cfg.ExecAllowlist) > 0 {
		execAllow = tools.NewSessionExecAllow(cfg.ExecAllowlist)
	}
	s := &session{
		cfg:              cfg,
		client:           client,
		agentLog:         agentLog,
		progressOut:      progressOut,
		mode:             agentMode,
		richOutput:       assistantOutputRich(*plainOut),
		streamReply:      *streamReply,
		designSaveDir:    *designSaveDir,
		goal:             *goal,
		taskFile:         *taskFile,
		userSystem:       *system,
		repoInstructions: repoInstr,
		projectContext:   projectCtx,
		execAllow:        execAllow,
	}
	s.registry = buildRegistry(cfg, agentMode, s)
	s.systemPrompt = buildAgentSystemPrompt(cfg, s.registry, agentMode, *system, repoInstr, projectCtx, effectiveAutoCheckCmd(cfg))

	// Determine whether to enter the REPL session.
	// REPL is the default when stdin is a TTY (interactive), or when -repl is explicit.
	stdinIsTTY := stdinIsInteractive()
	useREPL := *repl || (stdinIsTTY && strings.TrimSpace(*promptFlag) == "")

	if useREPL {
		// Override the timeout context with a signal-based one for the REPL.
		// The session can last indefinitely; only Ctrl+C should cancel it.
		cancel()
		replCtx, replCancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer replCancel()
		return s.runSession(replCtx, *promptFlag, *newSession)
	}

	// Single-turn mode (piped input or explicit -prompt without -repl).
	if err := cfg.RequireModel(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}
	user, err := resolvePrompt(*promptFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt: %v\n", err)
		return 2
	}
	if strings.TrimSpace(user) == "" {
		fmt.Fprintf(os.Stderr, "provide -prompt or pipe a message on stdin\n")
		return 2
	}
	return s.runSingleTurn(ctx, user)
}

func stdinIsInteractive() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func runBareStream(ctx context.Context, client *openaiclient.Client, system, user string) int {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, openai.SystemMessage(strings.TrimSpace(system)))
	}
	msgs = append(msgs, openai.UserMessage(user))
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(client.Model()),
		Messages: msgs,
	}
	if err := client.StreamChatCompletion(ctx, params, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "\nstream: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout)
	return 0
}

func stderrPromptPlain(plainFlag bool) bool {
	return plainFlag || strings.TrimSpace(os.Getenv("CODIENT_PLAIN")) == "1"
}

func assistantOutputRich(plainFlag bool) bool {
	if plainFlag || strings.TrimSpace(os.Getenv("CODIENT_PLAIN")) == "1" {
		return false
	}
	return assistout.StdoutIsInteractive()
}

func resolveProgressOut(progressFlag, logRequested bool) io.Writer {
	if strings.TrimSpace(os.Getenv("CODIENT_PROGRESS")) == "0" {
		return nil
	}
	if progressFlag || strings.TrimSpace(os.Getenv("CODIENT_PROGRESS")) == "1" {
		return os.Stderr
	}
	if logRequested {
		return os.Stderr
	}
	st, err := os.Stderr.Stat()
	if err != nil {
		return nil
	}
	if (st.Mode() & os.ModeCharDevice) != 0 {
		return os.Stderr
	}
	return nil
}

func resolveStreamReply(flag bool, stdoutTTY bool) bool {
	switch strings.TrimSpace(os.Getenv("CODIENT_STREAM_REPLY")) {
	case "0":
		return false
	case "1":
		return true
	}
	return flag && stdoutTTY
}

func streamWriterForTurn(streamReplyFlag bool, stdoutTTY bool, mode prompt.Mode, richAssistant bool, lastAssistantReply string) io.Writer {
	if !resolveStreamReply(streamReplyFlag, stdoutTTY) {
		return nil
	}
	if mode == prompt.ModePlan && richAssistant && assistout.ReplySignalsPlanWait(lastAssistantReply) {
		return nil
	}
	return os.Stdout
}

func writePlanDraftPreamble(w io.Writer, mode prompt.Mode, lastAssistantReply string) {
	if mode != prompt.ModePlan || !assistout.ReplySignalsPlanWait(lastAssistantReply) {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Building the implementation plan…")
}

func finishAssistantTurn(w io.Writer, reply string, useMarkdown, planMode, streamed bool) error {
	if streamed {
		_, err := fmt.Fprintln(w)
		return err
	}
	return assistout.WriteAssistant(w, reply, useMarkdown, planMode)
}

func resolveDesignSaveDir(flag string) string {
	if s := strings.TrimSpace(os.Getenv("CODIENT_DESIGN_SAVE_DIR")); s != "" {
		return s
	}
	return strings.TrimSpace(flag)
}

func maybeSaveDesign(stderr io.Writer, workspace, designSaveDirFlag, sessionID string, mode prompt.Mode, reply string, taskSlug string) {
	if mode != prompt.ModePlan {
		return
	}
	if strings.TrimSpace(os.Getenv("CODIENT_DESIGN_SAVE")) == "0" {
		return
	}
	text := assistout.PrepareAssistantText(reply, true)
	if !designstore.LooksLikeReadyToImplement(text) {
		return
	}
	path, err := designstore.Save(workspace, resolveDesignSaveDir(designSaveDirFlag), sessionID, taskSlug, text, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "codient: saving design: %v\n", err)
		return
	}
	fmt.Fprintf(stderr, "codient: wrote design to %s\n", path)
}

func resolvePrompt(flagPrompt string) (string, error) {
	if strings.TrimSpace(flagPrompt) != "" {
		return flagPrompt, nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
