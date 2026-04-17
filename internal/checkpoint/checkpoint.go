// Package checkpoint persists named conversation/workspace snapshots for rollback and branching.
package checkpoint

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

// Checkpoint is a frozen snapshot of conversation and workspace metadata.
type Checkpoint struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Name      string            `json:"name,omitempty"`
	Turn      int               `json:"turn"`
	CreatedAt time.Time         `json:"created_at"`
	Messages  []json.RawMessage `json:"messages"`
	Mode      string            `json:"mode"`
	Model     string            `json:"model"`
	GitSHA    string            `json:"git_sha,omitempty"`
	GitBranch string            `json:"git_branch,omitempty"`
	PlanPhase string            `json:"plan_phase,omitempty"`
	PlanJSON  json.RawMessage   `json:"plan_json,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
	Branch    string            `json:"branch,omitempty"`
}

// Dir returns <workspace>/.codient/checkpoints/<session_id>.
func Dir(workspace, sessionID string) string {
	base := strings.TrimSpace(workspace)
	if base == "" {
		base = "."
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "default"
	}
	return filepath.Join(base, ".codient", "checkpoints", sid)
}

// NewID returns a unique checkpoint id (time + random suffix).
func NewID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("cp_%s_%s", time.Now().UTC().Format("20060102_150405"), hex.EncodeToString(b[:]))
}

// Save writes the checkpoint JSON and updates the tree index.
func Save(workspace string, cp *Checkpoint) error {
	if cp == nil {
		return fmt.Errorf("checkpoint: nil checkpoint")
	}
	if strings.TrimSpace(cp.ID) == "" {
		cp.ID = NewID()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	dir := Dir(workspace, cp.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("checkpoint: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal: %w", err)
	}
	path := filepath.Join(dir, cp.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("checkpoint: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("checkpoint: rename: %w", err)
	}
	return upsertTreeNode(workspace, cp)
}

// Load reads a checkpoint by id.
func Load(workspace, sessionID, checkpointID string) (*Checkpoint, error) {
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("checkpoint: empty id")
	}
	path := filepath.Join(Dir(workspace, sessionID), checkpointID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("checkpoint: unmarshal: %w", err)
	}
	return &cp, nil
}

// Match describes a resolved checkpoint lookup.
type Match struct {
	ID   string
	Name string
	Turn int
}

// FindByName returns checkpoints whose name equals the query (case-insensitive).
func FindByName(workspace, sessionID, name string) ([]Match, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return nil, nil
	}
	idx, err := LoadTreeIndex(workspace, sessionID)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, n := range idx.Nodes {
		if strings.EqualFold(strings.TrimSpace(n.Name), name) {
			out = append(out, Match{ID: n.ID, Name: n.Name, Turn: n.Turn})
		}
	}
	return out, nil
}

// FindByTurn returns checkpoints with the given turn number.
func FindByTurn(workspace, sessionID string, turn int) ([]Match, error) {
	idx, err := LoadTreeIndex(workspace, sessionID)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, n := range idx.Nodes {
		if n.Turn == turn {
			out = append(out, Match{ID: n.ID, Name: n.Name, Turn: n.Turn})
		}
	}
	return out, nil
}

// FindByIDPrefix returns checkpoints whose id starts with prefix (case-insensitive).
func FindByIDPrefix(workspace, sessionID, prefix string) ([]Match, error) {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return nil, nil
	}
	idx, err := LoadTreeIndex(workspace, sessionID)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, n := range idx.Nodes {
		if strings.HasPrefix(strings.ToLower(n.ID), prefix) {
			out = append(out, Match{ID: n.ID, Name: n.Name, Turn: n.Turn})
		}
	}
	return out, nil
}

// ResolveQuery parses query as turn number, id prefix, or name.
func ResolveQuery(workspace, sessionID, query string) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}
	// Plain integer = turn number
	if t, err := strconv.Atoi(query); err == nil && t >= 0 {
		return FindByTurn(workspace, sessionID, t)
	}
	if t, err := parseTurnQuery(query); err == nil {
		return FindByTurn(workspace, sessionID, t)
	}
	// Prefer id prefix if it looks like cp_
	if strings.HasPrefix(strings.ToLower(query), "cp_") {
		if m, err := FindByIDPrefix(workspace, sessionID, query); err == nil && len(m) > 0 {
			return m, nil
		}
	}
	if m, err := FindByName(workspace, sessionID, query); err == nil && len(m) > 0 {
		return m, nil
	}
	return FindByIDPrefix(workspace, sessionID, query)
}

func parseTurnQuery(s string) (int, error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "turn") {
		return 0, fmt.Errorf("not turn")
	}
	s = strings.TrimSpace(s[len("turn"):])
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "_")
	return strconv.Atoi(strings.TrimSpace(s))
}

// MessagesToOpenAI deserializes stored JSON messages.
func MessagesToOpenAI(msgs []json.RawMessage) ([]openai.ChatCompletionMessageParamUnion, error) {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, raw := range msgs {
		var m openai.ChatCompletionMessageParamUnion
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("checkpoint: unmarshal message: %w", err)
		}
		out = append(out, m)
	}
	return out, nil
}

// ShortSHA returns a short git hash for display.
func ShortSHA(full string) string {
	full = strings.TrimSpace(full)
	if len(full) <= 12 {
		return full
	}
	return full[:12]
}
