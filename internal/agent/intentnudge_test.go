package agent

import (
	"strings"
	"testing"
)

func TestShouldNudgeIncompleteToolIntent(t *testing.T) {
	t.Parallel()
	longIntent := "Let me " + strings.Repeat("word ", 200)
	cases := []struct {
		text string
		want bool
	}{
		{"Let me check the semantic_search tool implementation:", true},
		{"let me grep for that symbol first.", true},
		{"I need to read internal/agent/runner.go.", true},
		{"I'm going to search the repo for that.", true},
		{"First, I'll list the directory.", true},
		{"To answer this, I need more context:", true},
		{"To investigate, I'll use grep:", true},
		{"Here is the plan:", true},
		{"The answer is 42.", false},
		{"", false},
		{"```go\nx := 1\n```", false},
		{longIntent, false},
	}
	for _, tc := range cases {
		if got := shouldNudgeIncompleteToolIntent(tc.text); got != tc.want {
			t.Errorf("shouldNudgeIncompleteToolIntent(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
