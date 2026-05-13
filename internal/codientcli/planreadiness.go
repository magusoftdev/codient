package codientcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/openaiclient"
	"codient/internal/planstore"
	"codient/internal/tokentracker"
)

type planReadinessResult struct {
	Ready   bool     `json:"ready"`
	Reason  string   `json:"reason"`
	Missing []string `json:"missing,omitempty"`
}

func evaluatePlanReadiness(ctx context.Context, client *openaiclient.Client, tracker *tokentracker.Tracker, plan *planstore.Plan, markdown string) planReadinessResult {
	pre := deterministicPlanReadiness(plan, markdown)
	if !pre.Ready {
		return pre
	}
	if client == nil || strings.TrimSpace(client.Model()) == "" {
		return pre
	}

	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(client.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(planReadinessSystemPrompt),
			openai.UserMessage(buildPlanReadinessUserMessage(plan, markdown)),
		},
		Temperature:         openai.Float(0),
		MaxCompletionTokens: openai.Int(160),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}
	res, err := client.ChatCompletion(ctx, params)
	if err != nil || res == nil || len(res.Choices) == 0 {
		return pre
	}
	if tracker != nil {
		tracker.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	var out planReadinessResult
	if err := json.Unmarshal([]byte(extractJSONBody(res.Choices[0].Message.Content)), &out); err != nil {
		return pre
	}
	if strings.TrimSpace(out.Reason) == "" {
		if out.Ready {
			out.Reason = "plan appears complete"
		} else {
			out.Reason = "plan is not ready"
		}
	}
	return out
}

func deterministicPlanReadiness(plan *planstore.Plan, markdown string) planReadinessResult {
	body := strings.ToLower(markdown)
	switch {
	case strings.Contains(body, "waiting for your answer"):
		return planNotReady("plan is waiting for a user answer", "remove blocking clarification")
	case strings.Contains(body, "## question") || strings.Contains(body, "### question"):
		return planNotReady("plan contains a blocking question", "answer or remove blocking question")
	case strings.Contains(body, "not ready to implement") || strings.Contains(body, "not yet ready to implement"):
		return planNotReady("plan explicitly says it is not ready", "produce a ready implementation plan")
	}
	if plan == nil {
		return planNotReady("no structured plan was parsed", "implementation steps")
	}
	if len(plan.Steps) == 0 {
		return planNotReady("plan has no implementation steps", "implementation steps")
	}
	if isFallbackOnlyPlan(plan) {
		return planNotReady("plan only contains the generic fallback step", "specific implementation steps")
	}
	if len(plan.Verification) == 0 {
		return planNotReady("plan has no verification guidance", "verification")
	}
	return planReadinessResult{Ready: true, Reason: "deterministic checks passed"}
}

func planNotReady(reason string, missing ...string) planReadinessResult {
	return planReadinessResult{Ready: false, Reason: reason, Missing: missing}
}

func isFallbackOnlyPlan(plan *planstore.Plan) bool {
	if plan == nil || len(plan.Steps) != 1 {
		return false
	}
	st := plan.Steps[0]
	return strings.EqualFold(strings.TrimSpace(st.ID), "step-1") &&
		strings.EqualFold(strings.TrimSpace(st.Title), "Implement plan") &&
		strings.Contains(strings.ToLower(st.Description), "execute the full plan")
}

const planReadinessSystemPrompt = `You evaluate whether a coding-agent implementation plan is ready to hand off to build mode.

Reply with ONLY JSON:
{"ready":true|false,"reason":"<=20 words","missing":["short item"]}

Ready requires concrete implementation steps, verification guidance, and no blocking user question. Reject generic or vague plans.`

func buildPlanReadinessUserMessage(plan *planstore.Plan, markdown string) string {
	var b strings.Builder
	if plan != nil {
		fmt.Fprintf(&b, "Summary: %s\n", compactIntentText(plan.Summary, 600))
		fmt.Fprintf(&b, "Steps:\n")
		for i, st := range plan.Steps {
			if i >= 12 {
				fmt.Fprintf(&b, "- ... %d more\n", len(plan.Steps)-i)
				break
			}
			fmt.Fprintf(&b, "- %s: %s", st.ID, st.Title)
			if st.Description != "" {
				fmt.Fprintf(&b, " — %s", compactIntentText(st.Description, 240))
			}
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Verification:\n")
		for _, v := range plan.Verification {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	md := strings.TrimSpace(markdown)
	if len(md) > 6000 {
		md = md[:6000] + "\n...[truncated]"
	}
	fmt.Fprintf(&b, "\nMarkdown:\n%s\n", md)
	return b.String()
}

func extractJSONBody(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}
