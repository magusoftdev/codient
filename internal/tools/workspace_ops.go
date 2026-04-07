package tools

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultGlobMaxResults = 200
)

func removePathWorkspace(root, rel string) error {
	abs, err := absUnderRoot(root, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// movePathWorkspace moves or renames a file or directory within the workspace (from -> to).
func movePathWorkspace(root, fromRel, toRel string) error {
	src, err := absUnderRoot(root, fromRel)
	if err != nil {
		return err
	}
	dst, err := absUnderRoot(root, toRel)
	if err != nil {
		return err
	}
	if err := mustNotMoveOntoDescendant(src, dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if err2 := copyPathAbs(src, dst); err2 != nil {
			return fmt.Errorf("move: rename failed (%v): %w", err, err2)
		}
		return os.RemoveAll(src)
	}
	return nil
}

func mustNotMoveOntoDescendant(srcAbs, dstAbs string) error {
	srcAbs = filepath.Clean(srcAbs)
	dstAbs = filepath.Clean(dstAbs)
	if srcAbs == dstAbs {
		return fmt.Errorf("source and destination are the same path")
	}
	sep := string(os.PathSeparator)
	if strings.HasPrefix(dstAbs+sep, srcAbs+sep) {
		return fmt.Errorf("cannot move a path into its own subdirectory")
	}
	return nil
}

// copyPathWorkspace copies a file or directory tree within the workspace.
func copyPathWorkspace(root, fromRel, toRel string) error {
	src, err := absUnderRoot(root, fromRel)
	if err != nil {
		return err
	}
	dst, err := absUnderRoot(root, toRel)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copy_path: symlinks are not supported")
	}
	if strings.HasPrefix(dst+string(os.PathSeparator), src+string(os.PathSeparator)) && src != dst {
		return fmt.Errorf("cannot copy a path into its own subdirectory")
	}
	return copyPathAbs(src, dst)
}

func copyPathAbs(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copy_path: symlinks are not supported")
	}
	if !fi.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return copyFileAbs(src, dst, fi.Mode())
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(out, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy_path: symlinks are not supported: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return copyFileAbs(path, out, info.Mode())
	})
}

func copyFileAbs(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// pathStatWorkspace returns a short text description of path metadata (no file contents).
func pathStatWorkspace(root, rel string) (string, error) {
	abs, err := absUnderRoot(root, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("path: %s\nexists: false", filepath.ToSlash(rel)), nil
		}
		return "", err
	}
	kind := "file"
	switch {
	case fi.IsDir():
		kind = "directory"
	case fi.Mode()&os.ModeSymlink != 0:
		kind = "symlink"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "path: %s\nexists: true\nkind: %s\nmode: %s\n", filepath.ToSlash(rel), kind, fi.Mode().String())
	fmt.Fprintf(&b, "size: %d\nmod_time: %s\n", fi.Size(), fi.ModTime().UTC().Format(time.RFC3339Nano))
	if fi.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(abs); err == nil {
			fmt.Fprintf(&b, "link_target: %s\n", target)
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// globFilesWorkspace lists file paths under `under` matching `pattern`.
// If pattern contains '/', it is matched against the path relative to under (slashes forward).
// Otherwise pattern is matched against each file's basename only (recursive).
func globFilesWorkspace(root, under, pattern string, maxResults int) (string, error) {
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	base, err := absUnderRoot(root, under)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(base)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("under is not a directory: %s", under)
	}
	matchFullPath := strings.Contains(pattern, "/")
	var paths []string
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(paths) >= maxResults {
			return errHitLimit
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		var ok bool
		if matchFullPath {
			ok, err = filepath.Match(pattern, relSlash)
		} else {
			ok, err = filepath.Match(pattern, filepath.Base(relSlash))
		}
		if err != nil {
			return err
		}
		if ok {
			paths = append(paths, relSlash)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errHitLimit) {
		return "", err
	}
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	out := strings.TrimSuffix(b.String(), "\n")
	if len(paths) >= maxResults {
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("[truncated: max_results=%d]", maxResults)
	}
	return out, nil
}
