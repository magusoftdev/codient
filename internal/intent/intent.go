// Package intent classifies a user prompt into one of four orchestrator categories
// (QUERY, DESIGN, SIMPLE_FIX, COMPLEX_TASK) by issuing a small JSON-only chat
// completion against the configured low-reasoning model.
//
// The supervisor is intentionally cheap: a terse system prompt, low temperature,
// and a tight token budget so high-end hardware (LM Studio / Ollama) returns the
// classification in well under a second.
//
// Resilience: many local "thinking" models (Qwen3-Thinking, DeepSeek-R1, etc.)
// emit a `<think>…</think>` block before the structured answer. When the
// initial budget truncates inside that block the response contains no JSON, so
// the supervisor automatically retries once with a much larger budget. Tags
// emitted alongside the JSON are stripped before parsing.
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
	"regexp"
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

// Source identifies which classifier produced an Identification. Surfaced on
// the on-screen status line and the ACP `session/intent_identified`
// notification so users (and Codient Unity) can tell whether the LLM was
// consulted at all.
type Source string

const (
	// SourceSupervisor — the low-tier supervisor LLM classified the prompt.
	SourceSupervisor Source = "supervisor"
	// SourceHeuristic — the pre-LLM fast-path heuristic matched the prompt
	// with high confidence and the supervisor was skipped entirely.
	SourceHeuristic Source = "heuristic"
	// SourceHeuristicFallback — the supervisor was consulted but failed (or
	// the client was nil / prompt empty); the heuristic provided a safe
	// default. Implies Fallback == true.
	SourceHeuristicFallback Source = "heuristic-fallback"
)

// Identification is the structured supervisor reply.
type Identification struct {
	Category  Category `json:"category"`
	Reasoning string   `json:"reasoning"`
	// Fallback is true when the supervisor failed and the classifier substituted
	// a safe default. Reasoning explains why in that case.
	Fallback bool `json:"-"`
	// Source records which classifier produced this Identification. Empty
	// values are treated as SourceSupervisor for back-compat with older
	// callers that don't populate the field.
	Source Source `json:"-"`
}

// Options tunes IdentifyIntent. All fields are optional.
type Options struct {
	// Tracker records token usage from the supervisor call when set.
	Tracker *tokentracker.Tracker
	// MaxReasoningChars truncates the user-visible reasoning string. 0 = 200.
	MaxReasoningChars int
	// MaxCompletionTokens caps the supervisor's initial completion budget.
	// 0 selects defaultSupervisorMaxCompletionTokens (80). Thinking models
	// (Qwen3-Thinking, DeepSeek-R1, …) burn tokens in a hidden reasoning
	// trace before emitting the JSON answer, so power users on those models
	// may want a larger initial budget (e.g. 256–512) to avoid the retry.
	MaxCompletionTokens int
	// RetryMaxCompletionTokens caps the second attempt's completion budget,
	// triggered when the first attempt finishes with reason "length" and no
	// JSON object was recovered. 0 selects max(4×initial, 1024) capped at
	// maxSupervisorRetryBudget (2048). Set to a value <= initial to disable
	// the retry entirely.
	RetryMaxCompletionTokens int
	// DisableHeuristic, when true, skips the pre-LLM heuristic fast-path so
	// every turn consults the supervisor model. Useful for users who trust
	// their model's classification over keyword patterns, or to A/B compare
	// the two paths. The post-LLM-failure heuristic fallback path is NOT
	// affected — a faulty supervisor still needs the safety net.
	DisableHeuristic bool
}

// SupervisorSystemPrompt is exported so callers (and tests) can reference the
// exact text that drives the classification. Kept terse so the model spends its
// budget on the JSON answer rather than the schema explanation.
//
// The trailing "Do NOT think out loud" line is model-agnostic guidance that
// pairs with the `/no_think` directive appended to the user message — Qwen3
// thinking variants recognise the directive natively; other models read the
// system-prompt line.
const SupervisorSystemPrompt = `You classify a coding-agent user prompt into ONE category. Reply with ONLY a JSON object, no prose, no code fences.

Schema:
{"category":"QUERY|DESIGN|SIMPLE_FIX|COMPLEX_TASK","reasoning":"<<=20 words>"}

Categories:
- QUERY: questions about the codebase or general info; no edits expected.
- DESIGN: architectural advice, design patterns, UI mockups, "how should I structure"; no code yet.
- SIMPLE_FIX: small localized change (1-2 files, typo, rename, add log, tweak config).
- COMPLEX_TASK: multi-file refactor, new feature, anything needing a plan first.

Pick the single best fit. Output JSON only.
Do NOT think out loud, use <think> blocks, or emit chain-of-thought before the JSON. Your VERY FIRST token must be "{".`

