package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	"golang.org/x/sync/errgroup"

	"codient/internal/assistout"
	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/tokentracker"
)

// PlanTotClient is the LLM surface for plan ToT (branch runs use ChatCompletion; evaluator uses streaming or non-streaming completion without tools).
type PlanTotClient interface {
	ChatClient
	StreamChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams, w io.Writer) (*openai.ChatCompletion, error)
}

// PlanTotHeuristicMet is true for the first plan-mode user turn (userTurn==1) or when the
// previous assistant reply was a blocking plan Question (waiting for the user's answer).
func PlanTotHeuristicMet(userTurn int, lastAssistantReply string) bool {
	if userTurn == 1 {
		return true
	}
	return assistout.ReplySignalsPlanWait(lastAssistantReply)
}

// NewPlanTotOpenAIClient returns a client for plan-mode ToT fan-out with at least four
// concurrent in-flight requests so three branch runs can overlap with the evaluator.
func NewPlanTotOpenAIClient(cfg *config.Config) *openaiclient.Client {
	base, key, model := cfg.ConnectionForMode("plan")
	n := cfg.MaxConcurrent
	if n < 4 {
		n = 4
	}
	return openaiclient.NewFromParams(base, key, model, n)
}

var planTotBranchSuffixes = []string{
	"### Plan generation emphasis: performance\n\nPrioritize runtime efficiency, memory use, and algorithmic complexity. Prefer designs that reduce hot-path work and allocations where it matters for this task, without sacrificing correctness.",
	"### Plan generation emphasis: readability and maintainability\n\nPrioritize clear structure, straightforward control flow, and ease of future changes. Prefer explicit naming, small testable units, and documentation over clever shortcuts.",
	"### Plan generation emphasis: idiomatic Go architecture\n\nPrioritize conventional Go project layout, package boundaries, interfaces where they clarify design, context.Context propagation, and patterns that fit the standard library ecosystem.",
}

const planTotEvaluatorSystem = `You are a Senior Principal Engineer reviewing three candidate implementation plans for the same user request (Options A, B, and C).

Critically compare trade-offs: correctness, feasibility, alignment with the user's stated goals, operational risk, testability, and fit for a typical Go codebase worked on via the codient CLI.

Your response must contain ONLY:
1) The full text of the single winning plan — copy it verbatim from the chosen option (do not replace it with a summary).
2) Immediately after that plan, add exactly two sentences explaining why that option wins over the other two.

Do not add a preamble or title before the copied plan. Do not restate "Option A/B/C" as a section header unless those words already appear inside the copied plan.`

func buildPlanTotEvaluatorUserMessage(a, b, c string) string {
	var sb strings.Builder
	sb.WriteString("Three draft plans follow. Pick the best and follow the output rules in your system message.\n\n")
	sb.WriteString("## Option A\n\n")
	sb.WriteString(strings.TrimSpace(a))
	sb.WriteString("\n\n## Option B\n\n")
	sb.WriteString(strings.TrimSpace(b))
	sb.WriteString("\n\n## Option C\n\n")
	sb.WriteString(strings.TrimSpace(c))
	return sb.String()
}

func clonePlanTotBranchRunner(base *Runner, llm ChatClient) *Runner {
	return &Runner{
		LLM:                 llm,
		Cfg:                 base.Cfg,
		Tools:               base.Tools,
		Log:                 base.Log,
		ErrorLog:            base.ErrorLog,
		Tracker:             base.Tracker,
		Hooks:               base.Hooks,
		ProgressPlain:       base.ProgressPlain,
		ProgressMode:        base.ProgressMode,
		MaxTurns:            base.MaxTurns,
		MaxCostUSD:          base.MaxCostUSD,
		EstimateSessionCost: base.EstimateSessionCost,
		// No progress, streaming to stdout, or UI callbacks for parallel branches.
		Progress:          nil,
		OnWorkingChange:   nil,
		OnTranscriptEvent: nil,
		OnToolBefore:      nil,
		OnToolAfter:       nil,
		OnIntent:          nil,
		PostReplyCheck:    nil,
		AutoCheck:         nil,
	}
}

