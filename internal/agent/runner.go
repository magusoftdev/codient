// Package agent runs a tool-calling loop against an OpenAI-compatible chat completions API.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/acpctx"
	"codient/internal/agentlog"
	"codient/internal/config"
	"codient/internal/hooks"
	"codient/internal/openaiclient"
	"codient/internal/tokenest"
	"codient/internal/tokentracker"
	"codient/internal/tools"
)

// EstimateSessionCostFn maps cumulative session usage to estimated USD; ok false means unknown (skip budget check).
type EstimateSessionCostFn func(u tokentracker.Usage) (costUSD float64, ok bool)

// ChatClient is the LLM surface the agent needs (implemented by *openaiclient.Client).
type ChatClient interface {
	ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
	Model() string
}

// ErrorLogger receives turn-level failures and panics for optional process error logs.
type ErrorLogger interface {
	LogError(context string, err error)
	LogPanic(recovered any, stack []byte)
}

// streamChatClient is implemented by *openaiclient.Client for token streaming during agent turns.
type streamChatClient interface {
	ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer, opts ...openaiclient.StreamOption) (*openai.ChatCompletion, error)
}

// AutoCheckOutcome is returned by Runner.AutoCheck after file-mutating tools succeed.
// Inject is a full user message to append (empty means nothing to inject). Progress is one line for Progress (empty to skip).
type AutoCheckOutcome struct {
	Inject      string
	Progress    string
	Passed      bool   // all steps succeeded (when true, Inject is empty)
	Signature   string // stable hash of parsed failures for no-progress detection
	FailingStep string // "build" | "lint" | "test" | "" — which step failed
}

// mutatingTools lists tool names that change files on disk; used to trigger auto-check.
var mutatingTools = map[string]struct{}{
	"write_file": {}, "str_replace": {}, "patch_file": {}, "insert_lines": {},
	"remove_path": {}, "move_path": {}, "copy_path": {}, "lsp_rename": {},
}

// ToolIsMutating reports whether the named tool may modify files on disk.
func ToolIsMutating(name string) bool {
	_, ok := mutatingTools[name]
	return ok
}

// PostReplyCheckInfo is passed to PostReplyCheck after a text-only model reply.
type PostReplyCheckInfo struct {
	Reply              string
	User               string
	TurnTools          []string // tool names invoked this user turn, in order (may repeat)
	Mutated            bool
	AutoCheckExhausted bool
}

// ToolBatchResult is a compact per-tool result for reflection callbacks.
type ToolBatchResult struct {
	Name    string
	Content string
}

// PlanReflectionInfo is passed after a successful mutating tool batch.
type PlanReflectionInfo struct {
	User      string
	TurnTools []string
	Results   []ToolBatchResult
}

// PlanReflectionOutcome can inject a follow-up user message into the loop.
type PlanReflectionOutcome struct {
	Inject   string
	Progress string
}