// supervisorUserSuffix is appended to every supervisor user message. Qwen3
// (and other recent thinking-capable models) recognise `/no_think` as a
// per-turn directive to skip their hidden reasoning channel — the same
// channel that burns the supervisor's tight token budget before the JSON
// answer can be emitted. Models that don't recognise the directive treat
// it as trailing text and harmlessly ignore it (the supervisor system
// prompt instructs them to classify the user prompt and emit JSON, so a
// "/no_think" suffix can't shift the classification).
const supervisorUserSuffix = " /no_think"

// defaultSupervisorMaxCompletionTokens is the initial completion budget for
// IdentifyIntent. It is intentionally tight so non-thinking models return the
// JSON answer in well under a second. Thinking models that need more headroom
// either trip the retry path below or override Options.MaxCompletionTokens.
const defaultSupervisorMaxCompletionTokens = 80

// defaultSupervisorRetryFactor multiplies the initial budget when the first
// attempt finishes with reason "length" and no JSON was recovered.
const defaultSupervisorRetryFactor = 4

// minSupervisorRetryBudget / maxSupervisorRetryBudget bound the retry budget
// regardless of the configured initial size. The lower bound ensures a
// "thinking" model has enough room to finish its reasoning + the JSON answer;
// the upper bound prevents a runaway local model from monopolising the tier.
const (
	minSupervisorRetryBudget = 1024
	maxSupervisorRetryBudget = 2048
)

// finishReasonLength is the openai-compatible finish_reason emitted when the
// server truncated the response because MaxCompletionTokens was reached.
const finishReasonLength = "length"

// IdentifyIntent classifies userPrompt into one of the four orchestrator
// categories. The classification path is:
//
//  1. **Heuristic fast path** (`heuristicQuickClassify`) — a pattern-based
//     classifier that matches high-confidence signals (DESIGN phrases like
//     "create a plan", polite imperatives like "please fix X", edit verbs
//     with scope hints, query phrases or trailing "?"). On a confident
//     match the supervisor LLM is skipped entirely. Pass
//     Options.DisableHeuristic to force every turn to consult the model.
//  2. **Supervisor LLM** (`runSupervisor`) — a tiny JSON-only chat
//     completion against client (which should be wired to the
//     low-reasoning tier). Truncated replies with no parseable JSON
//     trigger a single retry with a larger budget; the second attempt
//     additionally tries to salvage JSON from a non-standard
//     `reasoning_content` / `reasoning` field on the assistant message.
//  3. **Heuristic fallback** (`heuristicFallback`) — the same pattern
//     classifier from step 1, but with QUERY as the default for any
//     prompt that doesn't match a confident pattern (safety net so a
//     faulty supervisor can never deadlock the agent).
//
// Returns a populated Identification; never returns nil. The error is
// non-nil only when the classifier could not even contact the model AND
// the fallback path was applied — callers can usually log it and continue
// with the fallback.
func IdentifyIntent(ctx context.Context, client *openaiclient.Client, userPrompt string, opts Options) (Identification, error) {
	if client == nil {
		return fallback("nil client"), errors.New("intent: nil client")
	}
	trimmed := strings.TrimSpace(userPrompt)
	if trimmed == "" {
		return fallback("empty prompt"), errors.New("intent: empty prompt")
	}

	// 1. Heuristic fast path — skip the LLM entirely when intent is
	// unambiguous from the prompt's structure alone.
	if !opts.DisableHeuristic {
		if cat, reason, ok := heuristicQuickClassify(trimmed); ok {
			return Identification{
				Category:  cat,
				Reasoning: trimReasoning(reason, opts.MaxReasoningChars),
				Source:    SourceHeuristic,
			}, nil
		}
	}

	initialBudget := opts.MaxCompletionTokens
	if initialBudget <= 0 {
		initialBudget = defaultSupervisorMaxCompletionTokens
	}
	retryBudget := opts.RetryMaxCompletionTokens
	if retryBudget == 0 {
		retryBudget = initialBudget * defaultSupervisorRetryFactor
		if retryBudget < minSupervisorRetryBudget {
			retryBudget = minSupervisorRetryBudget
		}
		if retryBudget > maxSupervisorRetryBudget {
			retryBudget = maxSupervisorRetryBudget
		}
	}

	id, first, err := runSupervisor(ctx, client, trimmed, initialBudget, opts.Tracker)
	if err != nil {
		return id, err
	}
	if id.Category != "" {
		id.Reasoning = trimReasoning(id.Reasoning, opts.MaxReasoningChars)
		id.Source = SourceSupervisor
		return id, nil
	}

	// Retry only when the first attempt was truncated and we have additional
	// budget available. Otherwise heuristic-fallback on the original prompt.
	if !first.truncated || retryBudget <= initialBudget {
		return heuristicFallback(trimmed, "parse error: "+formatAttempt(first)), nil
	}

	retryID, second, retryErr := runSupervisor(ctx, client, trimmed, retryBudget, opts.Tracker)
	if retryErr != nil {
		// Network/chat error on retry — preserve the chat error for callers.
		return heuristicFallback(trimmed, fmt.Sprintf("chat error on retry: %v; first %s", retryErr, formatAttempt(first))), retryErr
	}
	if retryID.Category != "" {
		retryID.Reasoning = trimReasoning(retryID.Reasoning, opts.MaxReasoningChars)
		retryID.Source = SourceSupervisor
		return retryID, nil
	}
	reason := "parse error after retry: " + formatAttempt(first) + "; retry " + formatAttempt(second)
	return heuristicFallback(trimmed, reason), nil
}

