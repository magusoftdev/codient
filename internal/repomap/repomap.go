package repomap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"codient/internal/tokenest"
)

// Map holds extracted tags for a workspace and supports token-budgeted rendering.
type Map struct {
	mu        sync.RWMutex
	workspace string
	byPath    map[string][]Tag // rel path -> tags sorted by line
	ready     chan struct{}
	buildErr  error
}

// New creates an empty Map. Call Build in a goroutine to populate.
func New(workspace string) *Map {
	return &Map{
		workspace: strings.TrimSpace(workspace),
		byPath:    make(map[string][]Tag),
		ready:     make(chan struct{}),
	}
}

// Ready returns a channel closed when Build completes.
func (m *Map) Ready() <-chan struct{} {
	return m.ready
}

// BuildErr returns the error from the last Build, if any.
func (m *Map) BuildErr() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buildErr
}

// FileCount returns the number of files with at least one tag.
func (m *Map) FileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byPath)
}

// TagCount returns the total number of tags.
func (m *Map) TagCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, t := range m.byPath {
		n += len(t)
	}
	return n
}

// Build walks the workspace, extracts tags (with disk cache), and closes Ready.
func (m *Map) Build(ctx context.Context) {
	defer func() {
		select {
		case <-m.ready:
		default:
			close(m.ready)
		}
	}()

	ws := m.workspace
	if ws == "" {
		m.mu.Lock()
		m.buildErr = fmt.Errorf("empty workspace")
		m.mu.Unlock()
		return
	}

	paths, err := listWorkspaceFiles(ws)
	if err != nil {
		m.mu.Lock()
		m.buildErr = fmt.Errorf("list files: %w", err)
		m.mu.Unlock()
		return
	}

	cached, _ := loadStore(ws)
	if cached == nil {
		cached = make(map[string]fileEntry)
	}

	next := make(map[string]fileEntry, len(paths))
	for _, rel := range paths {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.buildErr = ctx.Err()
			m.mu.Unlock()
			return
		default:
		}

		abs := filepath.Join(ws, rel)
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		mod := info.ModTime().UnixNano()
		lang := LanguageFromPath(rel)

		if fe, ok := cached[rel]; ok && fe.ModUnixNano == mod && len(fe.Tags) >= 0 {
			next[rel] = fe
			continue
		}

		text, err := readSourceFile(abs)
		if err != nil || text == "" {
			next[rel] = fileEntry{ModUnixNano: mod, Tags: nil}
			continue
		}

		tags := ExtractTags(lang, filepath.ToSlash(rel), text)
		for i := range tags {
			tags[i].Path = filepath.ToSlash(rel)
		}
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Line != tags[j].Line {
				return tags[i].Line < tags[j].Line
			}
			return tags[i].Name < tags[j].Name
		})
		next[rel] = fileEntry{ModUnixNano: mod, Tags: tags}
	}

	if err := saveStore(ws, next); err != nil {
		fmt.Fprintf(os.Stderr, "codient: repo map persist: %v\n", err)
	}

	byPath := make(map[string][]Tag, len(next))
	for rel, fe := range next {
		if len(fe.Tags) == 0 {
			continue
		}
		p := filepath.ToSlash(rel)
		byPath[p] = append([]Tag(nil), fe.Tags...)

	}

	m.mu.Lock()
	m.byPath = byPath
	m.buildErr = nil
	m.mu.Unlock()
}

// PromptText returns the trimmed repo map string for injection into a system prompt, or "" when disabled or not ready.
func PromptText(repoMapTokens int, m *Map) string {
	if m == nil || repoMapTokens < 0 {
		return ""
	}
	select {
	case <-m.Ready():
	default:
		return ""
	}
	tok := repoMapTokens
	if tok == 0 {
		tok = AutoTokens(m.FileCount())
	}
	return strings.TrimSpace(m.Render(tok))
}

// AutoTokens picks a token budget from tracked file count (plan: small ~2K, medium ~4K, large ~6K, cap 8K).
func AutoTokens(fileCount int) int {
	switch {
	case fileCount <= 0:
		return 2000
	case fileCount <= 50:
		return 2000
	case fileCount <= 500:
		return 4000
	case fileCount <= 1000:
		return 6000
	default:
		return 8000
	}
}

// Render produces a text repo map within maxTokens (estimated).
func (m *Map) Render(maxTokens int) string {
	return m.RenderPrefix("", maxTokens)
}

// RenderPrefix is like Render but only includes paths starting with pathPrefix (slash-normalized).
func (m *Map) RenderPrefix(pathPrefix string, maxTokens int) string {
	select {
	case <-m.ready:
	default:
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.buildErr != nil || len(m.byPath) == 0 {
		return ""
	}

	prefix := strings.TrimSpace(pathPrefix)
	prefix = filepath.ToSlash(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var paths []string
	for p := range m.byPath {
		if prefix != "" {
			pp := p
			if !strings.HasPrefix(pp, prefix) && pp != strings.TrimSuffix(prefix, "/") {
				continue
			}
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	if maxTokens <= 0 {
		maxTokens = AutoTokens(len(paths))
	}

	// Priority order for inclusion under budget
	sort.Slice(paths, func(i, j int) bool {
		si, sj := filePriority(paths[i], m.byPath[paths[i]]), filePriority(paths[j], m.byPath[paths[j]])
		if si != sj {
			return si > sj
		}
		return paths[i] < paths[j]
	})

	var blocks []string
	for _, p := range paths {
		var b strings.Builder
		b.WriteString(p)
		b.WriteByte('\n')
		for _, t := range m.byPath[p] {
			fmt.Fprintf(&b, "    %s %s\n", t.Kind, t.Name)
		}
		blocks = append(blocks, b.String())
	}

	// Greedy pack from highest priority (paths already sorted by priority)
	var kept []string
	tokens := 0
	for _, blk := range blocks {
		et := tokenest.Estimate(blk)
		if tokens+et > maxTokens && len(kept) > 0 {
			break
		}
		if tokens+et > maxTokens && len(kept) == 0 {
			// First block alone exceeds budget: hard-truncate file listing
			kept = append(kept, truncateBlock(blk, maxTokens))
			break
		}
		kept = append(kept, blk)
		tokens += et
	}

	sort.Strings(kept)
	out := strings.Join(kept, "\n")
	if len(kept) < len(blocks) {
		out += fmt.Sprintf("\n\n[repo map truncated: showing %d of %d files within ~%d tokens]", len(kept), len(blocks), maxTokens)
	}
	return strings.TrimSpace(out)
}

func truncateBlock(s string, maxTokens int) string {
	// ~4 chars per token
	maxChars := maxTokens * 4
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "\n[truncated]"
}

func filePriority(path string, tags []Tag) int {
	p := filepath.ToSlash(path)
	base := filepath.Base(p)
	score := len(tags) * 2

	depth := strings.Count(p, "/")
	bonus := 200 - depth*10
	if bonus < 0 {
		bonus = 0
	}
	score += bonus

	switch base {
	case "main.go", "index.ts", "index.tsx", "index.js", "app.py", "main.py", "lib.rs", "main.rs":
		score += 500
	}

	if strings.Contains(base, "_test.go") || strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") || strings.HasSuffix(base, "_test.py") ||
		strings.Contains(base, "Test.java") {
		score -= 1000
	}

	return score
}