// Runner executes multi-step tool use with bounded LLM concurrency (via the ChatClient implementation).
type Runner struct {
	LLM   ChatClient
	Cfg   *config.Config
	Tools *tools.Registry
	Log   *agentlog.Logger
	// ErrorLog receives panics from RunConversation and optional error lines; nil skips.
	ErrorLog ErrorLogger
	// Tracker accumulates API-reported token usage; optional.
	Tracker *tokentracker.Tracker
	// Progress, when non-nil (e.g. os.Stderr), receives human-readable lines during the tool loop.
	Progress io.Writer
	// AutoCheck runs once after a tool batch that successfully used a mutating tool.
	// If Inject is non-empty, it is appended as a user message before the next LLM call.
	AutoCheck func(ctx context.Context) AutoCheckOutcome
	// PostReplyCheck, when non-nil, is called when the model produces a text reply
	// (no tool calls). If it returns a non-empty string, that string is injected as
	// a user message and the loop continues instead of returning. The field is nilled
	// after firing once to prevent infinite loops.
	PostReplyCheck func(ctx context.Context, info PostReplyCheckInfo) string
	// PlanReflection, when non-nil, is called after successful mutating tool
	// batches. If Inject is non-empty, it is appended as a user message before
	// the next LLM call.
	PlanReflection func(ctx context.Context, info PlanReflectionInfo) PlanReflectionOutcome
	// ProgressPlain suppresses ANSI styling on progress lines (e.g. -plain).
	ProgressPlain bool
	// ProgressMode is build|ask|plan; colors the thinking/intent bullet to match the REPL mode.
	ProgressMode string
	// MaxTurns caps LLM completion rounds for this user turn (0 = unlimited). Used for CI guardrails.
	MaxTurns int
	// MaxCostUSD caps estimated cumulative session cost in USD (0 = unlimited). Requires EstimateSessionCost.
	MaxCostUSD float64
	// EstimateSessionCost estimates USD for Tracker.Session() after each LLM call; optional.
	EstimateSessionCost EstimateSessionCostFn
	// Hooks runs lifecycle hooks (PreToolUse, PostToolUse, Stop); nil disables.
	Hooks *hooks.Manager
	// OnWorkingChange is called with true when the runner begins waiting for an
	// LLM response and false when the response arrives. Used to drive spinners.
	OnWorkingChange func(working bool)
	// OnToolBefore is invoked before each tool execution (ACP / external UIs).
	OnToolBefore func(ctx context.Context, toolCallID, name string, args json.RawMessage)
	// OnToolAfter is invoked after each tool execution with the display string and API error if any.
	OnToolAfter func(ctx context.Context, toolCallID, name string, args json.RawMessage, display string, err error)
	// OnIntent is invoked when the model emits an intent/thinking preface before tool calls.
	OnIntent func(text string)
	// OnTranscriptEvent surfaces structured progress for rich UIs (e.g. Bubble Tea).
	// Legacy Progress lines are still emitted when Progress is non-nil.
	OnTranscriptEvent func(TranscriptEvent)
	// StopHookActive is true when the next assistant text follows a Stop-hook continuation in this RunConversation.
	StopHookActive bool
	// AutoCheckMaxFixes is the maximum fix-loop iterations after auto-check
	// failures within a single user turn. 0 = single-shot (today's behaviour).
	AutoCheckMaxFixes int
	// AutoCheckStopOnNoProgress aborts the fix loop early when the failure
	// Signature is unchanged between consecutive attempts.
	AutoCheckStopOnNoProgress bool

	toolUseSeq         atomic.Uint64
	autoCheckAttempts  int
	autoCheckLastSig   string
	autoCheckExhausted bool
	turnMutated        bool
}

// Run carries out one user turn (no prior conversation history).
// streamTo is where assistant text deltas are written when streaming (e.g. os.Stdout); nil disables streaming.
// streamed is true when the reply was written incrementally (caller skips glamour for that turn).
func (r *Runner) Run(ctx context.Context, system string, user openai.ChatCompletionMessageParamUnion, streamTo io.Writer) (reply string, streamed bool, err error) {
	reply, _, streamed, err = r.RunConversation(ctx, system, nil, user, streamTo)
	return reply, streamed, err
}

// RunConversation runs one user message with optional prior messages (excluding system).
// history is a slice of user/assistant/tool messages from earlier turns; system is prepended each request.
// Returns the assistant's final text and updated history (including this turn), suitable for REPL.
// streamTo selects streaming for this turn only (nil = non-streaming completion).
// streamed is true when the final reply was produced via streaming (skip glamour in the caller).
func (r *Runner) RunConversation(ctx context.Context, system string, history []openai.ChatCompletionMessageParamUnion, user openai.ChatCompletionMessageParamUnion, streamTo io.Writer) (reply string, newHist []openai.ChatCompletionMessageParamUnion, streamed bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			stack := debug.Stack()
			if r != nil && r.ErrorLog != nil {
				r.ErrorLog.LogPanic(rec, stack)
			}
			reply = ""
			newHist = nil
			streamed = false
			err = fmt.Errorf("agent panic: %v", rec)
		}
	}()
	reply, newHist, streamed, err = r.runConversationBody(ctx, system, history, user, streamTo)
	return
}

