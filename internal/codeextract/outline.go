// Package codeextract produces dense structural views of source files for tools.
package codeextract

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"codient/internal/repomap"
)

// Outline returns a structural summary of src for the given workspace-relative path.
// lang should be a repomap language id (e.g. "go", "python"); if empty, it is inferred from relPath.
// If truncated is true, the caller read fewer bytes than the file size (max_bytes cap); Outline returns an error.
func Outline(relPath string, lang string, src []byte, truncated bool) (string, error) {
	if truncated {
		return "", fmt.Errorf("read_file outline: source truncated at max_bytes; increase max_bytes or use view=full with start_line/end_line")
	}
	if len(src) == 0 {
		return "(empty file)", nil
	}
	if lang == "" {
		lang = repomap.LanguageFromPath(relPath)
	}
	switch lang {
	case "go":
		return outlineGo(relPath, src)
	case "python", "typescript", "javascript", "rust", "java", "c", "cpp":
		out, err := outlineHeuristic(lang, relPath, string(src))
		if err != nil {
			return "", err
		}
		return out, nil
	default:
		ext := filepath.Ext(relPath)
		if lang == "" {
			return "", fmt.Errorf("read_file outline: unknown or non-source file type %q for path %s; use view=full", ext, relPath)
		}
		return "", fmt.Errorf("read_file outline is not supported for %s files (language %q); use view=full", ext, lang)
	}
}

func outlineGo(filename string, src []byte) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Base(filename), src, 0)
	if err != nil {
		return "", fmt.Errorf("read_file outline: Go parse error: %w (try view=full)", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", f.Name.Name)

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			fd := *d
			fd.Body = &ast.BlockStmt{}
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, &fd); err != nil {
				return "", fmt.Errorf("read_file outline: format func: %w", err)
			}
			b.WriteString(strings.TrimSpace(buf.String()))
			b.WriteString("\n\n")
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				gd := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{ts}}
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, gd); err != nil {
					return "", fmt.Errorf("read_file outline: format type %s: %w", ts.Name.Name, err)
				}
				b.WriteString(strings.TrimSpace(buf.String()))
				b.WriteString("\n\n")
			}
		}
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "(no func or type declarations in file)", nil
	}
	return out, nil
}
