package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codient/internal/config"
	"codient/internal/lspclient"

	"github.com/openai/openai-go/v3/shared"
)

// LSPClient is implemented by lspclient.Manager to avoid a circular import.
type LSPClient interface {
	PickServer(file string, languageOverride string, lspCfg map[string]config.LSPServerConfig) interface{}
	Definition(ctx context.Context, sc interface{}, absPath string, line, char int) ([]lspclient.Location, error)
	TypeDefinition(ctx context.Context, sc interface{}, absPath string, line, char int) ([]lspclient.Location, error)
	Implementation(ctx context.Context, sc interface{}, absPath string, line, char int) ([]lspclient.Location, error)
	References(ctx context.Context, sc interface{}, absPath string, line, char int) ([]lspclient.Location, error)
	Hover(ctx context.Context, sc interface{}, absPath string, line, char int) (*lspclient.HoverResult, error)
	DocumentSymbols(ctx context.Context, sc interface{}, absPath string) ([]lspclient.SymbolInformation, error)
	WorkspaceSymbols(ctx context.Context, sc interface{}, query string) ([]lspclient.SymbolInformation, error)
	Rename(ctx context.Context, sc interface{}, absPath string, line, char int, newName string) (*lspclient.WorkspaceEdit, error)
}

// RegisterLSPReadTools registers read-only LSP tools into the registry.
func RegisterLSPReadTools(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	if client == nil || len(lspCfg) == 0 {
		return
	}
	registerLSPDefinition(r, root, client, lspCfg)
	registerLSPReferences(r, root, client, lspCfg)
	registerLSPHover(r, root, client, lspCfg)
	registerLSPTypeDefinition(r, root, client, lspCfg)
	registerLSPImplementation(r, root, client, lspCfg)
	registerLSPDocumentSymbols(r, root, client, lspCfg)
	registerLSPWorkspaceSymbols(r, root, client, lspCfg)
}

// RegisterLSPMutatingTools registers write-mode-only LSP tools.
func RegisterLSPMutatingTools(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	if client == nil || len(lspCfg) == 0 {
		return
	}
	registerLSPRename(r, root, client, lspCfg)
}

var positionSchema = map[string]any{
	"file": map[string]any{
		"type":        "string",
		"description": "File path relative to workspace root.",
	},
	"line": map[string]any{
		"type":        "integer",
		"description": "1-based line number.",
	},
	"character": map[string]any{
		"type":        "integer",
		"description": "1-based column number.",
	},
	"language": map[string]any{
		"type":        "string",
		"description": "Optional: language server ID to use (e.g. \"go\", \"python\"). If omitted, selected by file extension.",
	},
}

func resolveLSP(root string, file string, language string, client LSPClient, lspCfg map[string]config.LSPServerConfig) (absPath string, sc interface{}, err error) {
	absPath, err = absUnderRoot(root, file)
	if err != nil {
		return "", nil, fmt.Errorf("path error: %w", err)
	}
	sc = client.PickServer(file, language, lspCfg)
	if sc == nil {
		return "", nil, fmt.Errorf("no LSP server configured for %s (configure lsp_servers in ~/.codient/config.json)", file)
	}
	return absPath, sc, nil
}

func registerLSPDefinition(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name: "lsp_definition",
		Description: "Jump to the definition of a symbol at a position using a language server. " +
			"Returns file path, line, and column of the definition.",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           positionSchema,
			"required":             []string{"file", "line", "character"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			locs, err := client.Definition(ctx, sc, absPath, p.Line-1, p.Character-1)
			if err != nil {
				return "", err
			}
			return formatLocations(root, locs), nil
		},
	})
}

func registerLSPReferences(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name: "lsp_references",
		Description: "Find all references to a symbol at a position using a language server. " +
			"Returns a list of file paths, lines, and columns.",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           positionSchema,
			"required":             []string{"file", "line", "character"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			locs, err := client.References(ctx, sc, absPath, p.Line-1, p.Character-1)
			if err != nil {
				return "", err
			}
			return formatLocations(root, locs), nil
		},
	})
}

func registerLSPHover(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name:        "lsp_hover",
		Description: "Get hover information (type, documentation) for a symbol at a position using a language server.",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           positionSchema,
			"required":             []string{"file", "line", "character"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			result, err := client.Hover(ctx, sc, absPath, p.Line-1, p.Character-1)
			if err != nil {
				return "", err
			}
			if result == nil || result.Contents.Value == "" {
				return "No hover information available at this position.", nil
			}
			return result.Contents.Value, nil
		},
	})
}

