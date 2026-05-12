package intent

import (
	"strings"
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain object",
			in:   `{"category":"QUERY","reasoning":"hi"}`,
			want: `{"category":"QUERY","reasoning":"hi"}`,
		},
		{
			name: "with leading prose",
			in:   "sure thing!\n{\"category\":\"DESIGN\",\"reasoning\":\"x\"}",
			want: `{"category":"DESIGN","reasoning":"x"}`,
		},
		{
			name: "wrapped in code fence",
			in:   "```json\n{\"category\":\"SIMPLE_FIX\",\"reasoning\":\"y\"}\n```",
			want: `{"category":"SIMPLE_FIX","reasoning":"y"}`,
		},
		{
			name: "nested braces",
			in:   `{"category":"COMPLEX_TASK","reasoning":"refactor {x}"}`,
			want: `{"category":"COMPLEX_TASK","reasoning":"refactor {x}"}`,
		},
		{
			name: "trailing prose",
			in:   `{"category":"QUERY","reasoning":"a"} that's all`,
			want: `{"category":"QUERY","reasoning":"a"}`,
		},
		{
			name: "no object",
			in:   "no json here",
			want: "",
		},
		{
			name: "qwen3 thinking model — closed think tag before JSON",
			in:   "<think>\nThe user is asking a question about the repo, no edits.\n</think>\n{\"category\":\"QUERY\",\"reasoning\":\"info request\"}",
			want: `{"category":"QUERY","reasoning":"info request"}`,
		},
		{
			name: "uppercase tag variant",
			in:   "<THINK>internal monologue…</THINK>{\"category\":\"DESIGN\",\"reasoning\":\"arch\"}",
			want: `{"category":"DESIGN","reasoning":"arch"}`,
		},
		{
			name: "multiple reasoning tag variants",
			in:   "<thought>step 1</thought><reasoning>step 2</reasoning>{\"category\":\"SIMPLE_FIX\",\"reasoning\":\"tiny\"}",
			want: `{"category":"SIMPLE_FIX","reasoning":"tiny"}`,
		},
		{
			name: "think tag with attributes",
			in:   `<think id="abc">hidden</think>{"category":"COMPLEX_TASK","reasoning":"refactor"}`,
			want: `{"category":"COMPLEX_TASK","reasoning":"refactor"}`,
		},
		{
			name: "think tag wrapping the JSON itself is stripped (caller retries)",
			in:   "<think>{\"category\":\"QUERY\",\"reasoning\":\"x\"}</think>",
			want: "",
		},
		{
			name: "dangling (unclosed) think tag drops trailing text",
			in:   "<think>truncated thoughts about the prompt and then the stream cut off",
			want: "",
		},
		{
			name: "JSON before dangling think tag is preserved",
			in:   `{"category":"QUERY","reasoning":"a"}<think>extra musing`,
			want: `{"category":"QUERY","reasoning":"a"}`,
		},
		{
			name: "fenced JSON after stripped think tag",
			in:   "<think>thinking…</think>\n```json\n{\"category\":\"DESIGN\",\"reasoning\":\"x\"}\n```",
			want: `{"category":"DESIGN","reasoning":"x"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSONObject(tc.in)
			if got != tc.want {
				t.Fatalf("extractJSONObject(%q) = %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripReasoningTags(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no tags", in: "hello world", want: "hello world"},
		{name: "single closed", in: "before<think>x</think>after", want: "beforeafter"},
		{name: "multiline closed", in: "<think>\nline 1\nline 2\n</think>tail", want: "tail"},
		{name: "scratchpad", in: "<scratchpad>note</scratchpad>{}", want: "{}"},
		{name: "reflection", in: "<reflection>rethink</reflection>x", want: "x"},
		{name: "uppercase close tag", in: "<think>x</THINK>y", want: "y"},
		{name: "dangling drops suffix", in: "head<think>tail not closed", want: "head"},
		{name: "case-insensitive opening", in: "head<THOUGHT>oops", want: "head"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripReasoningTags(tc.in)
			if got != tc.want {
				t.Fatalf("stripReasoningTags(%q) = %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSummariseBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "<empty>"},
		{name: "whitespace only", in: "  \t\n ", want: "<empty>"},
		{name: "short", in: `<think>boom`, want: "<think>boom"},
		{name: "newlines folded to space", in: "line1\nline2", want: "line1 line2"},
		{name: "drops control chars", in: "ok\x00bye", want: "okbye"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summariseBody(tc.in)
			if got != tc.want {
				t.Fatalf("summariseBody(%q) = %q want %q", tc.in, got, tc.want)
			}
		})
	}
	// Long inputs truncate with an ellipsis.
	long := strings.Repeat("x", 200)
	got := summariseBody(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation ellipsis, got %q", got)
	}
	if len([]rune(got)) > 121 {
		t.Fatalf("truncated body too long: %d runes", len([]rune(got)))
	}
}

func TestNormalizeCategory(t *testing.T) {
	cases := []struct {
		in   string
		want Category
		ok   bool
	}{
		{"QUERY", CategoryQuery, true},
		{"query", CategoryQuery, true},
		{"  ask ", CategoryQuery, true},
		{"DESIGN", CategoryDesign, true},
		{"plan", CategoryDesign, true},
		{"SIMPLE_FIX", CategorySimpleFix, true},
		{"simple-fix", CategorySimpleFix, true},
		{"fix", CategorySimpleFix, true},
		{"COMPLEX_TASK", CategoryComplexTask, true},
		{"complextask", CategoryComplexTask, true},
		{"refactor", CategoryComplexTask, true},
		{"unknown", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeCategory(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("normalizeCategory(%q) = (%q,%v) want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseSupervisorReply_Valid(t *testing.T) {
	id, err := parseSupervisorReply(`{"category":"COMPLEX_TASK","reasoning":"multi-file refactor"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Category != CategoryComplexTask {
		t.Fatalf("category = %q, want %q", id.Category, CategoryComplexTask)
	}
	if id.Reasoning != "multi-file refactor" {
		t.Fatalf("reasoning = %q", id.Reasoning)
	}
}

func TestParseSupervisorReply_Invalid(t *testing.T) {
	cases := []string{
		"",
		"not json",
		`{"category":"WAT","reasoning":"x"}`,
		`{"category":"","reasoning":"y"}`,
	}
	for _, in := range cases {
		_, err := parseSupervisorReply(in)
		if err == nil {
			t.Fatalf("expected error for input %q", in)
		}
	}
}

func TestTrimReasoning(t *testing.T) {
	long := strings.Repeat("a", 250)
	out := trimReasoning(long, 100)
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("expected ellipsis suffix; got %q", out)
	}
	if len([]rune(out)) != 101 {
		t.Fatalf("expected 101 runes (100 chars + ellipsis); got %d", len([]rune(out)))
	}
	short := trimReasoning("ok", 0)
	if short != "ok" {
		t.Fatalf("trim short: %q", short)
	}
}

