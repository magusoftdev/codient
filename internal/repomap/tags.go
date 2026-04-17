// Package repomap builds a structural overview of a workspace from extracted symbols.
package repomap

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Tag is a single extracted symbol from a source file.
type Tag struct {
	Path string // workspace-relative path (forward slashes)
	Name string
	Kind string // func, type, class, interface, struct, enum, trait, mod, var, const, ...
	Line int    // 1-based line number
}

// LanguageFromPath returns a language id from file extension, or "" if unknown.
func LanguageFromPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx":
		return "cpp"
	default:
		return ""
	}
}

// ExtractTags parses source text and returns tags for the given language.
func ExtractTags(lang, relPath, content string) []Tag {
	if lang == "" {
		lang = LanguageFromPath(relPath)
	}
	switch lang {
	case "go":
		return extractGo(relPath, content)
	case "python":
		return extractPython(relPath, content)
	case "typescript", "javascript":
		return extractTSJS(relPath, content)
	case "rust":
		return extractRust(relPath, content)
	case "java":
		return extractJava(relPath, content)
	case "c", "cpp":
		return extractCPP(relPath, content)
	default:
		return nil
	}
}

// --- Go ---

var (
	reGoFunc  = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(`)
	reGoType  = regexp.MustCompile(`(?m)^type\s+(\w+)\s+`)
	reGoConst = regexp.MustCompile(`(?m)^const\s+(?:\([^)]*\)\s*)?(\w+)\s*=`)
	reGoVar   = regexp.MustCompile(`(?m)^var\s+(?:\([^)]*\)\s*)?(\w+)\s*(?:=|\()`)
)

func extractGo(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "//") || s == "" {
			continue
		}
		if m := reGoFunc.FindStringSubmatch(line); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "func", Line: lineNo})
			continue
		}
		if m := reGoType.FindStringSubmatch(line); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "type", Line: lineNo})
			continue
		}
		if m := reGoConst.FindStringSubmatch(line); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "const", Line: lineNo})
			continue
		}
		if m := reGoVar.FindStringSubmatch(line); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "var", Line: lineNo})
		}
	}
	return out
}

// --- Python ---

var (
	rePyClass = regexp.MustCompile(`^class\s+(\w+)\s*(?:\(|:)`)
	rePyDef   = regexp.MustCompile(`^def\s+(\w+)\s*\(`)
)

func extractPython(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "#") || trim == "" {
			continue
		}
		// Top-level only: no leading indent (or only at column 0 after strip of spaces for class/def)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		if m := rePyClass.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "class", Line: lineNo})
			continue
		}
		if m := rePyDef.FindStringSubmatch(trim); len(m) > 1 {
			if m[1] == "__init__" {
				continue
			}
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "def", Line: lineNo})
		}
	}
	return out
}

// --- TypeScript / JavaScript ---

var (
	reTSFunc    = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(`)
	reTSClass   = regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`)
	reTSIface   = regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`)
	reTSType    = regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)\s*=`)
	reTSConstFn = regexp.MustCompile(`^(?:export\s+)?const\s+(\w+)\s*=\s*(?:async\s*)?\(`)
)

func extractTSJS(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") {
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		if m := reTSFunc.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "function", Line: lineNo})
			continue
		}
		if m := reTSClass.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "class", Line: lineNo})
			continue
		}
		if m := reTSIface.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "interface", Line: lineNo})
			continue
		}
		if m := reTSType.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "type", Line: lineNo})
			continue
		}
		if m := reTSConstFn.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "const", Line: lineNo})
		}
	}
	return out
}

// --- Rust ---

var (
	reRustFn     = regexp.MustCompile(`^(?:pub\s+)?(?:async\s+)?fn\s+(\w+)\s*\(`)
	reRustStruct = regexp.MustCompile(`^(?:pub\s+)?struct\s+(\w+)`)
	reRustEnum   = regexp.MustCompile(`^(?:pub\s+)?enum\s+(\w+)`)
	reRustTrait  = regexp.MustCompile(`^(?:pub\s+)?trait\s+(\w+)`)
	reRustImpl   = regexp.MustCompile(`^impl(?:<[^>]+>)?\s+(?:\w+\s+for\s+)?(\w+)`)
	reRustMod    = regexp.MustCompile(`^(?:pub\s+)?mod\s+(\w+)\s*\{`)
)

func extractRust(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "//") || trim == "" {
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		if m := reRustFn.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "fn", Line: lineNo})
			continue
		}
		if m := reRustStruct.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "struct", Line: lineNo})
			continue
		}
		if m := reRustEnum.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "enum", Line: lineNo})
			continue
		}
		if m := reRustTrait.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "trait", Line: lineNo})
			continue
		}
		if m := reRustImpl.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "impl", Line: lineNo})
			continue
		}
		if m := reRustMod.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "mod", Line: lineNo})
		}
	}
	return out
}

// --- Java ---

var (
	reJavaClass = regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?(?:static\s+)?(?:abstract\s+)?(?:final\s+)?class\s+(\w+)`)
	reJavaIface = regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?interface\s+(\w+)`)
	reJavaEnum  = regexp.MustCompile(`^(?:public\s+)?enum\s+(\w+)`)
)

func extractJava(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") {
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		if m := reJavaClass.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "class", Line: lineNo})
			continue
		}
		if m := reJavaIface.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "interface", Line: lineNo})
			continue
		}
		if m := reJavaEnum.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "enum", Line: lineNo})
		}
	}
	return out
}

// --- C / C++ ---

var reCppClass = regexp.MustCompile(`^(?:class|struct)\s+(\w+)`)

func extractCPP(relPath, content string) []Tag {
	lines := strings.Split(content, "\n")
	var out []Tag
	for i, line := range lines {
		lineNo := i + 1
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "#") {
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		if m := reCppClass.FindStringSubmatch(trim); len(m) > 1 {
			out = append(out, Tag{Path: relPath, Name: m[1], Kind: "class", Line: lineNo})
		}
	}
	return out
}
