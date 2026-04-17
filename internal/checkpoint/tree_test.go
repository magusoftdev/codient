package checkpoint

import (
	"encoding/json"
	"testing"
)

func TestSummarizeBranches(t *testing.T) {
	t.Parallel()
	idx := &TreeIndex{
		SessionID: "s",
		Nodes: []TreeNode{
			{ID: "a", Name: "one", Branch: "main", Turn: 1, CreatedAt: "2020-01-01T00:00:00Z"},
			{ID: "b", Name: "two", Branch: "main", Turn: 2, CreatedAt: "2020-01-02T00:00:00Z"},
			{ID: "c", Name: "alt", Branch: "fork", Turn: 3, CreatedAt: "2020-01-03T00:00:00Z"},
		},
	}
	sum := SummarizeBranches(idx, "main")
	if len(sum) != 2 {
		t.Fatalf("got %d summaries", len(sum))
	}
}

func TestRenderTreeSmoke(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sid := "sess"
	cp := &Checkpoint{SessionID: sid, Name: "root", Turn: 0, Mode: "ask", Messages: []json.RawMessage{}}
	if err := Save(dir, cp); err != nil {
		t.Fatal(err)
	}
	out, err := RenderTree(dir, sid, cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("empty render")
	}
}
