package agent

import (
	"fmt"
	"strings"
	"time"

	"codient/internal/agentlog"
)

// formatProgressDur renders a duration for stderr progress (e.g. 420ms, 4.8s).
func formatProgressDur(d time.Duration) string {
	d = d.Round(time.Millisecond)
	if d < time.Second {
		ms := d.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		return fmt.Sprintf("%dms", ms)
	}
	s := d.Seconds()
	if s < 60 {
		if s < 10 {
			return fmt.Sprintf("%.2fs", s)
		}
		return fmt.Sprintf("%.1fs", s)
	}
	return d.Round(time.Second).String()
}

// ProgressToolCompact is a short label for progress (no path= prefixes).
func ProgressToolCompact(toolName string, argsJSON []byte) string {
	sum := agentlog.SummarizeArgs(toolName, argsJSON)
	switch toolName {
	case "read_file", "write_file", "str_replace", "patch_file", "ensure_dir", "path_stat", "remove_path":
		if p, ok := sum["path"].(string); ok && p != "" {
			return toolName + " " + p
		}
		return toolName
	case "move_path", "copy_path":
		f, ok1 := sum["from"].(string)
		t, ok2 := sum["to"].(string)
		if ok1 && ok2 && f != "" && t != "" {
			return toolName + " " + f + " → " + t
		}
		return toolName
	case "glob_files":
		if pat, ok := sum["pattern"].(string); ok && pat != "" {
			return "glob_files " + truncateRunes(pat, 40)
		}
		return "glob_files"
	case "fetch_url":
		if u, ok := sum["url"].(string); ok && u != "" {
			return "fetch_url " + truncateRunes(u, 48)
		}
		return "fetch_url"
	case "web_search":
		if q, ok := sum["query"].(string); ok && q != "" {
			return "web_search " + truncateRunes(q, 48)
		}
		return "web_search"
	case "list_dir":
		if p, ok := sum["path"].(string); ok && strings.TrimSpace(p) != "" && p != "." {
			return "list_dir " + p
		}
		return "list_dir"
	case "grep":
		if p, ok := sum["pattern"].(string); ok && p != "" {
			return "grep " + truncateRunes(p, 40)
		}
		return "grep"
	case "search_files":
		var bits []string
		if v, ok := sum["substring"]; ok && fmt.Sprint(v) != "" {
			bits = append(bits, fmt.Sprint(v))
		}
		if v, ok := sum["suffix"]; ok && fmt.Sprint(v) != "" {
			bits = append(bits, fmt.Sprint(v))
		}
		if len(bits) == 0 {
			return "search_files"
		}
		return "search_files " + strings.Join(bits, " ")
	case "run_command":
		if argv, ok := sum["argv"].([]string); ok && len(argv) > 0 {
			s := strings.Join(argv, " ")
			if len(s) > 50 {
				s = s[:50] + "…"
			}
			return "run " + s
		}
		return "run_command"
	case "run_shell":
		if s, ok := sum["command_prefix"].(string); ok && s != "" {
			if len(s) > 50 {
				s = s[:50] + "…"
			}
			return "shell " + s
		}
		return "run_shell"
	case "get_time":
		return "get_time"
	case "echo":
		if msg, ok := sum["message"].(string); ok && msg != "" {
			return "echo " + truncateRunes(msg, 36)
		}
		return "echo"
	default:
		return ProgressToolLine(toolName, argsJSON)
	}
}

// progressToolActionPhrase is a short natural-language description of work about to start.
func progressToolActionPhrase(toolName string, argsJSON []byte, sum map[string]any) string {
	switch toolName {
	case "web_search":
		if q, ok := sum["query"].(string); ok && q != "" {
			return fmt.Sprintf("searching the web for %q", truncateRunes(q, 64))
		}
		return "searching the web"
	case "fetch_url":
		if u, ok := sum["url"].(string); ok && u != "" {
			return fmt.Sprintf("fetching %s", truncateRunes(u, 64))
		}
		return "fetching a URL"
	case "read_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("reading %s", p)
		}
		return "reading a file"
	case "write_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("writing %s", p)
		}
		return "writing a file"
	case "str_replace":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("editing %s (str_replace)", p)
		}
		return "editing a file (str_replace)"
	case "patch_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("patching %s", p)
		}
		return "patching a file"
	case "ensure_dir":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("ensuring directory %s", p)
		}
		return "ensuring a directory"
	case "path_stat":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("stat %s", p)
		}
		return "path stat"
	case "remove_path":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("removing %s", p)
		}
		return "removing a path"
	case "move_path":
		f, ok1 := sum["from"].(string)
		t, ok2 := sum["to"].(string)
		if ok1 && ok2 && f != "" && t != "" {
			return fmt.Sprintf("moving %s → %s", f, t)
		}
		return "moving a path"
	case "copy_path":
		f, ok1 := sum["from"].(string)
		t, ok2 := sum["to"].(string)
		if ok1 && ok2 && f != "" && t != "" {
			return fmt.Sprintf("copying %s → %s", f, t)
		}
		return "copying a path"
	case "glob_files":
		if pat, ok := sum["pattern"].(string); ok && pat != "" {
			return fmt.Sprintf("glob %s", truncateRunes(pat, 48))
		}
		return "glob files"
	case "list_dir":
		if p, ok := sum["path"].(string); ok && strings.TrimSpace(p) != "" && p != "." {
			return fmt.Sprintf("listing %s", p)
		}
		return "listing directory"
	case "grep":
		if p, ok := sum["pattern"].(string); ok && p != "" {
			return fmt.Sprintf("grep %q", truncateRunes(p, 48))
		}
		return "grep"
	case "search_files":
		return ProgressToolCompact(toolName, argsJSON)
	case "run_command":
		if argv, ok := sum["argv"].([]string); ok && len(argv) > 0 {
			s := strings.Join(argv, " ")
			return "running " + truncateRunes(s, 56)
		}
		return "running a command"
	case "run_shell":
		if s, ok := sum["command_prefix"].(string); ok && s != "" {
			return "running shell " + truncateRunes(s, 56)
		}
		return "running shell"
	case "get_time":
		return "reading current time"
	case "echo":
		return ProgressToolCompact(toolName, argsJSON)
	default:
		return ProgressToolCompact(toolName, argsJSON)
	}
}

