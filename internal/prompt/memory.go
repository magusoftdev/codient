package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxMemoryBytesPerFile caps each individual memory file (global and workspace).
const MaxMemoryBytesPerFile = 16 * 1024

// MemoryFileName is the filename used for both global and workspace memory.
const MemoryFileName = "memory.md"

// LoadMemory reads optional memory files from the global state directory and the
// workspace .codient directory. Returns formatted content ready for system prompt
// injection, or empty string when no memory files exist.
func LoadMemory(stateDir, workspaceRoot string) (string, error) {
	var b strings.Builder

	globalPath := ""
	if dir := strings.TrimSpace(stateDir); dir != "" {
		globalPath = filepath.Join(dir, MemoryFileName)
	}
	wsPath := ""
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		wsPath = filepath.Join(root, ".codient", MemoryFileName)
	}

	if globalPath != "" {
		text, err := readMemoryFile(globalPath)
		if err != nil {
			return "", fmt.Errorf("read global memory: %w", err)
		}
		if text != "" {
			b.WriteString("### Global memory (~/.codient/memory.md)\n\n")
			b.WriteString(text)
		}
	}

	if wsPath != "" {
		text, err := readMemoryFile(wsPath)
		if err != nil {
			return "", fmt.Errorf("read workspace memory: %w", err)
		}
		if text != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("### Workspace memory (<workspace>/.codient/memory.md)\n\n")
			b.WriteString(text)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func readMemoryFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(data) > MaxMemoryBytesPerFile {
		data = append(data[:MaxMemoryBytesPerFile], []byte("\n\n[truncated]\n")...)
	}
	return strings.TrimSpace(string(data)), nil
}

// GlobalMemoryPath returns the path to the global memory file.
func GlobalMemoryPath(stateDir string) string {
	return filepath.Join(stateDir, MemoryFileName)
}

// WorkspaceMemoryPath returns the path to the workspace memory file.
func WorkspaceMemoryPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".codient", MemoryFileName)
}