func TestFallback(t *testing.T) {
	f := fallback("nope")
	if f.Category != CategoryQuery {
		t.Fatalf("fallback category = %q, want QUERY", f.Category)
	}
	if !f.Fallback {
		t.Fatalf("fallback flag should be true")
	}
	if !strings.Contains(f.Reasoning, "nope") {
		t.Fatalf("fallback reasoning lost: %q", f.Reasoning)
	}
}

func TestSupervisorSystemPromptShape(t *testing.T) {
	if !strings.Contains(SupervisorSystemPrompt, "QUERY") ||
		!strings.Contains(SupervisorSystemPrompt, "DESIGN") ||
		!strings.Contains(SupervisorSystemPrompt, "SIMPLE_FIX") ||
		!strings.Contains(SupervisorSystemPrompt, "COMPLEX_TASK") {
		t.Fatalf("system prompt missing one of the four categories")
	}
	if !strings.Contains(SupervisorSystemPrompt, "JSON") {
		t.Fatalf("system prompt should require JSON output")
	}
	// Anti-thinking guidance — paired with the /no_think suffix appended to
	// the user message so thinking-capable models stop burning the budget
	// inside their hidden reasoning channel before emitting JSON.
	if !strings.Contains(SupervisorSystemPrompt, "<think>") &&
		!strings.Contains(SupervisorSystemPrompt, "think out loud") {
		t.Fatalf("system prompt should discourage internal reasoning / <think> blocks")
	}
}

func TestSupervisorUserSuffixIsNoThinkDirective(t *testing.T) {
	if !strings.Contains(supervisorUserSuffix, "/no_think") {
		t.Fatalf("supervisorUserSuffix should append the /no_think directive; got %q", supervisorUserSuffix)
	}
}

