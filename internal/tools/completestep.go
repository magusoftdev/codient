package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

// CompleteStepRequest is the model-facing payload for complete_step.
type CompleteStepRequest struct {
	StepID  string `json:"step_id"`
	Outcome string `json:"outcome"` // done | skipped
	Note    string `json:"note"`
}

// StepCompleter persists structured plan progress.
type StepCompleter func(req CompleteStepRequest) (string, error)

// RegisterCompleteStep adds complete_step when a structured plan is actively executing.
func RegisterCompleteStep(reg *Registry, completer StepCompleter) {
	if completer == nil {
		return
	}
	reg.Register(Tool{
		Name:        "complete_step",
		Description: "Mark one structured plan step as done or skipped after you finish or intentionally skip it. Use this for every active plan step; the host will not advance the plan until steps are completed.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"step_id": map[string]any{
					"type":        "string",
					"description": "The exact plan step id, e.g. step-2.",
				},
				"outcome": map[string]any{
					"type":        "string",
					"description": "done when implemented; skipped when the verified premise is wrong or impossible.",
					"enum":        []any{"done", "skipped"},
				},
				"note": map[string]any{
					"type":        "string",
					"description": "Optional concise evidence or reason, especially for skipped steps.",
				},
			},
			"required":             []string{"step_id", "outcome"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			_ = ctx
			var req CompleteStepRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return "", fmt.Errorf("complete_step: %w", err)
			}
			req.StepID = strings.TrimSpace(req.StepID)
			req.Outcome = strings.TrimSpace(strings.ToLower(req.Outcome))
			req.Note = strings.TrimSpace(req.Note)
			if req.StepID == "" {
				return "", fmt.Errorf("complete_step: step_id is required")
			}
			switch req.Outcome {
			case "done", "skipped":
			default:
				return "", fmt.Errorf("complete_step: invalid outcome %q", req.Outcome)
			}
			return completer(req)
		},
	})
}
