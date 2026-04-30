package codientcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const cursorRulesDir = ".cursor/rules"

var ruleStemRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// validRuleStem checks a safe filename stem for a Cursor rule (.mdc), without path segments.
func validRuleStem(s string) bool {
	if len(s) == 0 || len(s) > 80 {
		return false
	}
	return ruleStemRE.MatchString(s)
}

// buildCursorRuleMdc returns the full contents of a Cursor-style rule file (.mdc YAML frontmatter + body).
func buildCursorRuleMdc(description string, alwaysApply bool, globs string, title string, body string) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = "Project rule."
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Rule"
	}
	body = strings.TrimRight(strings.TrimSpace(body), "\r")

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: |\n")
	for _, line := range strings.Split(desc, "\n") {
		b.WriteString("  ")
		b.WriteString(strings.TrimRight(line, "\r"))
		b.WriteString("\n")
	}
	if !alwaysApply {
		g := strings.TrimSpace(globs)
		if g == "" {
			g = "**/*"
		}
		fmt.Fprintf(&b, "globs: %s\n", yamlDoubleQuoted(g))
	}
	fmt.Fprintf(&b, "alwaysApply: %t\n", alwaysApply)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", title)
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	} else {
		b.WriteString("## Guidelines\n\n- \n")
	}
	return b.String()
}

func yamlDoubleQuoted(s string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&out, "\\u%04x", r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

func (s *session) runCreateRuleWizard(sc *bufio.Scanner) {
	fmt.Fprintf(os.Stderr, "\n  ** Create a Cursor-style project rule **\n\n")
	fmt.Fprintf(os.Stderr, "  Writes an .mdc file under <workspace>/%s (same layout as Cursor).\n", cursorRulesDir)
	fmt.Fprintf(os.Stderr, "  Codient does not load these into the CLI prompt; they apply in Cursor and compatible tools.\n\n")

	ws := strings.TrimSpace(s.cfg.EffectiveWorkspace())
	if ws == "" {
		fmt.Fprintf(os.Stderr, "  No workspace set. Use /config workspace <path> or /workspace <path> first.\n\n")
		return
	}
	absWs, err := filepath.Abs(ws)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  workspace: %v\n\n", err)
		return
	}

	desc := strings.TrimSpace(promptWithDefault(sc, "  Short description (what this rule enforces; YAML block after description: |)", ""))
	if desc == "" {
		fmt.Fprintf(os.Stderr, "  Aborted (empty description).\n\n")
		return
	}

	fmt.Fprintf(os.Stderr, "\n  When should this rule apply?\n")
	fmt.Fprintf(os.Stderr, "    1) Always — every session for this project (alwaysApply: true)\n")
	fmt.Fprintf(os.Stderr, "    2) When matching files are in play — set a glob (e.g. **/*.go, **/*.tsx)\n\n")
	scope := strings.TrimSpace(strings.ToLower(promptWithDefault(sc, "  Enter 1 or 2", "1")))
	var alwaysApply bool
	var globs string
	switch scope {
	case "1", "a", "always", "y", "yes", "":
		alwaysApply = true
	case "2", "g", "glob", "files", "f":
		alwaysApply = false
		globs = strings.TrimSpace(promptWithDefault(sc, "  Glob pattern", "**/*"))
		if globs == "" {
			globs = "**/*"
		}
	default:
		fmt.Fprintf(os.Stderr, "  Invalid choice.\n\n")
		return
	}

	var stem string
	for {
		stem = strings.TrimSpace(strings.ToLower(promptWithDefault(sc, "  File name stem (lowercase, hyphens; e.g. go-style, api-errors)", "")))
		if stem == "" {
			fmt.Fprintf(os.Stderr, "  Aborted.\n\n")
			return
		}
		if !validRuleStem(stem) {
			fmt.Fprintf(os.Stderr, "  Invalid stem: use a-z, 0-9, single hyphens between segments; must start with a letter.\n")
			continue
		}
		break
	}

	rulesDir := filepath.Join(absWs, filepath.FromSlash(cursorRulesDir))
	outPath := filepath.Join(rulesDir, stem+".mdc")
	if _, err := os.Stat(outPath); err == nil {
		fmt.Fprintf(os.Stderr, "  Already exists: %s — pick another stem.\n\n", outPath)
		return
	}

	title := strings.TrimSpace(promptWithDefault(sc, "  Markdown title (H1 text)", strings.ReplaceAll(stem, "-", " ")))
	body := strings.TrimSpace(promptWithDefault(sc, "  Optional starter body (markdown after the title; leave empty for a stub)", ""))

	content := buildCursorRuleMdc(desc, alwaysApply, globs, title, body)
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  mkdir: %v\n\n", err)
		return
	}
	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  write: %v\n\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "\n  Wrote %s\n", outPath)
	fmt.Fprintf(os.Stderr, "  Edit the file to refine. Open the project in Cursor to use the rule.\n\n")
}
