package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

const memoryFileName = "memory.md"

// MemoryOptions configures the memory_update tool.
type MemoryOptions struct {
	StateDir      string // global state directory (~/.codient)
	WorkspaceRoot string // workspace root for project-scoped memory
}

func registerMemoryUpdate(r *Registry, opts *MemoryOptions) {
	if opts == nil {
		return
	}

	r.Register(Tool{
		Name: "memory_update",
		Description: "Update cross-session memory that persists across codient sessions. " +
			"Use this to record project conventions, user preferences, architecture decisions, and patterns discovered during this session. " +
			"Global memory (~/.codient/memory.md) applies to all projects; workspace memory (<workspace>/.codient/memory.md) applies to the current project only. " +
			"Keep entries concise — use bullet points under markdown headings. Do not store secrets or ephemeral information.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"description": "Where to store the memory: \"global\" for cross-project preferences, \"workspace\" for project-specific conventions.",
					"enum":        []string{"global", "workspace"},
				},
				"action": map[string]any{
					"type":        "string",
					"description": "How to update: \"append\" adds to the end of the file, \"replace_section\" replaces a ## heading section (or creates it if missing).",
					"enum":        []string{"append", "replace_section"},
				},
				"section": map[string]any{
					"type":        "string",
					"description": "Heading name for replace_section (without the ## prefix). Required when action is replace_section.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Markdown content to write. For replace_section, this replaces the section body (content after the heading until the next ## or EOF).",
				},
			},
			"required":             []string{"scope", "action", "content"},
			"additionalProperties": false,
		},
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Scope   string `json:"scope"`
				Action  string `json:"action"`
				Section string `json:"section"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}

			path, err := resolveMemoryPath(opts, p.Scope)
			if err != nil {
				return "", err
			}

			switch p.Action {
			case "append":
				return memoryAppend(path, p.Content)
			case "replace_section":
				if strings.TrimSpace(p.Section) == "" {
					return "", fmt.Errorf("section is required when action is replace_section")
				}
				return memoryReplaceSection(path, p.Section, p.Content)
			default:
				return "", fmt.Errorf("unknown action %q; use \"append\" or \"replace_section\"", p.Action)
			}
		},
	})
}

func resolveMemoryPath(opts *MemoryOptions, scope string) (string, error) {
	switch scope {
	case "global":
		if opts.StateDir == "" {
			return "", fmt.Errorf("global state directory not configured")
		}
		return filepath.Join(opts.StateDir, memoryFileName), nil
	case "workspace":
		if opts.WorkspaceRoot == "" {
			return "", fmt.Errorf("no workspace set")
		}
		return filepath.Join(opts.WorkspaceRoot, ".codient", memoryFileName), nil
	default:
		return "", fmt.Errorf("unknown scope %q; use \"global\" or \"workspace\"", scope)
	}
}

func memoryAppend(path, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing memory: %w", err)
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(strings.TrimSpace(content))
	b.WriteByte('\n')

	if err := atomicWrite(path, b.String()); err != nil {
		return "", err
	}

	action := "appended to"
	if len(existing) == 0 {
		action = "created"
	}
	return fmt.Sprintf("%s %s", action, path), nil
}

func memoryReplaceSection(path, section, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing memory: %w", err)
	}

	heading := "## " + strings.TrimSpace(section)
	lines := strings.Split(string(existing), "\n")
	sectionStart := -1
	sectionEnd := len(lines)

	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			sectionStart = i
			for j := i + 1; j < len(lines); j++ {
				trimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(trimmed, "## ") {
					sectionEnd = j
					break
				}
			}
			break
		}
	}

	var result string
	if sectionStart >= 0 {
		var b strings.Builder
		for _, line := range lines[:sectionStart] {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString(heading)
		b.WriteByte('\n')
		b.WriteByte('\n')
		b.WriteString(strings.TrimSpace(content))
		b.WriteByte('\n')
		if sectionEnd < len(lines) {
			b.WriteByte('\n')
			for i, line := range lines[sectionEnd:] {
				b.WriteString(line)
				if i < len(lines[sectionEnd:])-1 {
					b.WriteByte('\n')
				}
			}
		}
		result = b.String()
	} else {
		var b strings.Builder
		if len(existing) > 0 {
			b.Write(existing)
			if !strings.HasSuffix(string(existing), "\n") {
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
		}
		b.WriteString(heading)
		b.WriteByte('\n')
		b.WriteByte('\n')
		b.WriteString(strings.TrimSpace(content))
		b.WriteByte('\n')
		result = b.String()
	}

	if err := atomicWrite(path, result); err != nil {
		return "", err
	}

	action := "replaced"
	if sectionStart < 0 {
		action = "added"
	}
	return fmt.Sprintf("%s section %q in %s", action, section, path), nil
}

func atomicWrite(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