// attemptInfo captures the per-attempt diagnostics threaded into the
// fallback reasoning when the supervisor fails to classify. Surfacing the
// finish_reason and a body summary on every attempt makes it possible to
// distinguish "the model truncated mid-thought" from "the server returned
// an empty body" from "the model returned non-JSON prose".
type attemptInfo struct {
	budget       int
	finishReason string
	body         string
	channel      string // "content" or "reasoning_content" (when salvaged) — empty for unknown
	truncated    bool
}

// formatAttempt renders an attemptInfo as a single-line diagnostic.
func formatAttempt(a attemptInfo) string {
	channel := a.channel
	if channel == "" {
		channel = "content"
	}
	return fmt.Sprintf("budget=%d finish=%q channel=%s body=%s",
		a.budget, a.finishReason, channel, summariseBody(a.body))
}

// runSupervisor executes one supervisor turn and returns either a parsed
// Identification (Category != ""), or an empty Identification together with
// per-attempt diagnostics so the caller can decide whether to retry.
// A non-nil err is returned only when the chat call itself failed; parse
// failures are signaled by an empty Category on the returned Identification.
func runSupervisor(ctx context.Context, client *openaiclient.Client, userPrompt string, budget int, tracker *tokentracker.Tracker) (Identification, attemptInfo, error) {
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(client.Model()),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(SupervisorSystemPrompt),
			openai.UserMessage(userPrompt + supervisorUserSuffix),
		},
		Temperature:         openai.Float(0),
		MaxCompletionTokens: openai.Int(int64(budget)),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}
	info := attemptInfo{budget: budget}
	res, err := client.ChatCompletion(ctx, params)
	if err != nil {
		return fallback(fmt.Sprintf("supervisor: chat error: %v", err)), info, err
	}
	if tracker != nil && res != nil {
		tracker.Add(tokentracker.Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		})
	}
	if res == nil || len(res.Choices) == 0 {
		return fallback("supervisor: empty response"), info, nil
	}
	choice := res.Choices[0]
	info.body = choice.Message.Content
	info.finishReason = strings.ToLower(strings.TrimSpace(choice.FinishReason))
	info.truncated = info.finishReason == finishReasonLength
	info.channel = "content"

	if parsed, perr := parseSupervisorReply(info.body); perr == nil {
		return parsed, info, nil
	}

	// Salvage path: some local OpenAI-compatible servers (LM Studio, vLLM,
	// Ollama with reasoning patches) emit the assistant's structured answer
	// into a non-standard `reasoning_content` (or `reasoning`) field on the
	// message instead of `content`. When `content` failed to parse, try the
	// reasoning channel before giving up — without raising the token budget.
	if salvaged, channel, ok := tryReasoningChannelSalvage(choice.Message.RawJSON()); ok {
		info.body = salvaged
		info.channel = channel
		if parsed, perr := parseSupervisorReply(salvaged); perr == nil {
			return parsed, info, nil
		}
	}

	// Empty Category signals "no parse" to the caller; info.truncated tells
	// it whether a retry with a larger budget might help.
	return Identification{}, info, nil
}

