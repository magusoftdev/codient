package subagent

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/agent"
	"codient/internal/agentlog"
	"codient/internal/config"
	"codient/internal/prompt"
	"codient/internal/tools"
)

type stubLLM struct{}

func (stubLLM) Model() string { return "stub" }

func (stubLLM) ChatCompletion(context.Context, openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return nil, context.Canceled
}

func TestNewRunnerFromParams_SetsAutoCheck(t *testing.T) {
	reg := tools.NewRegistry()
	log := agentlog.New(ioDiscard{})
	cfg := &config.Config{}
	var ran bool
	ac := func(context.Context) agent.AutoCheckOutcome {
		ran = true
		return agent.AutoCheckOutcome{}
	}
	p := RunParams{
		Cfg:       cfg,
		Mode:      prompt.ModeBuild,
		AutoCheck: ac,
	}
	r := newRunnerFromParams(stubLLM{}, p, reg, log)
	if r.AutoCheck == nil {
		t.Fatal("expected AutoCheck to be set")
	}
	_ = r.AutoCheck(context.Background())
	if !ran {
		t.Fatal("expected AutoCheck to be the provided function")
	}
}

func TestNewRunnerFromParams_OmitsAutoCheckWhenNil(t *testing.T) {
	reg := tools.NewRegistry()
	log := agentlog.New(ioDiscard{})
	cfg := &config.Config{}
	p := RunParams{Cfg: cfg, Mode: prompt.ModeAsk}
	r := newRunnerFromParams(stubLLM{}, p, reg, log)
	if r.AutoCheck != nil {
		t.Fatal("expected AutoCheck to be nil")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
