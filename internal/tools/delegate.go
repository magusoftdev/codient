package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

// DelegateRunner is the callback signature for executing a sub-agent.
// mode is "build"|"ask"|"plan"; task is the self-contained task description;
// extraContext is optional context from the parent.
type DelegateRunner func(ctx context.Context, mode, task, extraContext string) (string, error)

// RegisterDelegateTask adds the delegate_task tool to a parent agent's registry.
// parentMode controls which sub-agent modes are allowed (privilege escalation guard).
// runFn is injected by the session layer to avoid circular dependencies.
func RegisterDelegateTask(r *Registry, parentMode string, runFn DelegateRunner) {
	allowedModes := []string{"ask"}
	desc := "Delegate a self-contained research task to a read-only sub-agent that runs in ask mode. " +
		"The sub-agent gets a fresh context with workspace access (read/search only). " +
		"Use to parallelize codebase exploration across multiple areas simultaneously. " +
		"Returns the sub-agent's complete reply."
	if parentMode == "build" {
		allowedModes = []string{"build", "ask", "plan"}
		desc = "Delegate a self-contained task to a sub-agent running in the specified mode. " +
			"The sub-agent gets a fresh context with full workspace access matching its mode. " +
			"Use for parallelizable exploration (ask), independent code changes (build), or focused design (plan). " +
			"Returns the sub-agent's complete reply."
	}

	modeEnum := make([]any, len(allowedModes))
	for i, m := range allowedModes {
		modeEnum[i] = m
	}

	r.Register(Tool{
		Name:        "delegate_task",
		Description: desc,
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{
					"type":        "string",
					"enum":        modeEnum,
					"description": "Mode for the sub-agent: controls its tool set and system prompt.",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "Clear, self-contained task description. The sub-agent has no knowledge of the parent conversation.",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Optional context snippets (file contents, prior findings) to give the sub-agent. Omit if the task is fully self-contained.",
				},
			},
			"required":             []string{"mode", "task"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Mode    string `json:"mode"`
				Task    string `json:"task"`
				Context string `json:"context"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			p.Mode = strings.TrimSpace(strings.ToLower(p.Mode))
			if p.Mode == "" {
				return "", fmt.Errorf("mode is required")
			}
			if !slices.Contains(allowedModes, p.Mode) {
				return "", fmt.Errorf("mode %q not allowed; permitted modes: %s", p.Mode, strings.Join(allowedModes, ", "))
			}
			if strings.TrimSpace(p.Task) == "" {
				return "", fmt.Errorf("task is required")
			}
			return runFn(ctx, p.Mode, p.Task, p.Context)
		},
	})
}