func (r *Runner) runConversationBody(ctx context.Context, system string, history []openai.ChatCompletionMessageParamUnion, user openai.ChatCompletionMessageParamUnion, streamTo io.Writer) (string, []openai.ChatCompletionMessageParamUnion, bool, error) {
	r.StopHookActive = false
	r.toolUseSeq.Store(0)
	r.autoCheckAttempts = 0
	r.autoCheckLastSig = ""
	r.autoCheckExhausted = false
	r.turnMutated = false
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+16)
	sys := strings.TrimSpace(system)
	sysOffset := 0
	if sys != "" {
		msgs = append(msgs, openai.SystemMessage(sys))
		sysOffset = 1
	}
	msgs = append(msgs, history...)
	userText := UserMessageText(user)
	msgs = append(msgs, user)

	apiTools := r.Tools.OpenAITools()
	toolsOverhead := 0
	if len(apiTools) > 0 {
		b, _ := json.Marshal(apiTools)
		toolsOverhead = tokenest.Estimate(string(b))
	}
	llmRound := 0
	streamedFinal := false
	consecutiveToolFails := 0
	finalReplyNudgeSent := false
	toolIntentNudgeSent := false
	const maxConsecutiveToolFails = 3
	var turnTools []string

	for {
		if r.MaxTurns > 0 && llmRound >= r.MaxTurns {
			return "", nil, false, fmt.Errorf("%w: limit %d LLM rounds", ErrMaxTurns, r.MaxTurns)
		}
		msgs = truncateHistory(msgs, sysOffset, r.Cfg.ContextWindowTokens, r.Cfg.ContextReserveTokens, toolsOverhead)
		params := openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(r.LLM.Model()),
			Messages: msgs,
		}
		toolsDisabled := consecutiveToolFails >= maxConsecutiveToolFails
		if len(apiTools) > 0 && !toolsDisabled {
			params.Tools = apiTools
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String("auto"),
			}
			params.ParallelToolCalls = openai.Bool(true)
		}

		if r.OnWorkingChange != nil {
			r.OnWorkingChange(true)
		}
		t0 := time.Now()
		res, wasStreamed, err := r.callLLMWithRetry(ctx, params, streamTo)
		if r.OnWorkingChange != nil {
			r.OnWorkingChange(false)
		}
		if wasStreamed {
			streamedFinal = true
		}
		llmRound++
		llmDur := time.Since(t0)
		var logU *agentlog.TokenUsage
		if res != nil && err == nil {
			if r.Tracker != nil {
				r.Tracker.Add(usageFromCompletionUsage(res.Usage))
			}
			if r.MaxCostUSD > 0 && r.EstimateSessionCost != nil && r.Tracker != nil {
				cost, ok := r.EstimateSessionCost(r.Tracker.Session())
				if ok && cost > r.MaxCostUSD {
					return "", nil, false, fmt.Errorf("%w: estimated %g USD exceeds limit %g USD", ErrMaxCost, cost, r.MaxCostUSD)
				}
			}
			logU = logUsageFromCompletion(res.Usage)
		}
		if r.Log != nil {
			n := 0
			if res != nil {
				n = len(res.Choices)
			}
			r.Log.LLM(llmRound, r.LLM.Model(), llmDur, err, n, logU)
		}
		if err != nil {
			r.emitProgress(&TranscriptEvent{
				Kind:        TranscriptModelError,
				Text:        progressErrShort(err),
				RoundLLMDur: llmDur,
			})
			return "", nil, false, err
		}
		if len(res.Choices) == 0 {
			return "", nil, false, fmt.Errorf("empty choices from model")
		}

		msg := res.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			toolCallSource := assistantTextToolCallSource(msg)
			// Check for XML-style tool calls embedded in text (e.g. Qwen3-coder).
			// Skip if we've already hit the consecutive failure limit — the model
			// is stuck and parsing more text tool calls will loop forever.
			if toolCallSource != "" && consecutiveToolFails < maxConsecutiveToolFails && containsTextToolCalls(toolCallSource) {
				if parsed := parseTextToolCalls(toolCallSource); len(parsed) > 0 {
					if r.OnIntent != nil {
						intent := strings.TrimSpace(stripTextToolCallFragments(toolCallSource))
						if intent != "" {
							r.OnIntent(intent)
						}
					}
					assistantHistoryText := strings.TrimSpace(msg.Content)
					if assistantHistoryText == "" {
						assistantHistoryText = strings.TrimSpace(stripTextToolCallFragments(toolCallSource))
					}
					if assistantHistoryText != "" {
						msgs = append(msgs, openai.AssistantMessage(assistantHistoryText))
					}

					if line := FormatThinkingProgressLine(r.ProgressPlain, r.ProgressMode, toolCallSource); line != "" {
						r.emitProgress(&TranscriptEvent{
							Kind:           TranscriptAssistantPreface,
							AssistantProse: toolCallSource,
							ThinkingFull:   FormatFullThinkingProse(toolCallSource),
						})
					}

					type toolResult struct {
						name     string
						content  string
						progress string
					}
					results := make([]toolResult, len(parsed))
					for _, tc := range parsed {
						args := textToolCallArgsJSON(tc.Args)
						r.emitProgress(&TranscriptEvent{Kind: TranscriptToolIntent, ToolName: tc.Name, ToolArgs: args})
					}
					var wg sync.WaitGroup
					for i, tc := range parsed {
						// Multi-step planning: check for wait_for_previous flag in arguments.
						args := textToolCallArgsJSON(tc.Args)
						var argsMap map[string]json.RawMessage
						if err := json.Unmarshal(args, &argsMap); err == nil {
							if wfpRaw, ok := argsMap["wait_for_previous"]; ok {
								var wfp bool
								if err := json.Unmarshal(wfpRaw, &wfp); err == nil && wfp {
									wg.Wait()
								}
								delete(argsMap, "wait_for_previous")
								if cleaned, err := json.Marshal(argsMap); err == nil {
									args = json.RawMessage(cleaned)
								}
							}
						}

						wg.Add(1)
						go func(idx int, tc textToolCall, cleanedArgs json.RawMessage) {
							defer wg.Done()
							args := cleanedArgs
							if r.Log != nil {
								r.Log.ToolStart(tc.Name, agentlog.SummarizeArgs(tc.Name, args))
							}
							t1 := time.Now()
							toolID := fmt.Sprintf("tu-%d", r.toolUseSeq.Add(1))
							out, toolErr := r.runOneTool(ctx, tc.Name, args, toolID, toolID)
							toolDur := time.Since(t1)
							if r.Log != nil {
								r.Log.ToolEnd(tc.Name, toolDur, toolErr, nil)
							}
							compact := progressToolCompact(tc.Name, args)
							var prog string
							if toolErr != nil {
								prog = fmt.Sprintf("%s ✗ %s", compact, progressErrShort(toolErr))
							} else {
								prog = compact + " " + formatProgressDur(toolDur)
							}
							content := out
							if toolErr != nil {
								content = fmt.Sprintf("error: %v", toolErr)
							}
							results[idx] = toolResult{name: tc.Name, content: content, progress: prog}
						}(i, tc, args)
					}
					wg.Wait()

					for _, res := range results {
						turnTools = append(turnTools, res.name)
					}

					toolParts := make([]string, 0, len(results))
					allFailed := true
					var resultBuf strings.Builder
					resultBuf.WriteString("[tool results]\n")
					for _, res := range results {
						fmt.Fprintf(&resultBuf, "# %s\n%s\n\n", res.name, res.content)
						toolParts = append(toolParts, res.progress)
						if !strings.HasPrefix(res.content, "error: ") {
							allFailed = false
						}
					}
					acIn := make([]autoCheckInput, len(results))
					for i, res := range results {
						acIn[i] = autoCheckInput{name: res.name, content: res.content}
					}
					if hasSuccessfulMutation(acIn) {
						r.turnMutated = true
					}
					inject, prog := r.autoCheckAfterMutations(ctx, acIn)
					if inject != "" {
						fmt.Fprintf(&resultBuf, "\n%s\n", inject)
					}
					if prog != "" {
						r.emitProgress(&TranscriptEvent{Kind: TranscriptAutoCheck, Text: prog})
					}
					reflInject, reflProg := r.reflectAfterMutations(ctx, acIn, userText, turnTools)
					if reflInject != "" {
						fmt.Fprintf(&resultBuf, "\n%s\n", reflInject)
					}
					if reflProg != "" {
						r.emitProgress(&TranscriptEvent{Kind: TranscriptStatus, Text: reflProg})
					}
					msgs = append(msgs, openai.UserMessage(resultBuf.String()))
					for _, res := range results {
						r.emitProgress(&TranscriptEvent{
							Kind: TranscriptToolResult,
							Text: ProgressNestedIndent + res.progress,
						})
					}
					if allFailed {
						consecutiveToolFails++
					} else {
						consecutiveToolFails = 0
					}
					if len(toolParts) > 0 {
						suf := roundUsageSuffix(res, r.Cfg.ContextWindowTokens)
						r.emitProgress(&TranscriptEvent{
							Kind:           TranscriptRoundSummary,
							RoundLLMDur:    llmDur,
							RoundToolParts: append([]string(nil), toolParts...),
							RoundUsageSuf:  suf,
						})
					}
					if consecutiveToolFails >= maxConsecutiveToolFails {
						r.emitProgress(&TranscriptEvent{
							Kind: TranscriptWarning,
							Text: fmt.Sprintf("%d consecutive tool failures — requesting text reply", consecutiveToolFails),
						})
					}
					continue
				}
			}

			suf := roundUsageSuffix(res, r.Cfg.ContextWindowTokens)
			r.emitProgress(&TranscriptEvent{
				Kind:           TranscriptRoundSummary,
				RoundLLMDur:    llmDur,
				RoundToolParts: nil,
				RoundUsageSuf:  suf,
			})
			replyContent := msg.Content
			if replyContent == "" && toolCallSource != "" && !containsTextToolCalls(toolCallSource) {
				replyContent = strings.TrimSpace(toolCallSource)
			}
			if replyContent != "" {
				content := replyContent
				if containsTextToolCalls(content) {
					content = stripTextToolCallFragments(content)
				}
				// Local models sometimes emit planning prose with finish_reason=stop and no tool_calls.
				// Nudge once so the next completion can emit native tool_calls (and/or XML tools).
				if len(apiTools) > 0 && !toolsDisabled && len(turnTools) == 0 && !toolIntentNudgeSent &&
					shouldNudgeIncompleteToolIntent(content) {
					toolIntentNudgeSent = true
					msgs = append(msgs, openai.AssistantMessage(content))
					msgs = append(msgs, openai.UserMessage(toolIntentContinueMessage))
					r.emitProgress(&TranscriptEvent{Kind: TranscriptStatus, Text: "requesting tool calls…"})
					continue
				}
				if r.PostReplyCheck != nil {
					if inject := r.PostReplyCheck(ctx, PostReplyCheckInfo{
						Reply:              content,
						User:               userText,
						TurnTools:          turnTools,
						Mutated:            r.turnMutated,
						AutoCheckExhausted: r.autoCheckExhausted,
					}); inject != "" {
						r.PostReplyCheck = nil
						msgs = append(msgs, openai.AssistantMessage(content))
						msgs = append(msgs, openai.UserMessage(inject))
						r.emitProgress(&TranscriptEvent{Kind: TranscriptStatus, Text: "verifying suggestions…"})
						continue
					}
				}
				if r.Hooks != nil {
					sr, err := r.Hooks.RunStop(ctx, content, r.StopHookActive)
					if err != nil {
						return "", nil, false, err
					}
					if strings.TrimSpace(sr.SystemMessage) != "" {
						r.emitProgress(&TranscriptEvent{Kind: TranscriptPlain, Text: sr.SystemMessage})
					}
					if sr.Continue {
						msgs = append(msgs, openai.AssistantMessage(content))
						msgs = append(msgs, openai.UserMessage(sr.ContinuationPrompt))
						r.StopHookActive = true
						continue
					}
				}
				r.StopHookActive = false
				msgs = append(msgs, openai.AssistantMessage(content))
				newHist := msgs[sysOffset:]
				return content, newHist, streamedFinal, nil
			}
			if strings.TrimSpace(msg.Refusal) != "" {
				return "", nil, false, fmt.Errorf("model refusal: %s", strings.TrimSpace(msg.Refusal))
			}
			if !finalReplyNudgeSent {
				finalReplyNudgeSent = true
				msgs = append(msgs, openai.UserMessage("Please provide a brief final response to the user now. Do not call tools."))
				r.emitProgress(&TranscriptEvent{Kind: TranscriptStatus, Text: "requesting final response text…"})
				continue
			}
			fr := string(res.Choices[0].FinishReason)
			return "", nil, false, fmt.Errorf("model returned no content and no tool calls (finish_reason=%s)", fr)
		}

		if r.OnIntent != nil {
			intent := strings.TrimSpace(msg.Content)
			if intent == "" {
				intent = strings.TrimSpace(assistantTextToolCallSource(msg))
				intent = stripTextToolCallFragments(intent)
			}
			if intent != "" {
				r.OnIntent(intent)
			}
		}
		msgs = append(msgs, msg.ToParam())

		type toolResult struct {
			id       string
			name     string
			content  string
			progress string
		}
		calls := make([]openai.ChatCompletionMessageFunctionToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			v, ok := tc.AsAny().(openai.ChatCompletionMessageFunctionToolCall)
			if !ok {
				return "", nil, false, fmt.Errorf("unsupported tool call variant")
			}
			calls = append(calls, v)
		}

		if strings.TrimSpace(msg.Content) != "" {
			if line := FormatThinkingProgressLine(r.ProgressPlain, r.ProgressMode, msg.Content); line != "" {
				r.emitProgress(&TranscriptEvent{
					Kind:           TranscriptAssistantPreface,
					AssistantProse: msg.Content,
					ThinkingFull:   FormatFullThinkingProse(msg.Content),
				})
			}
		}

		results := make([]toolResult, len(calls))
		for _, v := range calls {
			args := json.RawMessage(v.Function.Arguments)
			r.emitProgress(&TranscriptEvent{Kind: TranscriptToolIntent, ToolName: v.Function.Name, ToolArgs: args})
		}
		var wg sync.WaitGroup
		for i, v := range calls {
			// Multi-step planning: check for wait_for_previous flag in arguments.
			args := json.RawMessage(v.Function.Arguments)
			var argsMap map[string]json.RawMessage
			if err := json.Unmarshal(args, &argsMap); err == nil {
				if wfpRaw, ok := argsMap["wait_for_previous"]; ok {
					var wfp bool
					if err := json.Unmarshal(wfpRaw, &wfp); err == nil && wfp {
						wg.Wait()
					}
					delete(argsMap, "wait_for_previous")
					if cleaned, err := json.Marshal(argsMap); err == nil {
						args = json.RawMessage(cleaned)
					}
				}
			}

			wg.Add(1)
			go func(idx int, v openai.ChatCompletionMessageFunctionToolCall, cleanedArgs json.RawMessage) {
				defer wg.Done()
				args := cleanedArgs
				if r.Log != nil {
					r.Log.ToolStart(v.Function.Name, agentlog.SummarizeArgs(v.Function.Name, args))
				}
				t1 := time.Now()
				toolID := fmt.Sprintf("tu-%d", r.toolUseSeq.Add(1))
				openaiID := v.ID
				if strings.TrimSpace(openaiID) == "" {
					openaiID = toolID
				}
				out, toolErr := r.runOneTool(ctx, v.Function.Name, args, toolID, openaiID)
				toolDur := time.Since(t1)
				summary := map[string]any{}
				if v.Function.Name == "run_command" || v.Function.Name == "run_shell" {
					if ec := parseExitCodeFromRunOutput(out); ec != "" {
						summary["exit_code"] = ec
					}
				}
				if r.Log != nil {
					r.Log.ToolEnd(v.Function.Name, toolDur, toolErr, summary)
				}
				compact := progressToolCompact(v.Function.Name, args)
				var prog string
				if toolErr != nil {
					prog = fmt.Sprintf("%s ✗ %s", compact, progressErrShort(toolErr))
				} else {
					prog = compact + " " + formatProgressDur(toolDur)
					if v.Function.Name == "run_command" || v.Function.Name == "run_shell" {
						if ec := parseExitCodeFromRunOutput(out); ec != "" {
							prog += " exit=" + ec
						}
					}
				}
				content := out
				if toolErr != nil {
					content = fmt.Sprintf("error: %v", toolErr)
				}
				results[idx] = toolResult{id: v.ID, name: v.Function.Name, content: content, progress: prog}
			}(i, v, args)
		}
		wg.Wait()

		for _, v := range calls {
			turnTools = append(turnTools, v.Function.Name)
		}

		toolParts := make([]string, 0, len(results))
		allFailed := true
		for _, res := range results {
			msgs = append(msgs, openai.ToolMessage(res.content, res.id))
			toolParts = append(toolParts, res.progress)
			if !strings.HasPrefix(res.content, "error: ") {
				allFailed = false
			}
		}
		acIn := make([]autoCheckInput, len(results))
		for i, res := range results {
			acIn[i] = autoCheckInput{name: res.name, content: res.content}
		}
		if hasSuccessfulMutation(acIn) {
			r.turnMutated = true
		}
		inject, prog := r.autoCheckAfterMutations(ctx, acIn)
		if inject != "" {
			msgs = append(msgs, openai.UserMessage(inject))
		}
		if prog != "" {
			r.emitProgress(&TranscriptEvent{Kind: TranscriptAutoCheck, Text: prog})
		}
		reflInject, reflProg := r.reflectAfterMutations(ctx, acIn, userText, turnTools)
		if reflInject != "" {
			msgs = append(msgs, openai.UserMessage(reflInject))
		}
		if reflProg != "" {
			r.emitProgress(&TranscriptEvent{Kind: TranscriptStatus, Text: reflProg})
		}
		for _, res := range results {
			r.emitProgress(&TranscriptEvent{
				Kind: TranscriptToolResult,
				Text: ProgressNestedIndent + res.progress,
			})
		}
		if allFailed {
			consecutiveToolFails++
		} else {
			consecutiveToolFails = 0
		}
		if len(toolParts) > 0 {
			suf := roundUsageSuffix(res, r.Cfg.ContextWindowTokens)
			r.emitProgress(&TranscriptEvent{
				Kind:           TranscriptRoundSummary,
				RoundLLMDur:    llmDur,
				RoundToolParts: append([]string(nil), toolParts...),
				RoundUsageSuf:  suf,
			})
		}
		if consecutiveToolFails >= maxConsecutiveToolFails {
			r.emitProgress(&TranscriptEvent{
				Kind: TranscriptWarning,
				Text: fmt.Sprintf("%d consecutive tool failures — requesting text reply", consecutiveToolFails),
			})
		}
	}
}

