package fileref

import (
	"os"
	"runtime"
	"strings"
)

// SplitPastedPaths splits text into candidate file paths, handling backslash-
// escaped spaces (POSIX), double-quoted paths, and single-quoted paths.
func SplitPastedPaths(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	isWindows := runtime.GOOS == "windows"
	var paths []string
	var current strings.Builder
	mode := modeNormal

	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch mode {
		case modeNormal:
			switch {
			case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
				if current.Len() > 0 {
					paths = append(paths, current.String())
					current.Reset()
				}
			case ch == '"':
				mode = modeDouble
			case ch == '\'':
				mode = modeSingle
			case ch == '\\' && !isWindows && i+1 < len(text):
				current.WriteByte(text[i+1])
				i++
			default:
				current.WriteByte(ch)
			}
		case modeDouble:
			if ch == '"' {
				mode = modeNormal
			} else {
				current.WriteByte(ch)
			}
		case modeSingle:
			if ch == '\'' {
				mode = modeNormal
			} else {
				current.WriteByte(ch)
			}
		}
	}
	if current.Len() > 0 {
		paths = append(paths, current.String())
	}
	return paths
}

type parseMode int

const (
	modeNormal parseMode = iota
	modeDouble
	modeSingle
)

// pathPrefixRe checks if a string starts with a typical filesystem path prefix.
func looksLikePath(s string) bool {
	if s == "" {
		return false
	}
	// POSIX: /, ~, .
	if s[0] == '/' || s[0] == '~' || s[0] == '.' {
		return true
	}
	// Windows: C:\ or \\
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') &&
		((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) {
		return true
	}
	if len(s) >= 2 && s[0] == '\\' && s[1] == '\\' {
		return true
	}
	return false
}

// DetectPastedPaths checks whether text consists entirely of valid file paths.
// If so, returns the text rewritten with @ prefixes and ok=true.
// If any token is not a valid file path, returns ("", false).
func DetectPastedPaths(text string) (string, bool) {
	candidates := SplitPastedPaths(text)
	if len(candidates) == 0 {
		return "", false
	}

	for _, c := range candidates {
		if !looksLikePath(c) {
			return "", false
		}
		info, err := os.Stat(c)
		if err != nil {
			return "", false
		}
		if info.IsDir() {
			return "", false
		}
	}

	var b strings.Builder
	for i, c := range candidates {
		if i > 0 {
			b.WriteByte(' ')
		}
		if strings.ContainsAny(c, " \t") {
			b.WriteString(`@"` + c + `"`)
		} else {
			b.WriteByte('@')
			b.WriteString(c)
		}
	}
	return b.String(), true
}