// TestHeuristicQuickClassify covers the high-confidence pattern classifier
// shared by the pre-LLM fast path and the post-LLM-failure fallback. Cases
// are grouped by category to make it easy to see which signals fire which
// route.
func TestHeuristicQuickClassify(t *testing.T) {
	type wantMatch struct {
		category Category
		matched  bool // true if heuristicQuickClassify should fire; false = falls through to LLM
	}
	cases := []struct {
		name string
		in   string
		want wantMatch
	}{
		// --- DESIGN ---
		{"design — create a plan", "Create a plan for caching the repo map.", wantMatch{CategoryDesign, true}},
		{"design — draft a plan polite", "Please draft a plan to migrate the auth module.", wantMatch{CategoryDesign, true}},
		{"design — design a X", "Design a plugin system for codient.", wantMatch{CategoryDesign, true}},
		{"design — how should I", "how should I structure plugins?", wantMatch{CategoryDesign, true}},
		{"design — how should we", "How should we wire ACP into the editor?", wantMatch{CategoryDesign, true}},
		{"design — what's the best way", "what's the best way to handle retries?", wantMatch{CategoryDesign, true}},
		{"design — plan for X", "Plan for adding observability hooks.", wantMatch{CategoryDesign, true}},

		// --- COMPLEX_TASK (edit verb + multi-file scope) ---
		{"complex — refactor across every file", "refactor the session module across every file", wantMatch{CategoryComplexTask, true}},
		{"complex — migrate the entire codebase", "Migrate the entire codebase to the new API.", wantMatch{CategoryComplexTask, true}},
		{"complex — rewrite everywhere", "Rewrite the logging calls everywhere.", wantMatch{CategoryComplexTask, true}},
		{"complex — update all files", "update all files that import openaiclient", wantMatch{CategoryComplexTask, true}},
		{"complex — change throughout", "Change the import paths throughout the project.", wantMatch{CategoryComplexTask, true}},

		// --- SIMPLE_FIX (polite imperative OR edit verb + small scope) ---
		{"simple — please fix typo", "Please fix the typo in README.md", wantMatch{CategorySimpleFix, true}},
		{"simple — please add field", "Please add a field to the Options struct.", wantMatch{CategorySimpleFix, true}},
		{"simple — can you rename", "Can you rename foo to bar in this function?", wantMatch{CategorySimpleFix, true}},
		{"simple — could you fix", "Could you fix the lint warning on line 42?", wantMatch{CategorySimpleFix, true}},
		{"simple — fix typo", "Fix the typo on line 42 of config.go.", wantMatch{CategorySimpleFix, true}},
		{"simple — add comment", "Add a comment to the run loop explaining the retry budget.", wantMatch{CategorySimpleFix, true}},
		{"simple — rename in this function", "Rename ctx to opCtx in this function.", wantMatch{CategorySimpleFix, true}},

		// --- QUERY ---
		{"query — what is", "What is the difference between QUERY and DESIGN?", wantMatch{CategoryQuery, true}},
		{"query — why is", "Why is the supervisor budget so small?", wantMatch{CategoryQuery, true}},
		{"query — how does", "How does the orchestrator pick a tier?", wantMatch{CategoryQuery, true}},
		{"query — explain", "Explain the plan-to-build handoff.", wantMatch{CategoryQuery, true}},
		{"query — describe", "Describe the ACP session lifecycle.", wantMatch{CategoryQuery, true}},
		{"query — tell me about", "Tell me about the reasoning tiers.", wantMatch{CategoryQuery, true}},
		{"query — is this", "is this safe to run in CI?", wantMatch{CategoryQuery, true}},
		{"query — does this", "Does this support Windows?", wantMatch{CategoryQuery, true}},
		{"query — trailing ?", "the orchestrator skipped the LLM, normal?", wantMatch{CategoryQuery, true}},

		// --- Falls through to LLM (heuristic is intentionally conservative) ---
		{"fallthrough — empty", "", wantMatch{"", false}},
		{"fallthrough — whitespace", "   \n\t  ", wantMatch{"", false}},
		{"fallthrough — ambiguous noun", "anything", wantMatch{"", false}},
		{"fallthrough — edit verb no scope no politeness", "refactor session.go", wantMatch{"", false}},
		{"fallthrough — edit verb solo", "fix it", wantMatch{"", false}},
		// "refactoring" is a noun here, not the imperative verb the heuristic
		// looks for; correctly falls through to the LLM rather than firing
		// SIMPLE_FIX or DESIGN on a verb-as-noun mention.
		{"fallthrough — verb-as-noun", "tell me how refactoring works", wantMatch{CategoryQuery, true}}, // "tell me how" prefix matches QUERY phrase
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, why, ok := heuristicQuickClassify(tc.in)
			if ok != tc.want.matched {
				t.Fatalf("heuristicQuickClassify(%q) ok=%v (cat=%q reason=%q), want matched=%v", tc.in, ok, got, why, tc.want.matched)
			}
			if ok && got != tc.want.category {
				t.Fatalf("heuristicQuickClassify(%q) = %q (reason=%q), want %q", tc.in, got, why, tc.want.category)
			}
		})
	}
}

