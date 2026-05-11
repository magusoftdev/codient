// Package fileref parses @path file references in user input, reads their
// contents, and formats them for injection into user messages.
package fileref

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// DefaultMaxFileBytes is the per-file read limit (256 KiB, matching read_file).
const DefaultMaxFileBytes = 256 * 1024

// DefaultMaxAggregateBytes caps total referenced content per message (1 MiB).
const DefaultMaxAggregateBytes = 1 << 20

// FileReference holds a loaded file's path and content.
type FileReference struct {
	Path           string
	Content        string
	OrigBytes      int
	TruncatedBytes int // non-zero if content was truncated
}

// refPattern matches @path, @"path with spaces", @'path with spaces'.
// Excludes @image: (handled by imageutil) and escaped \@.
var refPattern = regexp.MustCompile(`(?:^|[^\\])@(?:"([^"]+)"|'([^']+)'|(\S+))`)

// ParseAndLoad finds @path references in text, reads each file from the workspace,
// and returns the text with references removed plus the loaded file contents.
// Skips @image: tokens (those are handled by imageutil.ParseInlineImages).
func ParseAndLoad(text, workspace string, maxFileBytes, maxAggregateBytes int) (clean string, refs []FileReference, warnings []string, err error) {
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultMaxFileBytes
	}
	if maxAggregateBytes <= 0 {
		maxAggregateBytes = DefaultMaxAggregateBytes
	}
	workspace = strings.TrimSpace(workspace)

	var out strings.Builder
	last := 0
	totalBytes := 0

	for _, sm := range refPattern.FindAllStringSubmatchIndex(text, -1) {
		if len(sm) < 2 {
			continue
		}

		// The match may include the preceding non-backslash character due to
		// the (?:^|[^\\]) prefix. Find where @ actually is.
		fullStart := sm[0]
		atPos := strings.Index(text[fullStart:sm[1]], "@")
		if atPos < 0 {
			continue
		}
		atPos += fullStart

		// Extract the path from whichever capture group matched.
		var rawPath string
		if len(sm) >= 4 && sm[2] >= 0 && sm[3] > sm[2] {
			rawPath = text[sm[2]:sm[3]] // double-quoted
		} else if len(sm) >= 6 && sm[4] >= 0 && sm[5] > sm[4] {
			rawPath = text[sm[4]:sm[5]] // single-quoted
		} else if len(sm) >= 8 && sm[6] >= 0 && sm[7] > sm[6] {
			rawPath = text[sm[6]:sm[7]] // unquoted
		}
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" {
			continue
		}

		// Skip @image: tokens — they're handled by the image pipeline.
		if strings.HasPrefix(rawPath, "image:") {
			continue
		}

		// Write text before the @ (including any non-backslash prefix char).
		out.WriteString(text[last:atPos])
		last = sm[1]

		// Resolve the path relative to workspace.
		p := rawPath
		if !filepath.IsAbs(p) && workspace != "" {
			p = filepath.Join(workspace, p)
		}
		p = filepath.Clean(p)

		// Workspace confinement check.
		if workspace != "" {
			absWs, _ := filepath.Abs(workspace)
			absP, _ := filepath.Abs(p)
			rel, relErr := filepath.Rel(absWs, absP)
			if relErr != nil || strings.HasPrefix(rel, "..") {
				warnings = append(warnings, fmt.Sprintf("@%s: path escapes workspace", rawPath))
				continue
			}
		}

		info, statErr := os.Stat(p)
		if statErr != nil {
			warnings = append(warnings, fmt.Sprintf("@%s: file not found", rawPath))
			continue
		}
		if info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("@%s: directories not supported as @references (use a file path)", rawPath))
			continue
		}

		b, readErr := os.ReadFile(p)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("@%s: %v", rawPath, readErr))
			continue
		}

		origBytes := len(b)
		truncated := 0
		if len(b) > maxFileBytes {
			truncated = len(b) - maxFileBytes
			b = b[:maxFileBytes]
		}
		if !utf8.Valid(b) {
			warnings = append(warnings, fmt.Sprintf("@%s: not a text file", rawPath))
			continue
		}

		if totalBytes+len(b) > maxAggregateBytes {
			warnings = append(warnings, fmt.Sprintf("@%s: skipped — exceeded %d byte reference limit", rawPath, maxAggregateBytes))
			continue
		}
		totalBytes += len(b)
		// Display path: relative to workspace if possible.
		displayPath := rawPath
		if workspace != "" && filepath.IsAbs(rawPath) {
			if rel, err := filepath.Rel(workspace, p); err == nil && !strings.HasPrefix(rel, "..") {
				displayPath = rel
			}
		}

		refs = append(refs, FileReference{
			Path:           displayPath,
			Content:        string(b),
			OrigBytes:      origBytes,
			TruncatedBytes: truncated,
		})
	}

	out.WriteString(text[last:])
	clean = strings.TrimSpace(out.String())
	return clean, refs, warnings, nil
}

// FormatReferences formats loaded file references as a context block to append
// to the user message.
func FormatReferences(refs []FileReference) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<referenced_files>\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "--- %s ---\n", r.Path)
		b.WriteString(r.Content)
		if r.TruncatedBytes > 0 {
			fmt.Fprintf(&b, "\n[truncated: %d bytes omitted]\n", r.TruncatedBytes)
		}
		b.WriteString("\n\n")
	}
	b.WriteString("</referenced_files>")
	return b.String()
}
