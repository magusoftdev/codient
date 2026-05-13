package codientcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/agent"
	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/planstore"
	"codient/internal/tokentracker"
)

type planReflectionDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func makePlanReflection(s *session) func(context.Context, agent.PlanReflectionInfo) agent.PlanReflectionOutcome {
	return func(ctx context.Context, info agent.PlanReflectionInfo) agent.PlanReflectionOutcome {
		if s == nil || s.currentPlan == nil || s.planPhase != planstore.PhaseExecuting || !s.cfg.PlanReflection {
			return agent.PlanReflectionOutcome{}
		}
		decision := evaluatePlanReflection(ctx, openaiclient.NewForTier(s.cfg, config.TierLow), s.tokenTracker, s.currentPlan, info)
		if decision.Action != "replan" {
			return agent.PlanReflectionOutcome{}
		}
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "plan no longer appears viable"
		}
		s.currentPlan.Phase = planstore.PhaseDraft
		s.planPhase = planstore.PhaseDraft
		planstore.IncrementRevision(s.currentPlan)
		if err := planstore.Save(s.currentPlan); err != nil {
			return agent.PlanReflectionOutcome{Progress: "plan reflection: replan requested but save failed"}
		}
		s.planActiveGroupLimited = false
		if s.cfg != nil {
			s.autoSave()
		}
		return agent.PlanReflectionOutcome{
			Progress: "plan reflection: replan requested",
			Inject:   fmt.Sprintf("[plan reflection] The current plan is no longer viable: %s\n\nStop implementing. Summarize what you found and what needs to change in the plan.", reason),
		}
	}
}

func evaluatePlanReflection(ctx context.Context, client *openaiclient.Client, tracker *tokentracker.Tracker, plan *planstore.Plan, info agent.PlanReflectionInfo) planReflectionDecision {
	if client == nil || plan == nil {
		return planReflectionDecision{Action: "continue"}
	}
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(client.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(planReflectionSystemPrompt),
			openai.UserMessage(buildPlanReflectionUserMessage(plan, info)),
		},
		Temperature:         openai.Float(0),
		MaxCompletionTokens: openai.Int(120),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}
	res, err := client.ChatCompletion(ctx, params)
	if err != nil || res == nil || len(res.Choices) == 0 {
		return planReflectionDecision{Action: "continue"}
	}
	if tracker != nil {
		tracker.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	var out planReflectionDecision
	if err := json.Unmarshal([]byte(extractJSONBody(res.Choices[0].Message.Content)), &out); err != nil {
		return planReflectionDecision{Action: "continue"}
	}
	out.Action = strings.ToLower(strings.TrimSpace(out.Action))
	if out.Action != "replan" {
		out.Action = "continue"
	}
	return out
}

const planReflectionSystemPrompt = `You decide if a structured implementation plan is still viable after a tool batch.

Reply with ONLY JSON:
{"action":"continue|replan","reason":"<=20 words"}

Choose replan only when tool results show the plan premise is invalid, required files/APIs do not exist, or the current plan cannot work as written. Otherwise choose continue.`

func buildPlanReflectionUserMessage(plan *planstore.Plan, info agent.PlanReflectionInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %s\n\n", summarizePlanForIntent(plan, plan.Phase))
	fmt.Fprintf(&b, "User request: %s\n\n", compactIntentText(info.User, 600))
	b.WriteString("Tool results:\n")
	for i, res := range info.Results {
		if i >= 8 {
			break
		}
		content := strings.TrimSpace(res.Content)
		if len(content) > 1200 {
			content = content[:1200] + "\n...[truncated]"
		}
		fmt.Fprintf(&b, "# %s\n%s\n\n", res.Name, content)
	}
	return b.String()
}
