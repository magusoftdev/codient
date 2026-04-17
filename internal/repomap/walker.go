package repomap

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxFilesTotal = 10_000

var skipDirs = map[string]struct{}{
	".git": {}, "node_modules": {}, "__pycache__": {},
	".venv": {}, "venv": {}, ".tox": {},
	".next": {}, "dist": {}, ".cache": {},
	".idea": {}, ".vscode": {}, ".codient": {},
	"vendor": {}, ".bundle": {},
}

var skipExtensions = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {},
	".ico": {}, ".svg": {}, ".bmp": {}, ".tiff": {},
	".mp3": {}, ".mp4": {}, ".wav": {}, ".avi": {}, ".mov": {},
	".zip": {}, ".tar": {}, ".gz": {}, ".bz2": {}, ".xz": {}, ".7z": {}, ".rar": {},
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".o": {}, ".a": {},
	".wasm": {}, ".class": {}, ".pyc": {}, ".pyo": {},
	".pdf": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {},
	".db": {}, ".sqlite": {}, ".sqlite3": {},
	".jar": {}, ".war": {},
	".min.js": {}, ".min.css": {},
}

// codeLikeExtensions limits tag extraction to plausible source files.
var codeLikeExtensions = map[string]struct{}{
	".go": {}, ".py": {}, ".rs": {}, ".java": {},
	".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {},
	".c": {}, ".h": {}, ".cc": {}, ".cpp": {}, ".cxx": {}, ".hpp": {}, ".hh": {}, ".hxx": {},
}

func skipByExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, skip := skipExtensions[ext]; skip {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
		return true
	}
	return false
}

func isCodeFile(rel string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	_, ok := codeLikeExtensions[ext]
	return ok
}

func listWorkspaceFiles(root string) ([]string, error) {
	paths, err := gitListFiles(root)
	if err != nil {
		paths, err = walkDir(root)
		if err != nil {
			return nil, err
		}
	}
	if len(paths) > maxFilesTotal {
		paths = paths[:maxFilesTotal]
	}
	var out []string
	for _, rel := range paths {
		if skipByExtension(rel) {
			continue
		}
		if !isCodeFile(rel) {
			continue
		}
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

func gitListFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var paths []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			paths = append(paths, filepath.FromSlash(l))
		}
	}
	return paths, nil
}

func walkDir(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= maxFilesTotal {
			return filepath.SkipAll
		}
		return nil
	})
	return paths, err
}

const maxReadBytes = 512 * 1024

func readSourceFile(abs string) (string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.ContainsRune(sample, 0) {
		return "", nil
	}
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
	}
	return string(data), nil
}