// progressToolFirstPersonPhrase describes the tool call in the agent's voice (REPL turns).
func progressToolFirstPersonPhrase(toolName string, argsJSON []byte, sum map[string]any) string {
	switch toolName {
	case "web_search":
		if q, ok := sum["query"].(string); ok && q != "" {
			return fmt.Sprintf("I'll search the web for %q", truncateRunes(q, 64))
		}
		return "I'll run a web search"
	case "fetch_url":
		if u, ok := sum["url"].(string); ok && u != "" {
			return fmt.Sprintf("I'll fetch %s", truncateRunes(u, 64))
		}
		return "I'll fetch a URL"
	case "read_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll read %s", p)
		}
		return "I'll read a file"
	case "write_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll write %s", p)
		}
		return "I'll write a file"
	case "str_replace":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll edit %s", p)
		}
		return "I'll edit a file"
	case "patch_file":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll patch %s", p)
		}
		return "I'll apply a patch"
	case "ensure_dir":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll ensure the directory %s exists", p)
		}
		return "I'll create a directory if needed"
	case "path_stat":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll inspect %s", p)
		}
		return "I'll inspect a path"
	case "remove_path":
		if p, ok := sum["path"].(string); ok && p != "" {
			return fmt.Sprintf("I'll remove %s", p)
		}
		return "I'll remove a path"
	case "move_path":
		f, ok1 := sum["from"].(string)
		t, ok2 := sum["to"].(string)
		if ok1 && ok2 && f != "" && t != "" {
			return fmt.Sprintf("I'll move %s to %s", f, t)
		}
		return "I'll move a path"
	case "copy_path":
		f, ok1 := sum["from"].(string)
		t, ok2 := sum["to"].(string)
		if ok1 && ok2 && f != "" && t != "" {
			return fmt.Sprintf("I'll copy %s to %s", f, t)
		}
		return "I'll copy a path"
	case "glob_files":
		if pat, ok := sum["pattern"].(string); ok && pat != "" {
			return fmt.Sprintf("I'll glob for %s", truncateRunes(pat, 48))
		}
		return "I'll list matching paths"
	case "list_dir":
		if p, ok := sum["path"].(string); ok && strings.TrimSpace(p) != "" && p != "." {
			return fmt.Sprintf("I'll list %s", p)
		}
		return "I'll list the workspace"
	case "grep":
		if p, ok := sum["pattern"].(string); ok && p != "" {
			return fmt.Sprintf("I'll grep the tree for %q", truncateRunes(p, 48))
		}
		return "I'll grep the codebase"
	case "search_files":
		return "I'll search files in the project"
	case "run_command":
		if argv, ok := sum["argv"].([]string); ok && len(argv) > 0 {
			s := strings.Join(argv, " ")
			return "I'll run " + truncateRunes(s, 56)
		}
		return "I'll run a command"
	case "run_shell":
		if s, ok := sum["command_prefix"].(string); ok && s != "" {
			return "I'll run a shell snippet: " + truncateRunes(s, 56)
		}
		return "I'll run a shell command"
	case "get_time":
		return "I'll check the current time"
	case "echo":
		return "I'll leave a short note (echo)"
	default:
		return "I'll use " + ProgressToolCompact(toolName, argsJSON)
	}
}

// ProgressToolIntentLine is printed immediately before a tool runs so the user sees
// activity while the tool is in flight. fromUserTurn selects first-person agent phrasing
// (REPL); otherwise a neutral participle phrase is used (e.g. headless / API callers).
func ProgressToolIntentLine(toolName string, argsJSON []byte, fromUserTurn bool) string {
	sum := agentlog.SummarizeArgs(toolName, argsJSON)
	var phrase string
	if fromUserTurn {
		phrase = progressToolFirstPersonPhrase(toolName, argsJSON, sum)
	} else {
		phrase = progressToolActionPhrase(toolName, argsJSON, sum)
	}
	return fmt.Sprintf("  ▸ %s…", phrase)
}

// formatThinkingLine extracts a short reasoning summary from assistant content
// that accompanies tool calls. Many models emit a brief explanation of what
// they're about to do alongside tool_calls; this surfaces it on the progress
// writer so the user can follow along.
func formatThinkingLine(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	// For XML-style tool calls, strip the tool markup to get just the prose.
	if strings.Contains(s, "<function=") {
		s = reToolCallMarkup.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
	}
	if s == "" {
		return ""
	}
	// Take just the first meaningful line/sentence for brevity.
	lines := strings.SplitN(s, "\n", 4)
	var out []string
	total := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if total+len(line) > 200 {
			if total == 0 {
				r := []rune(line)
				if len(r) > 200 {
					line = string(r[:197]) + "..."
				}
				out = append(out, line)
			}
			break
		}
		out = append(out, line)
		total += len(line)
	}
	return strings.Join(out, " ")
}

func progressErrShort(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = strings.TrimSpace(strings.Split(s, "\n")[0])
	if len(s) > 72 {
		return s[:72] + "…"
	}
	return s
}
