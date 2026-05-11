// Package intent classifies a user prompt into one of four orchestrator categories
// (QUERY, DESIGN, SIMPLE_FIX, COMPLEX_TASK) by issuing a small JSON-only chat
// completion against the configured low-reasoning model.
//
// The supervisor is intentionally cheap: a terse system prompt, low temperature,
// and a tight token budget so high-end hardware (LM Studio / Ollama) returns the
// classification in well under a second.
//
// On any error or malformed response the classifier falls back to QUERY (the
// safest read-only path) so a degraded supervisor can never accidentally trigger
// a write.
package intent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"codient/internal/openaiclient"
	"codient/internal/tokentracker"
)

// Category is the orchestrator routing decision returned by IdentifyIntent.
type Category string

// Categories the supervisor may choose. Anything else is treated as malformed
// and triggers a fallback to CategoryQuery.
const (
	CategoryQuery       Category = "QUERY"
	CategoryDesign      Category = "DESIGN"
	CategorySimpleFix   Category = "SIMPLE_FIX"
	CategoryComplexTask Category = "COMPLEX_TASK"
)

// Identification is the structured supervisor reply.
type Identification struct {
	Category  Category `json:"category"`
	Reasoning string   `json:"reasoning"`
	// Fallback is true when the supervisor failed and the classifier substituted
	// a safe default. Reasoning explains why in that case.
	Fallback bool `json:"-"`
}

// Options tunes IdentifyIntent. All fields are optional.
type Options struct {
	// Tracker records token usage from the supervisor call when set.
	Tracker *tokentracker.Tracker
	// MaxReasoningChars truncates the user-visible reasoning string. 0 = 200.
	MaxReasoningChars int
}

// SupervisorSystemPrompt is exported so callers (and tests) can reference the
// exact text that drives the classification. Kept terse so the model spends its
// budget on the JSON answer rather than the schema explanation.
const SupervisorSystemPrompt = `You classify a coding-agent user prompt into ONE category. Reply with ONLY a JSON object, no prose, no code fences.

Schema:
{"category":"QUERY|DESIGN|SIMPLE_FIX|COMPLEX_TASK","reasoning":"<<=20 words>"}

Categories:
- QUERY: questions about the codebase or general info; no edits expected.
- DESIGN: architectural advice, design patterns, UI mockups, "how should I structure"; no code yet.
- SIMPLE_FIX: small localized change (1-2 files, typo, rename, add log, tweak config).
- COMPLEX_TASK: multi-file refactor, new feature, anything needing a plan first.

Pick the single best fit. Output JSON only.`

const supervisorMaxCompletionTokens = 80

// IdentifyIntent runs the supervisor classifier on userPrompt using client
// (which should be wired to the low-reasoning tier). Returns a populated
// Identification; never returns nil. The error is non-nil only when the
// classifier could not even contact the model AND the fallback path was
// applied — callers can usually log it and continue with the fallback.
func IdentifyIntent(ctx context.Context, client *openaiclient.Client, userPrompt string, opts Options) (Identification, error) {
	if client == nil {
		return fallback("supervisor: nil client"), errors.New("intent: nil client")
	}
	trimmed := strings.TrimSpace(userPrompt)
	if trimmed == "" {
		return fallback("supervisor: empty prompt"), errors.New("intent: empty prompt")
	}
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(client.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(SupervisorSystemPrompt),
			openai.UserMessage(trimmed),
		},
		Temperature:         openai.Float(0),
		MaxCompletionTokens: openai.Int(supervisorMaxCompletionTokens),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}
	res, err := client.ChatCompletion(ctx, params)
	if err != nil {
		return fallback(fmt.Sprintf("supervisor: chat error: %v", err)), err
	}
	if opts.Tracker != nil && res != nil {
		opts.Tracker.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	if res == nil || len(res.Choices) == 0 {
		return fallback("supervisor: empty response"), errors.New("intent: empty response")
	}
	body := res.Choices[0].Message.Content
	parsed, perr := parseSupervisorReply(body)
	if perr != nil {
		return fallback("supervisor: parse error: " + perr.Error()), nil
	}
	parsed.Reasoning = trimReasoning(parsed.Reasoning, opts.MaxReasoningChars)
	return parsed, nil
}

// parseSupervisorReply extracts and validates the supervisor JSON object. It
// tolerates leading/trailing whitespace, code fences, and trailing prose by
// scanning for the first balanced {...} block.
func parseSupervisorReply(raw string) (Identification, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return Identification{}, fmt.Errorf("no JSON object in reply")
	}
	var probe struct {
		Category  string `json:"category"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(body), &probe); err != nil {
		return Identification{}, err
	}
	cat, ok := normalizeCategory(probe.Category)
	if !ok {
		return Identification{}, fmt.Errorf("unknown category %q", probe.Category)
	}
	return Identification{Category: cat, Reasoning: strings.TrimSpace(probe.Reasoning)}, nil
}

// extractJSONObject returns the first balanced {...} substring from s, ignoring
// surrounding ``` fences, leading text, or trailing prose. Empty when none.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "```") {
		// Drop the first fence line (e.g. ```json) and any closing fence.
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inStr {
			switch c {
			case '\\':
				escape = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// normalizeCategory accepts case-insensitive variants and returns the canonical
// Category constant. The boolean is false when the input does not match one of
// the four supported categories.
func normalizeCategory(in string) (Category, bool) {
	switch strings.ToUpper(strings.TrimSpace(in)) {
	case "QUERY", "ASK", "QUESTION":
		return CategoryQuery, true
	case "DESIGN", "PLAN":
		return CategoryDesign, true
	case "SIMPLE_FIX", "SIMPLE-FIX", "SIMPLEFIX", "FIX":
		return CategorySimpleFix, true
	case "COMPLEX_TASK", "COMPLEX-TASK", "COMPLEX", "COMPLEXTASK", "REFACTOR":
		return CategoryComplexTask, true
	default:
		return "", false
	}
}

func trimReasoning(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		max = 200
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func fallback(reason string) Identification {
	return Identification{Category: CategoryQuery, Reasoning: reason, Fallback: true}
}