func completionAssistantText(res *openai.ChatCompletion) string {
	if res == nil || len(res.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(res.Choices[0].Message.Content)
}

func runPlanTotEvaluator(ctx context.Context, client PlanTotClient, tr *tokentracker.Tracker, user string, streamTo io.Writer) (string, error) {
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(client.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(planTotEvaluatorSystem), openai.UserMessage(user)},
	}
	var res *openai.ChatCompletion
	var err error
	if streamTo != nil {
		res, err = client.StreamChatCompletion(ctx, params, streamTo)
	} else {
		res, err = client.ChatCompletion(ctx, params)
	}
	if err != nil {
		return "", err
	}
	if tr != nil && res != nil {
		tr.Add(usageFromCompletionUsage(res.Usage))
	}
	out := completionAssistantText(res)
	if out == "" {
		return "", fmt.Errorf("evaluator returned empty content")
	}
	return out, nil
}

// RunPlanModeTot runs three parallel branch agent turns (same user message, diversified system suffix),
// then an evaluator completion. On failure it returns used=false and nil error so the caller can fall
// back to a normal RunConversation. Returns ctx.Err() if the context is canceled.
func RunPlanModeTot(ctx context.Context, base *Runner, totClient PlanTotClient, systemPrompt string, history []openai.ChatCompletionMessageParamUnion, user openai.ChatCompletionMessageParamUnion, streamTo io.Writer) (reply string, newHist []openai.ChatCompletionMessageParamUnion, streamed bool, used bool, err error) {
	if totClient == nil || base == nil {
		return "", nil, false, false, nil
	}
	baseSys := strings.TrimSpace(systemPrompt)
	if baseSys == "" {
		return "", nil, false, false, nil
	}

	branchLLM := ChatClient(totClient)
	type branchOutcome struct {
		idx  int
		text string
		err  error
	}
	ch := make(chan branchOutcome, 3)

	g, ctx := errgroup.WithContext(ctx)
	for i := range 3 {
		i := i
		g.Go(func() error {
			if ctx.Err() != nil {
				ch <- branchOutcome{idx: i, err: ctx.Err()}
				return nil
			}
			sys := baseSys + "\n\n" + planTotBranchSuffixes[i]
			br := clonePlanTotBranchRunner(base, branchLLM)
			text, _, _, rerr := br.RunConversation(ctx, sys, history, user, io.Discard)
			ch <- branchOutcome{idx: i, text: strings.TrimSpace(text), err: rerr}
			return nil
		})
	}
	if werr := g.Wait(); werr != nil {
		return "", nil, false, false, werr
	}

	outcomes := make([]branchOutcome, 3)
	for range 3 {
		o := <-ch
		outcomes[o.idx] = o
	}

	var plans [3]string
	for i := range 3 {
		if outcomes[i].err != nil || outcomes[i].text == "" {
			return "", nil, false, false, nil
		}
		plans[i] = outcomes[i].text
	}

	evalUser := buildPlanTotEvaluatorUserMessage(plans[0], plans[1], plans[2])
	evalText, evalErr := runPlanTotEvaluator(ctx, totClient, base.Tracker, evalUser, streamTo)
	if evalErr != nil || strings.TrimSpace(evalText) == "" {
		fallback := plans[0]
		if fallback == "" {
			return "", nil, false, false, nil
		}
		return fallback, mergePlanTotHistory(history, user, fallback), false, true, nil
	}
	reply = strings.TrimSpace(evalText)
	if base.MaxCostUSD > 0 && base.EstimateSessionCost != nil && base.Tracker != nil {
		cost, ok := base.EstimateSessionCost(base.Tracker.Session())
		if ok && cost > base.MaxCostUSD {
			return "", nil, false, false, fmt.Errorf("%w: estimated %g USD exceeds limit %g USD", ErrMaxCost, cost, base.MaxCostUSD)
		}
	}
	return reply, mergePlanTotHistory(history, user, reply), streamTo != nil, true, nil
}

func mergePlanTotHistory(history []openai.ChatCompletionMessageParamUnion, user openai.ChatCompletionMessageParamUnion, assistant string) []openai.ChatCompletionMessageParamUnion {
	out := append([]openai.ChatCompletionMessageParamUnion(nil), history...)
	out = append(out, user)
	out = append(out, openai.AssistantMessage(assistant))
	return out
}
