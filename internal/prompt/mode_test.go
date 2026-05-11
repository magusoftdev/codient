package prompt

import (
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"", ModeAuto},
		{"  ", ModeAuto},
		{"auto", ModeAuto},
		{"AUTO", ModeAuto},
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

func TestModeIsResolved(t *testing.T) {
	for _, m := range []Mode{ModeBuild, ModeAsk, ModePlan} {
		if !m.IsResolved() {
			t.Fatalf("%q should be resolved", m)
		}
	}
	for _, m := range []Mode{ModeAuto, "", "wat"} {
		if m.IsResolved() {
			t.Fatalf("%q should not be resolved", m)
		}
	}
}