func assistantTextToolCallSource(msg openai.ChatCompletionMessage) string {
	if strings.TrimSpace(msg.Content) != "" {
		return msg.Content
	}
	raw := strings.TrimSpace(msg.RawJSON())
	if raw == "" {
		return ""
	}
	var payload struct {
		ReasoningContent string `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return payload.ReasoningContent
}

type autoCheckInput struct {
	name    string
	content string
}

func (r *Runner) runOneTool(ctx context.Context, name string, args json.RawMessage, toolUseID, displayToolCallID string) (display string, underlyingErr error) {
	dispID := strings.TrimSpace(displayToolCallID)
	if dispID == "" {
		dispID = toolUseID
	}
	ctx = acpctx.WithToolCallID(ctx, dispID)
	if r.OnToolBefore != nil {
		r.OnToolBefore(ctx, dispID, name, args)
	}
	if r.OnToolAfter != nil {
		defer func() { r.OnToolAfter(ctx, dispID, name, args, display, underlyingErr) }()
	}
	if r.Hooks != nil {
		pre, herr := r.Hooks.RunPreToolUse(ctx, name, args, toolUseID)
		if herr != nil {
			return fmt.Sprintf("error: hook: %v", herr), nil
		}
		if !pre.Allow {
			if strings.TrimSpace(pre.SystemMessage) != "" {
				r.emitProgress(&TranscriptEvent{Kind: TranscriptPlain, Text: pre.SystemMessage})
			}
			return fmt.Sprintf("error: blocked by hook: %s", pre.BlockReason), nil
		}
		if strings.TrimSpace(pre.SystemMessage) != "" {
			r.emitProgress(&TranscriptEvent{Kind: TranscriptPlain, Text: pre.SystemMessage})
		}
	}
	out, toolErr := r.Tools.Run(ctx, name, args)
	underlyingErr = toolErr
	display = out
	if toolErr != nil {
		display = fmt.Sprintf("error: %v", toolErr)
	}
	if r.Hooks != nil {
		post, herr := r.Hooks.RunPostToolUse(ctx, name, args, toolUseID, out, toolErr)
		if herr == nil {
			if strings.TrimSpace(post.SystemMessage) != "" {
				r.emitProgress(&TranscriptEvent{Kind: TranscriptPlain, Text: post.SystemMessage})
			}
			if strings.TrimSpace(post.AdditionalContext) != "" {
				display = display + "\n\n[hook context]\n" + post.AdditionalContext
			}
		}
	}
	return display, underlyingErr
}

func (r *Runner) autoCheckAfterMutations(ctx context.Context, results []autoCheckInput) (inject string, progress string) {
	if r.AutoCheck == nil {
		return "", ""
	}
	if !hasSuccessfulMutation(results) {
		return "", ""
	}
	out := r.AutoCheck(ctx)

	if out.Passed || out.Inject == "" {
		r.autoCheckAttempts = 0
		r.autoCheckLastSig = ""
		r.autoCheckExhausted = false
		return out.Inject, out.Progress
	}

	if r.autoCheckExhausted {
		return "", out.Progress
	}

	maxFixes := r.AutoCheckMaxFixes
	if maxFixes <= 0 {
		return out.Inject, out.Progress
	}

	if r.AutoCheckStopOnNoProgress && r.autoCheckLastSig != "" && out.Signature == r.autoCheckLastSig {
		r.autoCheckExhausted = true
		r.emitProgress(&TranscriptEvent{
			Kind: TranscriptWarning,
			Text: fmt.Sprintf("auto-check fix loop: no progress on %s (same failure signature) — stopping", out.FailingStep),
		})
		notice := fmt.Sprintf("[auto-check] fix loop stopped: identical %s failures after %d attempt(s) — no progress. "+
			"Report what is still failing and stop editing files.", out.FailingStep, r.autoCheckAttempts)
		return notice, out.Progress
	}

	if r.autoCheckAttempts >= maxFixes {
		r.autoCheckExhausted = true
		r.emitProgress(&TranscriptEvent{
			Kind: TranscriptWarning,
			Text: fmt.Sprintf("auto-check fix loop: max retries (%d) exhausted — stopping", maxFixes),
		})
		notice := fmt.Sprintf("[auto-check] fix loop stopped: max retries (%d) exhausted. "+
			"Report what is still failing and stop editing files.\n\nLast failure:\n%s", maxFixes, out.Inject)
		r.autoCheckLastSig = out.Signature
		return notice, out.Progress
	}

	r.autoCheckAttempts++
	r.autoCheckLastSig = out.Signature
	decorated := fmt.Sprintf("%s\n\n[auto-check fix attempt %d/%d]", out.Inject, r.autoCheckAttempts, maxFixes)
	return decorated, out.Progress
}

func (r *Runner) reflectAfterMutations(ctx context.Context, results []autoCheckInput, user string, turnTools []string) (inject string, progress string) {
	if r.PlanReflection == nil || !hasSuccessfulMutation(results) {
		return "", ""
	}
	out := make([]ToolBatchResult, 0, len(results))
	for _, res := range results {
		out = append(out, ToolBatchResult{Name: res.name, Content: res.content})
	}
	refl := r.PlanReflection(ctx, PlanReflectionInfo{
		User:      user,
		TurnTools: append([]string(nil), turnTools...),
		Results:   out,
	})
	return refl.Inject, refl.Progress
}

func hasSuccessfulMutation(results []autoCheckInput) bool {
	for _, res := range results {
		if _, ok := mutatingTools[res.name]; ok && !strings.HasPrefix(res.content, "error: ") {
			return true
		}
	}
	return false
}

func usageFromCompletionUsage(u openai.CompletionUsage) tokentracker.Usage {
	return tokentracker.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func logUsageFromCompletion(u openai.CompletionUsage) *agentlog.TokenUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return nil
	}
	return &agentlog.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func roundUsageSuffix(res *openai.ChatCompletion, contextWindow int) string {
	if res == nil {
		return ""
	}
	u := usageFromCompletionUsage(res.Usage)
	if !u.HasAny() {
		return ""
	}
	return " · " + tokentracker.FormatLineCtx(u, contextWindow)
}

func parseExitCodeFromRunOutput(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "exit_code:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "exit_code:"))
		}
	}
	return ""
}
