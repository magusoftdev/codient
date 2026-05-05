package codeextract

import (
	"fmt"
	"strings"
)

const maxHeuristicLinesPerDecl = 48

// outlineHeuristic extracts declaration-like blocks from source using brace/paren
// balancing from anchor lines. It is not a full AST; dense enough for small models.
func outlineHeuristic(lang, relPath, content string) (string, error) {
	tags := extractAnchorLines(lang, content)
	if len(tags) == 0 {
		return "(no top-level declarations matched for outline; use view=full)", nil
	}
	lines := strings.Split(content, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "// read_file outline (heuristic %s): %s\n\n", lang, relPath)
	for _, a := range tags {
		block := sliceBalancedDecl(lines, a.lineIndex, maxHeuristicLinesPerDecl)
		if strings.TrimSpace(block) == "" {
			continue
		}
		b.WriteString(strings.TrimRight(block, "\n"))
		b.WriteString("\n\n")
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "(no extractable declarations; use view=full)", nil
	}
	return out, nil
}

type anchor struct {
	lineIndex int // 0-based
}

func extractAnchorLines(lang, content string) []anchor {
	lines := strings.Split(content, "\n")
	var out []anchor
	for i, line := range lines {
		trim := strings.TrimLeft(line, " \t")
		if isSkippableLine(lang, trim) {
			continue
		}
		if !isTopLevelLine(line) {
			continue
		}
		if anchorLine(lang, trim) {
			out = append(out, anchor{lineIndex: i})
		}
	}
	return out
}

func isSkippableLine(lang, trim string) bool {
	if trim == "" {
		return true
	}
	switch lang {
	case "python":
		if strings.HasPrefix(trim, "#") {
			return true
		}
	case "rust", "go":
		if strings.HasPrefix(trim, "//") {
			return true
		}
	case "java", "c", "cpp":
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") {
			return true
		}
	case "typescript", "javascript":
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") {
			return true
		}
	}
	return false
}

func isTopLevelLine(line string) bool {
	if len(line) == 0 {
		return false
	}
	r := rune(line[0])
	return r != ' ' && r != '\t'
}

func anchorLine(lang, trim string) bool {
	switch lang {
	case "python":
		return strings.HasPrefix(trim, "class ") ||
			strings.HasPrefix(trim, "def ") ||
			strings.HasPrefix(trim, "async def ")
	case "typescript", "javascript":
		t := trim
		if after, ok := strings.CutPrefix(t, "export "); ok {
			t = strings.TrimLeft(after, " \t")
		}
		return strings.HasPrefix(t, "class ") ||
			strings.HasPrefix(t, "interface ") ||
			strings.HasPrefix(t, "type ") ||
			strings.HasPrefix(t, "async function ") ||
			strings.HasPrefix(t, "function ") ||
			(strings.HasPrefix(t, "const ") && strings.Contains(t, "("))
	case "rust":
		return strings.HasPrefix(trim, "pub fn ") ||
			strings.HasPrefix(trim, "fn ") ||
			strings.HasPrefix(trim, "pub struct ") ||
			strings.HasPrefix(trim, "struct ") ||
			strings.HasPrefix(trim, "pub enum ") ||
			strings.HasPrefix(trim, "enum ") ||
			strings.HasPrefix(trim, "pub trait ") ||
			strings.HasPrefix(trim, "trait ") ||
			strings.HasPrefix(trim, "impl") ||
			strings.HasPrefix(trim, "pub mod ")
	case "java":
		return strings.Contains(trim, "class ") || strings.Contains(trim, "interface ") || strings.Contains(trim, "enum ")
	case "c", "cpp":
		return strings.HasPrefix(trim, "class ") || strings.HasPrefix(trim, "struct ")
	default:
		return false
	}
}

// sliceBalancedDecl collects lines starting at startIdx until { } counts balance to 0
// after having seen '{', or for Python until dedent from function/class body heuristic:
// we use brace for C-family; for Python use indent of first logical line after def/class.
func sliceBalancedDecl(lines []string, startIdx, maxLines int) string {
	if startIdx < 0 || startIdx >= len(lines) {
		return ""
	}
	// Python: grab block by indentation
	first := strings.TrimRight(lines[startIdx], "\r")
	ft := strings.TrimLeft(first, " \t")
	if strings.HasPrefix(ft, "class ") || strings.HasPrefix(ft, "def ") || strings.HasPrefix(ft, "async def ") {
		baseIndent := countIndent(first)
		var parts []string
		parts = append(parts, first)
		for i := startIdx + 1; i < len(lines) && len(parts) < maxLines; i++ {
			line := strings.TrimRight(lines[i], "\r")
			trim := strings.TrimLeft(line, " \t")
			if trim == "" {
				parts = append(parts, line)
				continue
			}
			ind := countIndent(line)
			if ind <= baseIndent {
				break
			}
			parts = append(parts, line)
		}
		return strings.Join(parts, "\n")
	}

	// Brace languages: balance { }
	depth := 0
	started := false
	var parts []string
	for i := startIdx; i < len(lines) && len(parts) < maxLines; i++ {
		line := strings.TrimRight(lines[i], "\r")
		parts = append(parts, line)
		for _, r := range stripStringsAndRunes(line) {
			switch r {
			case '{':
				depth++
				started = true
			case '}':
				if started {
					depth--
				}
			}
		}
		if started && depth <= 0 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

func countIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// stripStringsAndRunes removes quoted runs so braces inside strings do not count.
// Best-effort for outline only.
func stripStringsAndRunes(line string) string {
	var b strings.Builder
	inSingle, inDouble, inBack := false, false, false
	escape := false
	for _, r := range line {
		if escape {
			escape = false
			continue
		}
		if r == '\\' && (inSingle || inDouble || inBack) {
			escape = true
			continue
		}
		switch {
		case inBack:
			if r == '`' {
				inBack = false
			}
			continue
		case inSingle:
			if r == '\'' {
				inSingle = false
			}
			continue
		case inDouble:
			if r == '"' {
				inDouble = false
			}
			continue
		}
		switch r {
		case '`':
			inBack = true
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		default:
			if r == '{' || r == '}' {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
