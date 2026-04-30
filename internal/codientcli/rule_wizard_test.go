package codientcli

import (
	"strings"
	"testing"
)

func TestValidRuleStem(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"go-style", true},
		{"a", true},
		{"api-v2-errors", true},
		{"", false},
		{"-bad", false},
		{"bad-", false},
		{"double--hyphen", false},
		{"UPPER", false},
		{"has/slash", false},
		{strings.Repeat("a", 81), false},
	}
	for _, tc := range tests {
		if got := validRuleStem(tc.in); got != tc.want {
			t.Errorf("validRuleStem(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildCursorRuleMdc_AlwaysApply(t *testing.T) {
	got := buildCursorRuleMdc("One line desc.", true, "", "My rule", "## Body\n")
	wantSub := []string{
		"description: |",
		"  One line desc.",
		"alwaysApply: true",
		"---",
		"# My rule",
		"## Body",
	}
	for _, s := range wantSub {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
	if strings.Contains(got, "globs:") {
		t.Errorf("always-apply rule should not contain globs: %s", got)
	}
}

func TestBuildCursorRuleMdc_Globs(t *testing.T) {
	got := buildCursorRuleMdc("For Go files.", false, "**/*.go", "Go", "")
	if !strings.Contains(got, `globs: "**/*.go"`) {
		t.Errorf("expected globs line, got:\n%s", got)
	}
	if !strings.Contains(got, "alwaysApply: false") {
		t.Errorf("expected alwaysApply false, got:\n%s", got)
	}
}

func TestYamlDoubleQuoted(t *testing.T) {
	if got := yamlDoubleQuoted(`say "hi"`); got != `"say \"hi\""` {
		t.Errorf("yamlDoubleQuoted = %q", got)
	}
	if got := yamlDoubleQuoted("a\nb"); got != `"a\nb"` {
		t.Errorf("yamlDoubleQuoted newline = %q", got)
	}
}