// tryReasoningChannelSalvage scans a chat-completion message's raw JSON for a
// "reasoning_content" or "reasoning" field whose value (a string, or an
// object with a text/content/summary field) contains parseable supervisor
// JSON. Returns the salvaged body, the channel name it came from, and a flag
// indicating whether anything was extracted. This is a defensive fallback
// for thinking-model servers that leak the structured answer into a
// non-content channel when the answer arrives near the budget limit.
func tryReasoningChannelSalvage(raw string) (body, channel string, ok bool) {
	if raw == "" {
		return "", "", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", "", false
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		v, present := m[key]
		if !present {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return s, key, true
		}
		var obj struct {
			Text    string `json:"text"`
			Content string `json:"content"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal(v, &obj) == nil {
			switch {
			case strings.TrimSpace(obj.Text) != "":
				return obj.Text, key, true
			case strings.TrimSpace(obj.Content) != "":
				return obj.Content, key, true
			case strings.TrimSpace(obj.Summary) != "":
				return obj.Summary, key, true
			}
		}
	}
	return "", "", false
}

// summariseBody returns a short, single-line preview of a supervisor reply for
// inclusion in fallback diagnostics. Long bodies are truncated; control chars
// and embedded newlines are replaced with spaces so the reason fits on one line.
func summariseBody(body string) string {
	const max = 120
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\r', r == '\t':
			return ' '
		case r < 0x20:
			return -1
		}
		return r
	}, body)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "<empty>"
	}
	if len(cleaned) > max {
		return cleaned[:max] + "…"
	}
	return cleaned
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

// reasoningTagNames lists the wrapper tags that thinking models emit before
// their JSON answer. Each is stripped along with its content by stripReasoningTags.
var reasoningTagNames = []string{"think", "thought", "reasoning", "reflection", "scratchpad"}

// reasoningTagREs holds one regex per tag in reasoningTagNames. Go's RE2 has
// no backreferences, so we can't write a single `<(name)…</\1>` pattern; we
// compile per-tag patterns instead. `(?is)` makes `.` match newlines and the
// match case-insensitive; `.*?` keeps it non-greedy so adjacent blocks are
// stripped individually.
var reasoningTagREs = compileReasoningTagREs()

// danglingReasoningTagRE matches the *opening* of any reasoning tag. When the
// model was truncated mid-thought the closing tag is missing; we drop
// everything from the opening tag onwards so the JSON scan starts from
// clean territory (the JSON, if any, lives before the tag — or hasn't been
// emitted yet, in which case the caller will retry).
var danglingReasoningTagRE = regexp.MustCompile(
	`(?is)<(` + strings.Join(reasoningTagNames, "|") + `)\b[^>]*>`,
)

func compileReasoningTagREs() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(reasoningTagNames))
	for _, name := range reasoningTagNames {
		out = append(out, regexp.MustCompile(`(?is)<`+name+`\b[^>]*>.*?</\s*`+name+`\s*>`))
	}
	return out
}

// stripReasoningTags removes hidden-reasoning blocks emitted by thinking
// models (Qwen3-Thinking, DeepSeek-R1, …) so the JSON scanner can find the
// final structured answer. Closed pairs are removed in place; an unclosed
// opening tag truncates everything after it.
func stripReasoningTags(s string) string {
	for _, re := range reasoningTagREs {
		s = re.ReplaceAllString(s, "")
	}
	if loc := danglingReasoningTagRE.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return s
}

// extractJSONObject returns the first balanced {...} substring from s, ignoring
// surrounding ``` fences, leading text, trailing prose, or `<think>` blocks
// emitted by thinking models. Empty when none.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = stripReasoningTags(s)
	if strings.HasPrefix(s, "```") {
		// Drop the first fence line (e.g. ```json) and any closing fence.
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	// A fence may also appear after the stripped reasoning block; trim again.
	s = strings.TrimSpace(s)
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
	return Identification{
		Category:  CategoryQuery,
		Reasoning: "supervisor: " + reason,
		Fallback:  true,
		Source:    SourceHeuristicFallback,
	}
}

// heuristicFallback is invoked when the supervisor LLM cannot produce a
// parseable answer (parse error, truncation without recovery, chat error).
// It runs heuristicQuickClassify on the original prompt and routes to the
// matched category on a confident match, or to QUERY on no match (safety
// net so a faulty supervisor can never deadlock the agent on a write path
// that the user never explicitly requested).
//
// The returned Identification preserves the supervisor's diagnostic
// reasoning so the on-screen `intent: ... (fallback) — supervisor: …` line
// tells the user exactly why the LLM call was bypassed.
func heuristicFallback(userPrompt, supervisorReason string) Identification {
	cat, why, ok := heuristicQuickClassify(userPrompt)
	if !ok {
		cat = CategoryQuery
		why = "no confident heuristic match"
	}
	reason := "supervisor: " + supervisorReason + "; routed " + string(cat) + " via heuristic (" + why + ")"
	return Identification{
		Category:  cat,
		Reasoning: reason,
		Fallback:  true,
		Source:    SourceHeuristicFallback,
	}
}

// heuristicEditVerbs lists imperative verbs that signal the user wants the
// agent to make code changes. Used by heuristicQuickClassify together with
// scope-hint detection to pick SIMPLE_FIX vs COMPLEX_TASK.
var heuristicEditVerbs = map[string]bool{
	"add": true, "address": true, "build": true, "change": true,
	"clean": true, "create": true, "delete": true, "design": true,
	"draft": true, "extract": true, "factor": true, "fix": true,
	"hook": true, "implement": true, "improve": true, "inline": true,
	"install": true, "introduce": true, "make": true, "migrate": true,
	"move": true, "patch": true, "port": true, "refactor": true,
	"remove": true, "rename": true, "replace": true, "restructure": true,
	"rewrite": true, "sketch": true, "split": true, "tweak": true,
	"update": true, "wire": true, "write": true,
}

// heuristicDesignPhrases lists multi-word starters that map to DESIGN
// regardless of any other signal. Checked after the politeness prefix is
// stripped, so "please create a plan for X" still matches "create a plan".
var heuristicDesignPhrases = []string{
	"create a plan",
	"draft a plan",
	"make a plan",
	"design a ",
	"design the ",
	"design an ",
	"architect a ",
	"architect the ",
	"architect an ",
	"how should i ",
	"how should we ",
	"how would you ",
	"how do i structure",
	"how do we structure",
	"what's the best way",
	"what is the best way",
	"what approach",
	"plan for ",
	"plan how to ",
	"sketch a design",
}

// heuristicQueryPhrases lists high-confidence question starters that map
// to QUERY. The trailing space in each entry prevents accidental matches
// (e.g. "where " — but not "wherever the code").
var heuristicQueryPhrases = []string{
	"what ", "why ", "where ", "when ", "who ",
	"which ", "whose ",
	"explain ", "describe ", "summarize ", "summarise ",
	"tell me about ", "tell me ",
	"is this ", "is there ", "is the ", "are there ", "are the ",
	"does this ", "does the ", "do you ", "do these ",
	"can you tell ", "can you explain ", "can you describe ",
	"how does ", "how is ", "how are ", "how was ", "how were ",
}

// heuristicComplexScopeHints fire COMPLEX_TASK when paired with a leading
// edit verb. These words indicate the user is asking for a multi-file or
// repo-wide change (which the orchestrator routes through the plan-first
// COMPLEX_TASK path).
var heuristicComplexScopeHints = []string{
	"across", "everywhere", "throughout",
	"all files", "every file", "all the files",
	"entire ", "whole codebase", "whole code base",
	"every place", "every spot", "every module",
	"the entire ", "the whole ",
}

// heuristicSimpleScopeHints fire SIMPLE_FIX when paired with a leading
// edit verb. These mark a clearly localized change (one or two files,
// trivial scope).
var heuristicSimpleScopeHints = []string{
	"typo", "typos",
	"comment", "comments",
	"log statement", "log call", "log line",
	"in line ", "on line ",
	"this function", "this method", "this file", "this class",
	"the function", "the method",
	"single line", "one line",
	"import statement", "import line",
	"trailing whitespace", "leading whitespace",
}

// heuristicPolitenessPrefixes are stripped from the start of the prompt
// before verb detection so "please fix X" / "could you rename Y" / "can you
// add Z" are all treated as imperatives. Order matters: longer prefixes
// first so we strip the maximal match.
var heuristicPolitenessPrefixes = []string{
	"could you please ",
	"would you please ",
	"can you please ",
	"could you kindly ",
	"would you kindly ",
	"would you mind ",
	"could you ",
	"would you ",
	"can you ",
	"please kindly ",
	"please ",
	"kindly ",
	"hey codient, ",
	"hey codient ",
}

// heuristicQuickClassify is the high-confidence pattern classifier shared
// by the pre-LLM fast path (skip the supervisor entirely on match) and the
// post-LLM-failure heuristic fallback. Returns (category, reason, true)
// when the prompt's structure unambiguously implies one of the four
// categories; (_, _, false) when the caller should consult the LLM (or
// fall back to QUERY in the post-failure case).
//
// The patterns are intentionally narrow — they require explicit signals
// like a leading edit verb plus a scope hint, a recognised phrase prefix,
// or a question structure. Anything ambiguous falls through to the LLM,
// which can read context the heuristic cannot.
//
// Pattern ordering (first match wins):
//  1. DESIGN phrase prefixes (after politeness strip)
//  2. Edit verb at start + multi-file scope hint anywhere → COMPLEX_TASK
//  3. Edit verb at start + small-scope hint anywhere → SIMPLE_FIX
//  4. Polite imperative (politeness prefix WAS stripped) + edit verb → SIMPLE_FIX
//  5. QUERY phrase prefixes
//  6. Trailing "?" (and no leading edit verb) → QUERY
//
// Steps 2-4 require the FIRST WORD to be an edit verb, so "what does
// Fix() do?" doesn't match (Fix is in the verb list but not at position 0
// after politeness strip).
func heuristicQuickClassify(userPrompt string) (Category, string, bool) {
	p := strings.TrimSpace(strings.ToLower(userPrompt))
	if p == "" {
		return "", "", false
	}

	// Strip politeness prefix once. politenessStripped is true when a real
	// prefix was removed — used below to recognise "please fix X" as a
	// clear SIMPLE_FIX even without other scope hints.
	rest := p
	politenessStripped := false
	for _, prefix := range heuristicPolitenessPrefixes {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			politenessStripped = true
			break
		}
	}
	if rest == "" {
		return "", "", false
	}

	// 1. DESIGN phrase prefixes (highest priority — explicit "create a
	// plan" outranks any edit-verb / scope-hint combination).
	for _, phrase := range heuristicDesignPhrases {
		if strings.HasPrefix(rest, phrase) {
			return CategoryDesign, "design phrase: " + strings.TrimSpace(phrase), true
		}
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	first := strings.Trim(fields[0], "\"'.,:;!?()[]{}*_`")
	leadingVerb := heuristicEditVerbs[first]

	// 2. Edit verb at start + multi-file scope hint → COMPLEX_TASK.
	if leadingVerb {
		if hint, ok := containsAny(p, heuristicComplexScopeHints); ok {
			return CategoryComplexTask, fmt.Sprintf("edit verb %q + multi-file scope hint %q", first, hint), true
		}
		// 3. Edit verb at start + small-scope hint → SIMPLE_FIX.
		if hint, ok := containsAny(p, heuristicSimpleScopeHints); ok {
			return CategorySimpleFix, fmt.Sprintf("edit verb %q + localized scope hint %q", first, hint), true
		}
		// 4. Polite imperative without an explicit scope hint → SIMPLE_FIX.
		// "please fix X" / "could you rename Y" make the user's intent
		// explicit; the agent will figure scope out from there.
		if politenessStripped {
			return CategorySimpleFix, "polite imperative: " + first, true
		}
		// Unambiguous edit verb at start with NO scope hint and NO
		// politeness marker — let the LLM disambiguate scope.
		return "", "", false
	}

	// 5. QUERY phrase prefixes.
	for _, phrase := range heuristicQueryPhrases {
		if strings.HasPrefix(rest, phrase) {
			return CategoryQuery, "query phrase: " + strings.TrimSpace(phrase), true
		}
	}

	// 6. Trailing "?" — clear question structure when no edit verb at
	// start. Strip trailing whitespace before checking (we already
	// trimmed on entry but defensively recheck the original p).
	if strings.HasSuffix(p, "?") {
		return CategoryQuery, "trailing question mark", true
	}

	return "", "", false
}

// containsAny reports the first member of needles that is a substring of
// haystack (which is expected to be pre-lowercased). Returns the match and
// true on hit, "" and false on miss.
func containsAny(haystack string, needles []string) (string, bool) {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return strings.TrimSpace(n), true
		}
	}
	return "", false
}
