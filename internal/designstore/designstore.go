// Package designstore writes implementation designs to disk with unique filenames.
package designstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// TaskSlug derives a filesystem-friendly name from -goal, -task-file basename, or the first line of user input.
func TaskSlug(goal, taskFilePath, userLine string) string {
	var raw string
	if g := strings.TrimSpace(goal); g != "" {
		raw = g
	} else if p := strings.TrimSpace(taskFilePath); p != "" {
		base := filepath.Base(p)
		raw = strings.TrimSuffix(base, filepath.Ext(base))
	} else {
		raw = firstLine(userLine)
	}
	s := slugify(raw, 60)
	if s == "" {
		return "plan"
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func slugify(s string, maxBytes int) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reNonAlnum.ReplaceAllString(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if maxBytes > 0 && len(s) > maxBytes {
		s = s[:maxBytes]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// LooksLikeReadyToImplement is true when the assistant ended with a handoff design (saved as an artifact).
func LooksLikeReadyToImplement(markdown string) bool {
	return strings.Contains(strings.ToLower(markdown), "ready to implement")
}

// Dir resolves the directory to store designs.
// When sessionID is non-empty, designs are stored in a per-session subdirectory.
func Dir(workspace, override, sessionID string) (string, error) {
	if o := strings.TrimSpace(override); o != "" {
		return filepath.Abs(o)
	}
	base := strings.TrimSpace(workspace)
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base = wd
	}
	dir := filepath.Join(base, ".codient", "designs")
	if sid := strings.TrimSpace(sessionID); sid != "" {
		dir = filepath.Join(dir, sid)
	}
	return filepath.Abs(dir)
}

// Save writes markdown to a new file named {slug}_{date}_{unixNano}.md.
// sessionID scopes the file into a per-session subdirectory.
func Save(workspace, dirOverride, sessionID, slug, markdown string, t time.Time) (absPath string, err error) {
	dir, err := Dir(workspace, dirOverride, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s := slugify(slug, 60)
	if s == "" {
		s = "plan"
	}
	name := s + "_" + t.UTC().Format("20060102-150405") + "_" + strconv.FormatInt(t.UnixNano(), 10) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