func registerLSPTypeDefinition(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name: "lsp_type_definition",
		Description: "Jump to the type definition of a symbol at a position using a language server. " +
			"Returns file path, line, and column of the type.",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           positionSchema,
			"required":             []string{"file", "line", "character"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			locs, err := client.TypeDefinition(ctx, sc, absPath, p.Line-1, p.Character-1)
			if err != nil {
				return "", err
			}
			return formatLocations(root, locs), nil
		},
	})
}

func registerLSPImplementation(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name:        "lsp_implementation",
		Description: "Find implementations of an interface or abstract method at a position using a language server.",
		Parameters: shared.FunctionParameters{
			"type":                 "object",
			"properties":           positionSchema,
			"required":             []string{"file", "line", "character"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			locs, err := client.Implementation(ctx, sc, absPath, p.Line-1, p.Character-1)
			if err != nil {
				return "", err
			}
			return formatLocations(root, locs), nil
		},
	})
}

func registerLSPDocumentSymbols(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name:        "lsp_document_symbols",
		Description: "List all symbols (functions, types, variables) in a file using a language server.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"file": map[string]any{
					"type":        "string",
					"description": "File path relative to workspace root.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional: language server ID to use.",
				},
			},
			"required":             []string{"file"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File     string `json:"file"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}
			syms, err := client.DocumentSymbols(ctx, sc, absPath)
			if err != nil {
				return "", err
			}
			return formatSymbols(root, syms), nil
		},
	})
}

func registerLSPWorkspaceSymbols(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name: "lsp_workspace_symbols",
		Description: "Search for symbols across the workspace by name using a language server. " +
			"Specify a language server ID since workspace symbol search is server-specific.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Symbol name or pattern to search for.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Language server ID to query (e.g. \"go\", \"python\"). Required.",
				},
			},
			"required":             []string{"query", "language"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Query    string `json:"query"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			sc := client.PickServer("", p.Language, lspCfg)
			if sc == nil {
				return "", fmt.Errorf("no LSP server %q configured (configure lsp_servers in ~/.codient/config.json)", p.Language)
			}
			syms, err := client.WorkspaceSymbols(ctx, sc, p.Query)
			if err != nil {
				return "", err
			}
			return formatSymbols(root, syms), nil
		},
	})
}

