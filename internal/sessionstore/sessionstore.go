// Package sessionstore persists REPL session state to disk so codient can resume
// where it left off across process restarts.
package sessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

// SessionState is the top-level structure persisted to a JSON file.
type SessionState struct {
	ID        string            `json:"id"`
	Workspace string            `json:"workspace"`
	Mode      string            `json:"mode"`
	Model     string            `json:"model"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Messages  []json.RawMessage `json:"messages"`

	// Plan lifecycle fields (populated when a structured plan exists).
	PlanPhase string `json:"plan_phase,omitempty"`
	PlanPath  string `json:"plan_path,omitempty"`
}

// SessionSummary is returned by List for UI purposes.
type SessionSummary struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	UpdatedAt time.Time `json:"updated_at"`
	Turns     int       `json:"turns"`
}

// Dir returns the session storage directory for a workspace.
func Dir(workspace string) string {
	base := strings.TrimSpace(workspace)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, ".codient", "sessions")
}

// NewID generates a session ID from the workspace name and current time.
func NewID(workspace string) string {
	slug := slugify(filepath.Base(workspace))
	if slug == "" {
		slug = "session"
	}
	return fmt.Sprintf("%s_%s", slug, time.Now().Format("20060102_150405"))
}

// Save writes the session state atomically (write tmp + rename).
func Save(state *SessionState) error {
	dir := Dir(state.Workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sessionstore: mkdir: %w", err)
	}
	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("sessionstore: marshal: %w", err)
	}
	path := filepath.Join(dir, state.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("sessionstore: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("sessionstore: rename: %w", err)
	}
	return nil
}

// LoadLatest finds and loads the most recently updated session for a workspace.
// Returns nil, nil when no session exists.
func LoadLatest(workspace string) (*SessionState, error) {
	dir := Dir(workspace)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessionstore: readdir: %w", err)
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			best = filepath.Join(dir, e.Name())
		}
	}
	if best == "" {
		return nil, nil
	}
	return Load(best)
}

// Load reads a session from a specific file path.
func Load(path string) (*SessionState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: read: %w", err)
	}
	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("sessionstore: unmarshal: %w", err)
	}
	return &s, nil
}

// List returns summaries of all sessions for a workspace, newest first.
func List(workspace string) ([]SessionSummary, error) {
	dir := Dir(workspace)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := Load(path)
		if err != nil {
			continue
		}
		turns := countUserMessages(s.Messages)
		out = append(out, SessionSummary{
			ID:        s.ID,
			Mode:      s.Mode,
			UpdatedAt: s.UpdatedAt,
			Turns:     turns,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// FromOpenAI serializes API message types to JSON for on-disk storage.
func FromOpenAI(msgs []openai.ChatCompletionMessageParamUnion) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

// ToOpenAI deserializes stored JSON messages back to API types.
func ToOpenAI(msgs []json.RawMessage) ([]openai.ChatCompletionMessageParamUnion, error) {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, raw := range msgs {
		var m openai.ChatCompletionMessageParamUnion
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("sessionstore: unmarshal message: %w", err)
		}
		out = append(out, m)
	}
	return out, nil
}

// MessageRole extracts the role from a raw JSON message for filtering.
func MessageRole(raw json.RawMessage) string {
	var probe struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &probe) == nil {
		return probe.Role
	}
	return ""
}

// MessageContent extracts a text preview from a raw JSON message. For multipart
// user content, text parts are joined and image parts appear as "[image]".
func MessageContent(raw json.RawMessage) string {
	var probe struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ""
	}
	if len(probe.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(probe.Content, &s) == nil {
		return s
	}
	var parts []json.RawMessage
	if json.Unmarshal(probe.Content, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for i, pr := range parts {
		if i > 0 {
			b.WriteString(" ")
		}
		var part struct {
			Type string `json:"type"`
			Text string `json:"text"`
			// image_url block (OpenAI chat format)
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if json.Unmarshal(pr, &part) != nil {
			continue
		}
		if part.ImageURL.URL != "" || part.Type == "image_url" {
			b.WriteString("[image]")
			continue
		}
		if part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func countUserMessages(msgs []json.RawMessage) int {
	n := 0
	for _, raw := range msgs {
		if MessageRole(raw) == "user" {
			n++
		}
	}
	return n
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 30 {
		result = result[:30]
	}
	return result
}
