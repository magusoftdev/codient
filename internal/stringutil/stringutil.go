// Package stringutil provides small string helpers shared across codient packages.
package stringutil

import (
	"strings"
	"unicode/utf8"
)

// TruncateRunes truncates s to at most max runes, appending "…" if truncated.
// Returns "" when max <= 0.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	rs := []rune(s)
	if len(rs) > max {
		rs = rs[:max]
	}
	return strings.TrimSpace(string(rs)) + "…"
}
