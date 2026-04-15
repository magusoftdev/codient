// Package tokenest provides lightweight token-count estimation for context window management.
// Uses a characters-per-token heuristic (no external tokenizer dependency).
package tokenest

// charsPerToken is a conservative ratio for English prose and code across most tokenizers.
const charsPerToken = 4.0

// messageOverhead accounts for per-message framing tokens (role, delimiters).
const messageOverhead = 4

// MessageOverhead is the per-message framing token count (role, delimiters).
const MessageOverhead = messageOverhead

// Estimate returns an approximate token count for a single string.
func Estimate(s string) int {
	return int(float64(len(s))/charsPerToken) + 1
}