func TestHeuristicFallback_RoutesViaQuickClassifier(t *testing.T) {
	id := heuristicFallback("refactor the auth module across every file", "parse error after retry: budget=80 finish=\"length\"")
	if !id.Fallback {
		t.Fatal("expected Fallback=true")
	}
	if id.Source != SourceHeuristicFallback {
		t.Fatalf("source = %q, want %q", id.Source, SourceHeuristicFallback)
	}
	if id.Category != CategoryComplexTask {
		t.Fatalf("category = %q, want COMPLEX_TASK (multi-file scope hint should win)", id.Category)
	}
	for _, want := range []string{"supervisor:", "parse error", "heuristic", "edit verb"} {
		if !strings.Contains(id.Reasoning, want) {
			t.Fatalf("reasoning missing %q; got %q", want, id.Reasoning)
		}
	}
}

func TestHeuristicFallback_NoMatchDefaultsToQuery(t *testing.T) {
	id := heuristicFallback("anything", "parse error after retry: …")
	if !id.Fallback {
		t.Fatal("expected Fallback=true")
	}
	if id.Category != CategoryQuery {
		t.Fatalf("category = %q, want QUERY (safety net for ambiguous prompts)", id.Category)
	}
	if !strings.Contains(id.Reasoning, "no confident heuristic match") {
		t.Fatalf("expected fallback to mention no-match reason; got %q", id.Reasoning)
	}
}

func TestTryReasoningChannelSalvage(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantOK      bool
		wantChannel string
		wantBody    string
	}{
		{
			name:        "reasoning_content string",
			raw:         `{"role":"assistant","content":"","reasoning_content":"{\"category\":\"QUERY\",\"reasoning\":\"x\"}"}`,
			wantOK:      true,
			wantChannel: "reasoning_content",
			wantBody:    `{"category":"QUERY","reasoning":"x"}`,
		},
		{
			name:        "reasoning string",
			raw:         `{"role":"assistant","content":"","reasoning":"{\"category\":\"DESIGN\",\"reasoning\":\"y\"}"}`,
			wantOK:      true,
			wantChannel: "reasoning",
			wantBody:    `{"category":"DESIGN","reasoning":"y"}`,
		},
		{
			name:        "reasoning_content object with text field",
			raw:         `{"role":"assistant","content":"","reasoning_content":{"text":"{\"category\":\"SIMPLE_FIX\",\"reasoning\":\"z\"}"}}`,
			wantOK:      true,
			wantChannel: "reasoning_content",
			wantBody:    `{"category":"SIMPLE_FIX","reasoning":"z"}`,
		},
		{
			name:   "neither field present",
			raw:    `{"role":"assistant","content":""}`,
			wantOK: false,
		},
		{
			name:   "empty reasoning_content",
			raw:    `{"role":"assistant","content":"","reasoning_content":""}`,
			wantOK: false,
		},
		{
			name:   "invalid json",
			raw:    `not json`,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, channel, ok := tryReasoningChannelSalvage(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (body=%q channel=%q)", ok, tc.wantOK, body, channel)
			}
			if !ok {
				return
			}
			if channel != tc.wantChannel {
				t.Fatalf("channel = %q, want %q", channel, tc.wantChannel)
			}
			if body != tc.wantBody {
				t.Fatalf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestFormatAttempt(t *testing.T) {
	a := attemptInfo{
		budget:       80,
		finishReason: "length",
		body:         "<think>round 1 thinking aloud about the prompt…",
		channel:      "content",
		truncated:    true,
	}
	got := formatAttempt(a)
	for _, want := range []string{"budget=80", `finish="length"`, "channel=content", "<think>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatAttempt missing %q; got %q", want, got)
		}
	}
}
