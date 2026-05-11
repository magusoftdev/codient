package codientcli

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// parsedFailure holds structured output from parsing an auto-check step's
// combined stdout/stderr.
type parsedFailure struct {
	// Signature is a stable, canonical hash of the failing items. When two
	// consecutive attempts produce the same Signature the fix loop considers
	// the model stuck ("no progress").
	Signature string
	// Highlights are short summaries of individual failures (test name,
	// file:line, compiler error) extracted by the parser — at most
	// maxHighlights entries. They are appended under a header in the injected
	// user message to help the model focus.
	Highlights []string
}

const maxHighlights = 8

// stepParser extracts structured failure data from a single auto-check step.
type stepParser interface {
	Parse(label, cmd, body string, exitCode int) parsedFailure
}

// selectParser picks the most specific parser for a step based on the command.
func selectParser(label, cmdLine string) stepParser {
	cl := strings.ToLower(cmdLine)
	switch {
	case strings.Contains(cl, "go test") || strings.Contains(cl, "go build") || strings.Contains(cl, "go vet"):
		return goTestParser{}
	default:
		return opaqueParser{}
	}
}

// --- opaque (default) parser -------------------------------------------------

// opaqueParser hashes the raw output body as a signature and extracts the
// first N non-empty lines as highlights. This works for any language.
type opaqueParser struct{}

func (opaqueParser) Parse(_, _, body string, _ int) parsedFailure {
	sig := opaqueSignature(body)
	return parsedFailure{
		Signature:  sig,
		Highlights: firstNonEmptyLines(body, maxHighlights),
	}
}

func opaqueSignature(body string) string {
	normalized := strings.TrimSpace(body)
	const maxSigInput = 16 * 1024
	if len(normalized) > maxSigInput {
		normalized = normalized[:maxSigInput]
	}
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h[:8])
}

func firstNonEmptyLines(body string, n int) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= n {
			break
		}
	}
	return out
}

// --- Go test parser ----------------------------------------------------------

var (
	goTestFailRe = regexp.MustCompile(`^--- FAIL: (\S+)`)
	goFileLine   = regexp.MustCompile(`^\s+(\S+\.go:\d+):`)
	goPkgFail    = regexp.MustCompile(`^FAIL\s+(\S+)`)
)

// goTestParser extracts failing test names, file:line locations, and failing
// packages from `go test` / `go build` / `go vet` output.
type goTestParser struct{}

func (goTestParser) Parse(_, _, body string, _ int) parsedFailure {
	type key struct{ pkg, test, loc string }
	var keys []key
	var highlights []string

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if m := goTestFailRe.FindStringSubmatch(line); m != nil {
			testName := m[1]
			loc := ""
			for j := i - 1; j >= 0 && j >= i-5; j-- {
				if lm := goFileLine.FindStringSubmatch(lines[j]); lm != nil {
					loc = lm[1]
					break
				}
			}
			keys = append(keys, key{test: testName, loc: loc})
			hl := "FAIL: " + testName
			if loc != "" {
				hl += " (" + loc + ")"
			}
			highlights = append(highlights, hl)
		}
		if m := goPkgFail.FindStringSubmatch(line); m != nil {
			keys = append(keys, key{pkg: m[1]})
		}
	}

	if len(keys) == 0 {
		return opaqueParser{}.Parse("", "", body, 0)
	}

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k.pkg + "|" + k.test + "|" + k.loc
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	sig := fmt.Sprintf("go:%x", h[:8])

	if len(highlights) > maxHighlights {
		highlights = highlights[:maxHighlights]
	}

	return parsedFailure{
		Signature:  sig,
		Highlights: highlights,
	}
}
