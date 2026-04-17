package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TreeIndex is the lightweight index for listing checkpoints without loading full messages.
type TreeIndex struct {
	SessionID string     `json:"session_id"`
	Nodes     []TreeNode `json:"nodes"`
}

// TreeNode is metadata for one checkpoint in the index.
type TreeNode struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Turn      int    `json:"turn"`
	GitSHA    string `json:"git_sha,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

func treePath(workspace, sessionID string) string {
	return filepath.Join(Dir(workspace, sessionID), "tree.json")
}

// LoadTreeIndex reads tree.json or returns an empty index.
func LoadTreeIndex(workspace, sessionID string) (*TreeIndex, error) {
	path := treePath(workspace, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TreeIndex{SessionID: sessionID, Nodes: nil}, nil
		}
		return nil, fmt.Errorf("checkpoint: read tree: %w", err)
	}
	var idx TreeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("checkpoint: unmarshal tree: %w", err)
	}
	return &idx, nil
}

func saveTreeIndex(workspace string, idx *TreeIndex) error {
	if idx == nil {
		return fmt.Errorf("checkpoint: nil tree index")
	}
	dir := Dir(workspace, idx.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("checkpoint: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal tree: %w", err)
	}
	path := treePath(workspace, idx.SessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("checkpoint: write tree tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("checkpoint: rename tree: %w", err)
	}
	return nil
}

func upsertTreeNode(workspace string, cp *Checkpoint) error {
	idx, err := LoadTreeIndex(workspace, cp.SessionID)
	if err != nil {
		return err
	}
	idx.SessionID = cp.SessionID
	created := ""
	if !cp.CreatedAt.IsZero() {
		created = cp.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	node := TreeNode{
		ID:        cp.ID,
		ParentID:  cp.ParentID,
		Name:      cp.Name,
		Branch:    cp.Branch,
		Turn:      cp.Turn,
		GitSHA:    cp.GitSHA,
		CreatedAt: created,
	}
	replaced := false
	for i := range idx.Nodes {
		if idx.Nodes[i].ID == cp.ID {
			idx.Nodes[i] = node
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Nodes = append(idx.Nodes, node)
	}
	return saveTreeIndex(workspace, idx)
}

// BranchSummary is one logical conversation branch with its tip checkpoint.
type BranchSummary struct {
	Label     string
	TipID     string
	TipName   string
	TipTurn   int
	IsCurrent bool
}

// SummarizeBranches groups nodes by Branch label and picks the latest tip per branch by CreatedAt.
func SummarizeBranches(idx *TreeIndex, currentBranch string) []BranchSummary {
	if idx == nil {
		return nil
	}
	type tip struct {
		node TreeNode
		t    string
	}
	byLabel := map[string]tip{}
	for _, n := range idx.Nodes {
		lbl := strings.TrimSpace(n.Branch)
		if lbl == "" {
			lbl = "main"
		}
		cur, ok := byLabel[lbl]
		if !ok || n.CreatedAt > cur.t {
			byLabel[lbl] = tip{node: n, t: n.CreatedAt}
		}
	}
	var labels []string
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	cur := strings.TrimSpace(currentBranch)
	if cur == "" {
		cur = "main"
	}
	var out []BranchSummary
	for _, l := range labels {
		t := byLabel[l]
		out = append(out, BranchSummary{
			Label:     l,
			TipID:     t.node.ID,
			TipName:   t.node.Name,
			TipTurn:   t.node.Turn,
			IsCurrent: strings.EqualFold(l, cur),
		})
	}
	return out
}

// RenderTree builds a text tree of checkpoints for stderr display.
func RenderTree(workspace, sessionID, currentCheckpointID string) (string, error) {
	idx, err := LoadTreeIndex(workspace, sessionID)
	if err != nil {
		return "", err
	}
	if len(idx.Nodes) == 0 {
		return fmt.Sprintf("codient: no checkpoints for session %s\n", sessionID), nil
	}
	idSet := map[string]struct{}{}
	for _, n := range idx.Nodes {
		idSet[n.ID] = struct{}{}
	}
	children := map[string][]TreeNode{}
	var roots []TreeNode
	for _, n := range idx.Nodes {
		pid := strings.TrimSpace(n.ParentID)
		if pid == "" {
			roots = append(roots, n)
			continue
		}
		if _, ok := idSet[pid]; !ok {
			roots = append(roots, n)
			continue
		}
		children[pid] = append(children[pid], n)
	}
	for k := range children {
		sort.Slice(children[k], func(i, j int) bool {
			if children[k][i].CreatedAt != children[k][j].CreatedAt {
				return children[k][i].CreatedAt < children[k][j].CreatedAt
			}
			return children[k][i].ID < children[k][j].ID
		})
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].CreatedAt != roots[j].CreatedAt {
			return roots[i].CreatedAt < roots[j].CreatedAt
		}
		return roots[i].ID < roots[j].ID
	})

	var b strings.Builder
	fmt.Fprintf(&b, "codient: checkpoints for session %s\n", sessionID)
	cur := strings.TrimSpace(currentCheckpointID)

	var printNode func(n TreeNode, prefix string, isLast bool)
	printNode = func(n TreeNode, prefix string, isLast bool) {
		connector := "|-- "
		if isLast {
			connector = "`-- "
		}
		branch := ""
		if lbl := strings.TrimSpace(n.Branch); lbl != "" && !strings.EqualFold(lbl, "main") {
			branch = fmt.Sprintf(" branch=%q", lbl)
		}
		mark := " "
		if n.ID == cur {
			mark = "*"
		}
		name := strings.TrimSpace(n.Name)
		if name == "" {
			name = "(unnamed)"
		}
		sha := ShortSHA(n.GitSHA)
		if sha != "" {
			sha = " " + sha
		}
		fmt.Fprintf(&b, "%s%s[%s] turn %d %q%s%s\n", prefix, connector, mark, n.Turn, name, sha, branch)

		kids := children[n.ID]
		ext := "|   "
		if isLast {
			ext = "    "
		}
		childPrefix := prefix + ext
		for i, c := range kids {
			printNode(c, childPrefix, i == len(kids)-1)
		}
	}
	for i, r := range roots {
		printNode(r, "", i == len(roots)-1)
	}
	return b.String(), nil
}
