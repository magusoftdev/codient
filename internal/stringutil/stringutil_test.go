package stringutil

import "testing"

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty", "", 10, ""},
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello…"},
		{"zero max", "hello", 0, ""},
		{"negative max", "hello", -1, ""},
		{"unicode", "héllo wörld", 5, "héllo…"},
		{"emoji", "👋🌍🎉💡🔥✨", 3, "👋🌍🎉…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateRunes(tc.s, tc.max)
			if got != tc.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
			}
		})
	}
}
