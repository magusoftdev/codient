package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

const (
	defaultAstGrepMax = 50
	maxAstGrepMax     = 200
)

// registerAstGrepTools registers the find_references tool if sgPath is non-empty.
func registerAstGrepTools(r *Registry, root string, sgPath string) {
	if sgPath == "" {
		return
	}
	r.Register(Tool{
		Name: "find_references",
		Description: "Structural code search via ast-grep. Finds all references to a symbol " +
			"(function calls, type usages, variable references) using AST parsing. " +
			"More precise than grep for understanding call chains and verifying who uses a function. " +
			"Requires a symbol name; optionally specify language and path scope.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"symbol": map[string]any{
					"type":        "string",
					"description": "The function, type, or variable name to find references for.",
				},
				"lang": map[string]any{
					"type":        "string",
					"description": "Language (e.g. go, python, typescript, rust, java, c, cpp). Auto-detected from workspace if omitted.",
				},
				"path_prefix": map[string]any{
					"type":        "string",
					"description": "Optional subdirectory to scope the search.",
				},
				"max_matches": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum results (default %d, max %d).", defaultAstGrepMax, maxAstGrepMax),
				},
			},
			"required":             []string{"symbol"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Symbol     string `json:"symbol"`
				Lang       string `json:"lang"`
				PathPrefix string `json:"path_prefix"`
				MaxMatches int    `json:"max_matches"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			sym := strings.TrimSpace(p.Symbol)
			if sym == "" {
				return "", fmt.Errorf("symbol is required")
			}
			max := p.MaxMatches
			if max <= 0 {
				max = defaultAstGrepMax
			}
			if max > maxAstGrepMax {
				max = maxAstGrepMax
			}
			lang := strings.TrimSpace(p.Lang)
			if lang == "" {
				lang = detectLang(root)
			}
			if lang == "" {
				return "", fmt.Errorf("could not auto-detect language; please provide the lang parameter")
			}

			searchRoot := root
			if prefix := strings.TrimSpace(p.PathPrefix); prefix != "" {
				candidate, err := absUnderRoot(root, prefix)
				if err != nil {
					return "", err
				}
				searchRoot = candidate
			}

			return runAstGrep(ctx, sgPath, searchRoot, root, sym, lang, max)
		},
	})
}

func runAstGrep(ctx context.Context, sgPath, searchRoot, workspaceRoot, symbol, lang string, maxMatches int) (string, error) {
	pattern := symbol
	args := []string{
		"run",
		"--lang", lang,
		"-p", pattern,
		"--json=compact",
		searchRoot,
	}

	cmd := exec.CommandContext(ctx, sgPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && stdout.Len() == 0 {
			return "(no matches)", nil
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("ast-grep: %s", errMsg)
		}
		return "", fmt.Errorf("ast-grep: %w", err)
	}

	output := stdout.String()
	if strings.TrimSpace(output) == "" || strings.TrimSpace(output) == "[]" {
		return "(no matches)", nil
	}

	return formatAstGrepOutput(output, workspaceRoot, maxMatches)
}

type astGrepMatch struct {
	File  string `json:"file"`
	Range struct {
		Start struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"start"`
	} `json:"range"`
	Lines string `json:"lines"`
}

func formatAstGrepOutput(jsonOutput, workspaceRoot string, maxMatches int) (string, error) {
	var matches []astGrepMatch
	if err := json.Unmarshal([]byte(jsonOutput), &matches); err != nil {
		trimmed := strings.TrimSpace(jsonOutput)
		if len(trimmed) > 500 {
			trimmed = trimmed[:500] + "..."
		}
		return trimmed, nil
	}

	if len(matches) == 0 {
		return "(no matches)", nil
	}

	var b strings.Builder
	count := 0
	for _, m := range matches {
		if count >= maxMatches {
			break
		}
		rel, err := filepath.Rel(workspaceRoot, m.File)
		if err != nil {
			rel = m.File
		}
		rel = filepath.ToSlash(rel)
		line := strings.TrimRight(m.Lines, "\n\r")
		lineNo := m.Range.Start.Line + 1
		fmt.Fprintf(&b, "%s:%d:%s\n", rel, lineNo, line)
		count++
	}

	out := strings.TrimSuffix(b.String(), "\n")
	if count >= maxMatches && len(matches) > maxMatches {
		out += fmt.Sprintf("\n\n[truncated at %d matches]", maxMatches)
	}
	return out, nil
}

// detectLang infers the primary language from workspace marker files.
func detectLang(root string) string {
	markers := []struct {
		file string
		lang string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"setup.py", "python"},
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(root, m.file)); err == nil {
			return m.lang
		}
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err == nil {
			return "typescript"
		}
		return "javascript"
	}
	return ""
}
