package codientcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/openai/openai-go/v3"

	"codient/internal/agent"
	"codient/internal/tokentracker"
)

// AutoApprovePolicy controls non-interactive approval for exec and fetch tools.
type AutoApprovePolicy int

const (
	// AutoApproveOff uses interactive prompts when possible; denies when stdin is not a TTY.
	AutoApproveOff AutoApprovePolicy = iota
	// AutoApproveExec allows run_command/run_shell without prompts when denied by allowlist.
	AutoApproveExec
	// AutoApproveFetch allows fetch_url to unknown hosts without prompts.
	AutoApproveFetch
	// AutoApproveAll allows both exec and fetch without prompts.
	AutoApproveAll
)

// ParseAutoApprove parses off|exec|fetch|all (case-insensitive).
func ParseAutoApprove(s string) (AutoApprovePolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off":
		return AutoApproveOff, nil
	case "exec":
		return AutoApproveExec, nil
	case "fetch":
		return AutoApproveFetch, nil
	case "all":
		return AutoApproveAll, nil
	default:
		return AutoApproveOff, fmt.Errorf("unknown auto-approve %q (want off, exec, fetch, or all)", s)
	}
}

// ParseOutputFormat parses text|json|stream-json (case-insensitive).
func ParseOutputFormat(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	case "stream-json":
		return "stream-json", nil
	default:
		return "", fmt.Errorf("unknown output-format %q (want text, json, or stream-json)", s)
	}
}

func (p AutoApprovePolicy) allowsExec() bool {
	return p == AutoApproveExec || p == AutoApproveAll
}

func (p AutoApprovePolicy) allowsFetch() bool {
	return p == AutoApproveFetch || p == AutoApproveAll
}

// headlessJSONOut is the single JSON object written for -output-format json.
type headlessJSONOut struct {
	Reply         string          `json:"reply"`
	ToolsUsed     []string        `json:"tools_used"`
	FilesModified []string        `json:"files_modified,omitempty"`
	Tokens        *headlessTokens `json:"tokens,omitempty"`
	CostUSD       *float64        `json:"cost_usd,omitempty"`
	ExitReason    string          `json:"exit_reason"`
	Error         string          `json:"error,omitempty"`
}

type headlessTokens struct {
	Prompt     int64 `json:"prompt,omitempty"`
	Completion int64 `json:"completion,omitempty"`
	Total      int64 `json:"total,omitempty"`
}

func writeHeadlessJSONResult(w io.Writer, reply string, tools, files []string, prompt, completion, total int64, cost *float64, err error) error {
	out := headlessJSONOut{
		Reply:         reply,
		ToolsUsed:     tools,
		FilesModified: files,
		ExitReason:    "complete",
	}
	if prompt > 0 || completion > 0 || total > 0 {
		out.Tokens = &headlessTokens{Prompt: prompt, Completion: completion, Total: total}
	}
	if cost != nil {
		out.CostUSD = cost
	}
	if err != nil {
		out.ExitReason = exitReasonForError(err)
		out.Error = err.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func exitReasonForError(err error) string {
	if err == nil {
		return "complete"
	}
	if errors.Is(err, agent.ErrMaxTurns) {
		return "max_turns"
	}
	if errors.Is(err, agent.ErrMaxCost) {
		return "max_cost"
	}
	return "error"
}

// summarizeToolsAndFilesFromHistory inspects OpenAI message params from one user turn
// (assistant tool_calls) for headless JSON summaries.
func summarizeToolsAndFilesFromHistory(hist []openai.ChatCompletionMessageParamUnion) (tools []string, files []string) {
	toolSet := map[string]struct{}{}
	fileSet := map[string]struct{}{}
	for _, m := range hist {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			continue
		}
		if tcs, ok := raw["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				tcm, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				fn, ok := tcm["function"].(map[string]any)
				if !ok {
					continue
				}
				name, _ := fn["name"].(string)
				if name != "" {
					toolSet[name] = struct{}{}
				}
				if argsStr, ok := fn["arguments"].(string); ok && argsStr != "" {
					addPathsFromToolJSON(name, argsStr, fileSet)
				}
			}
		}
	}
	return sortedStringKeys(toolSet), sortedStringKeys(fileSet)
}

func sortedStringKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func addPathsFromToolJSON(toolName, argsJSON string, fileSet map[string]struct{}) {
	var args map[string]any
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return
	}
	pathKeys := []string{"path", "from", "to", "under"}
	switch toolName {
	case "write_file", "read_file", "str_replace", "patch_file", "insert_lines", "remove_path", "path_stat", "glob_files":
		for _, k := range pathKeys {
			if p, ok := args[k].(string); ok && p != "" {
				fileSet[p] = struct{}{}
			}
		}
	case "move_path", "copy_path":
		if p, ok := args["from"].(string); ok && p != "" {
			fileSet[p] = struct{}{}
		}
		if p, ok := args["to"].(string); ok && p != "" {
			fileSet[p] = struct{}{}
		}
	default:
		// ignore
	}
}

// writeHeadlessStreamJSONFinal appends a single summary line after JSONL tool/llm events (stream-json mode).
func writeHeadlessStreamJSONFinal(w io.Writer, reply string, tools, files []string, u tokentracker.Usage, cost *float64, err error) error {
	m := map[string]any{
		"type":        "result",
		"reply":       reply,
		"exit_reason": exitReasonForError(err),
	}
	if len(tools) > 0 {
		m["tools_used"] = tools
	}
	if len(files) > 0 {
		m["files_modified"] = files
	}
	if u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 {
		m["tokens"] = map[string]any{
			"prompt":     u.PromptTokens,
			"completion": u.CompletionTokens,
			"total":      u.TotalTokens,
		}
	}
	if cost != nil {
		m["cost_usd"] = *cost
	}
	if err != nil {
		m["error"] = err.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(m)
}

// finishHeadlessTurn writes structured output for -print and returns an exit code.
func (s *session) finishHeadlessTurn(reply string, execErr error) int {
	u := tokentracker.Usage{}
	if s.tokenTracker != nil {
		u = s.tokenTracker.Session()
	}
	var costPtr *float64
	if execErr == nil {
		if c, ok := s.estimateCostForUsage(u); ok {
			v := c
			costPtr = &v
		}
	}
	tools, files := summarizeToolsAndFilesFromHistory(s.history)
	switch s.outputFormat {
	case "json":
		if err := writeHeadlessJSONResult(os.Stdout, reply, tools, files, u.PromptTokens, u.CompletionTokens, u.TotalTokens, costPtr, execErr); err != nil {
			fmt.Fprintf(os.Stderr, "codient: write output: %v\n", err)
			return 2
		}
	case "stream-json":
		if err := writeHeadlessStreamJSONFinal(os.Stdout, reply, tools, files, u, costPtr, execErr); err != nil {
			fmt.Fprintf(os.Stderr, "codient: write output: %v\n", err)
			return 2
		}
	case "text":
		if execErr != nil {
			fmt.Fprintf(os.Stderr, "agent: %v\n", execErr)
			return 1
		}
		return 0
	}
	if execErr != nil {
		return 1
	}
	return 0
}
