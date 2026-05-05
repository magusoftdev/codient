package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/assistout"
	"codient/internal/config"
	"codient/internal/tools"
)

func TestPlanTotHeuristicMet(t *testing.T) {
	if !PlanTotHeuristicMet(1, "") {
		t.Fatal("first turn should match")
	}
	if PlanTotHeuristicMet(2, "") {
		t.Fatal("second turn without wait should not match")
	}
	waiting := "## Question\n\nPick?\n\n**Waiting for your answer**\n"
	if !PlanTotHeuristicMet(2, assistout.PrepareAssistantText(waiting, true)) {
		t.Fatal("after blocking question should match")
	}
}

func TestNewPlanTotOpenAIClient_MinConcurrent4(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		BaseURL:       "http://example/v1",
		APIKey:        "k",
		Model:         "m",
		MaxConcurrent: 1,
	}
	c := NewPlanTotOpenAIClient(cfg)
	if c == nil {
		t.Fatal("nil client")
	}
	// Semaphore capacity is not exported; client must at least construct.
	_ = c.Model()
}

func TestMergePlanTotHistory(t *testing.T) {
	h := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("prior")}
	u := openai.UserMessage("go")
	out := mergePlanTotHistory(h, u, "assistant text")
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
}

func messagesBlob(p openai.ChatCompletionNewParams) string {
	b, err := json.Marshal(p.Messages)
	if err != nil {
		return ""
	}
	return string(b)
}

// planTotMock routes completions by serialized messages (branch system suffix vs evaluator system).
type planTotMock struct {
	failReadabilityBranch bool
}

func (planTotMock) Model() string { return "mock" }

func (m planTotMock) ChatCompletion(ctx context.Context, p openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blob := messagesBlob(p)
	switch {
	case strings.Contains(blob, "Senior Principal Engineer"):
		return &openai.ChatCompletion{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "PLAN-A-BODY\n\nFirst justification sentence.\nSecond justification sentence."},
			}},
			Usage: openai.CompletionUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		}, nil
	case m.failReadabilityBranch && strings.Contains(blob, "readability"):
		return nil, errors.New("simulated branch failure")
	case strings.Contains(blob, "performance"):
		return &openai.ChatCompletion{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "PLAN-A-BODY"},
			}},
			Usage: openai.CompletionUsage{TotalTokens: 1},
		}, nil
	case strings.Contains(blob, "readability"):
		return &openai.ChatCompletion{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "PLAN-B-BODY"},
			}},
			Usage: openai.CompletionUsage{TotalTokens: 1},
		}, nil
	case strings.Contains(blob, "idiomatic"):
		return &openai.ChatCompletion{
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: "assistant", Content: "PLAN-C-BODY"},
			}},
			Usage: openai.CompletionUsage{TotalTokens: 1},
		}, nil
	default:
		snippet := blob
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		return nil, errors.New("unexpected plan_tot mock request: " + snippet)
	}
}

func (m planTotMock) StreamChatCompletion(ctx context.Context, p openai.ChatCompletionNewParams, w io.Writer) (*openai.ChatCompletion, error) {
	res, err := m.ChatCompletion(ctx, p)
	if err != nil || res == nil || len(res.Choices) == 0 {
		return res, err
	}
	if w != nil {
		_, _ = io.WriteString(w, res.Choices[0].Message.Content)
	}
	return res, nil
}

func TestRunPlanModeTot_Success(t *testing.T) {
	t.Parallel()
	base := &Runner{
		LLM:   &mockLLM{model: "x", script: []string{}},
		Cfg:   &config.Config{ContextWindowTokens: 32000, ContextReserveTokens: 1024},
		Tools: tools.NewRegistry(),
	}
	reply, hist, streamed, used, err := RunPlanModeTot(context.Background(), base, planTotMock{}, "base system", nil, openai.UserMessage("design X"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Fatal("expected ToT path used")
	}
	if !strings.Contains(reply, "PLAN-A-BODY") {
		t.Fatalf("reply=%q", reply)
	}
	if len(hist) != 2 {
		t.Fatalf("hist len=%d", len(hist))
	}
	if streamed {
		t.Fatal("expected non-streamed when streamTo nil")
	}
}

func TestRunPlanModeTot_BranchFailureFallsBackSignal(t *testing.T) {
	t.Parallel()
	base := &Runner{
		LLM:   &mockLLM{model: "x", script: []string{}},
		Cfg:   &config.Config{ContextWindowTokens: 32000, ContextReserveTokens: 1024},
		Tools: tools.NewRegistry(),
	}
	_, _, _, used, err := RunPlanModeTot(context.Background(), base, planTotMock{failReadabilityBranch: true}, "base system", nil, openai.UserMessage("design X"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if used {
		t.Fatal("expected used=false when a branch fails")
	}
}

func TestBuildPlanTotEvaluatorUserMessage(t *testing.T) {
	s := buildPlanTotEvaluatorUserMessage("a", "b", "c")
	if !strings.Contains(s, "Option A") || !strings.Contains(s, "Option C") {
		t.Fatalf("bad format: %s", s)
	}
}
