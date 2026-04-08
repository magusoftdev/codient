package prompt

import (
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"", ModeBuild},
		{"  ", ModeBuild},
		{"build", ModeBuild},
		{"BUILD", ModeBuild},
		{"ask", ModeAsk},
		{"Plan", ModePlan},
		{"design", ModePlan},
	}
	for _, tc := range tests {
		got, err := ParseMode(tc.in)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMode(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
	_, err := ParseMode("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}