func registerLSPRename(r *Registry, root string, client LSPClient, lspCfg map[string]config.LSPServerConfig) {
	r.Register(Tool{
		Name: "lsp_rename",
		Description: "Rename a symbol across the workspace using a language server. " +
			"The language server computes all necessary file changes. " +
			"Prefer this over manual str_replace for renaming identifiers.",
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"file": map[string]any{
					"type":        "string",
					"description": "File path relative to workspace root.",
				},
				"line": map[string]any{
					"type":        "integer",
					"description": "1-based line number.",
				},
				"character": map[string]any{
					"type":        "integer",
					"description": "1-based column number.",
				},
				"new_name": map[string]any{
					"type":        "string",
					"description": "New name for the symbol.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional: language server ID to use.",
				},
			},
			"required":             []string{"file", "line", "character", "new_name"},
			"additionalProperties": false,
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				File      string `json:"file"`
				Line      int    `json:"line"`
				Character int    `json:"character"`
				NewName   string `json:"new_name"`
				Language  string `json:"language"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(p.NewName) == "" {
				return "", fmt.Errorf("new_name must not be empty")
			}
			absPath, sc, err := resolveLSP(root, p.File, p.Language, client, lspCfg)
			if err != nil {
				return "", err
			}

			edit, err := func() (*lspclient.WorkspaceEdit, error) {
				unlock := r.LockPath(absPath)
				defer unlock()
				return client.Rename(ctx, sc, absPath, p.Line-1, p.Character-1, p.NewName)
			}()
			if err != nil {
				return "", err
			}
			if edit == nil {
				return "No changes returned by the language server.", nil
			}
			edit.NormalizeChanges()
			if len(edit.Changes) == 0 {
				return "No changes returned by the language server.", nil
			}

			var lockPaths []string
			for uri := range edit.Changes {
				if fsPath, err := lspclient.ParseFileURI(uri); err == nil {
					lockPaths = append(lockPaths, fsPath)
				}
			}
			defer r.LockPaths(lockPaths...)()

			return applyWorkspaceEdit(root, edit)
		},
	})
}

// applyWorkspaceEdit validates all paths and applies TextEdits to files.
func applyWorkspaceEdit(root string, edit *lspclient.WorkspaceEdit) (string, error) {
	var filesChanged int
	var editsApplied int
	var summary []string

	for uri, edits := range edit.Changes {
		fsPath, err := lspclient.ParseFileURI(uri)
		if err != nil {
			return "", fmt.Errorf("invalid URI %s: %w", uri, err)
		}
		relPath, err := filepath.Rel(root, fsPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return "", fmt.Errorf("rename target %s escapes workspace root", fsPath)
		}
		// Validate path stays under root.
		if _, err := absUnderRoot(root, relPath); err != nil {
			return "", fmt.Errorf("rename target path error: %w", err)
		}

		content, err := os.ReadFile(fsPath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", relPath, err)
		}

		lines := strings.Split(string(content), "\n")
		// Apply edits in reverse order to preserve line/character offsets.
		sorted := sortEditsReverse(edits)
		for _, e := range sorted {
			lines = applyTextEdit(lines, e)
			editsApplied++
		}

		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(fsPath, []byte(newContent), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", relPath, err)
		}
		filesChanged++
		summary = append(summary, relPath)
	}

	return fmt.Sprintf("Renamed across %d file(s) (%d edits): %s", filesChanged, editsApplied, strings.Join(summary, ", ")), nil
}

func applyTextEdit(lines []string, e lspclient.TextEdit) []string {
	startLine := e.Range.Start.Line
	startChar := e.Range.Start.Character
	endLine := e.Range.End.Line
	endChar := e.Range.End.Character

	if startLine >= len(lines) {
		return lines
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
		endChar = len(lines[endLine])
	}

	startStr := ""
	if startChar <= len(lines[startLine]) {
		startStr = lines[startLine][:startChar]
	}
	endStr := ""
	if endChar <= len(lines[endLine]) {
		endStr = lines[endLine][endChar:]
	}

	replacement := startStr + e.NewText + endStr
	newLines := strings.Split(replacement, "\n")

	result := make([]string, 0, startLine+len(newLines)+(len(lines)-endLine-1))
	result = append(result, lines[:startLine]...)
	result = append(result, newLines...)
	if endLine+1 < len(lines) {
		result = append(result, lines[endLine+1:]...)
	}
	return result
}

// sortEditsReverse returns edits sorted by position descending so we can apply
// from bottom to top without disturbing earlier offsets.
func sortEditsReverse(edits []lspclient.TextEdit) []lspclient.TextEdit {
	out := make([]lspclient.TextEdit, len(edits))
	copy(out, edits)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j], out[j-1]
			if a.Range.Start.Line > b.Range.Start.Line ||
				(a.Range.Start.Line == b.Range.Start.Line && a.Range.Start.Character > b.Range.Start.Character) {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
	}
	return out
}

// formatLocations renders LSP locations relative to the workspace root.
func formatLocations(root string, locs []lspclient.Location) string {
	if len(locs) == 0 {
		return "No results found."
	}
	var b strings.Builder
	for i, loc := range locs {
		if i > 0 {
			b.WriteByte('\n')
		}
		path := loc.URI
		if fsPath, err := lspclient.ParseFileURI(loc.URI); err == nil {
			if rel, err := filepath.Rel(root, fsPath); err == nil && !strings.HasPrefix(rel, "..") {
				path = rel
			} else {
				path = fsPath
			}
		}
		fmt.Fprintf(&b, "%s:%d:%d", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return b.String()
}

// formatSymbols renders LSP symbols in a compact table.
func formatSymbols(root string, syms []lspclient.SymbolInformation) string {
	if len(syms) == 0 {
		return "No symbols found."
	}
	var b strings.Builder
	for i, s := range syms {
		if i > 0 {
			b.WriteByte('\n')
		}
		path := s.Location.URI
		if fsPath, err := lspclient.ParseFileURI(s.Location.URI); err == nil {
			if rel, err := filepath.Rel(root, fsPath); err == nil && !strings.HasPrefix(rel, "..") {
				path = rel
			} else {
				path = fsPath
			}
		}
		kind := lspclient.SymbolKindName(s.Kind)
		container := ""
		if s.ContainerName != "" {
			container = " in " + s.ContainerName
		}
		fmt.Fprintf(&b, "%-12s %s%s  %s:%d", kind, s.Name, container, path, s.Location.Range.Start.Line+1)
	}
	return b.String()
}
