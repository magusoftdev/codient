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
}
