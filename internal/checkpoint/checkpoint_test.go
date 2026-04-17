package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sid := "test_sess"
	cp := &Checkpoint{
		SessionID: sid,
		Name:      "after-setup",
		Turn:      3,
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)},
		Mode:      "build",
		Model:     "m",
		GitSHA:    "abc123",
		Branch:    "main",
	}
	if err := Save(dir, cp); err != nil {
		t.Fatal(err)
	}
	if cp.ID == "" {
		t.Fatal("expected ID assigned")
	}
	loaded, err := Load(dir, sid, cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "after-setup" || loaded.Turn != 3 || loaded.Mode != "build" {
		t.Fatalf("unexpected %+v", loaded)
	}
}

func TestFindByNameAndTurn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sid := "s1"
	cp1 := &Checkpoint{SessionID: sid, Name: "alpha", Turn: 1, Mode: "ask", Messages: []json.RawMessage{}}
	if err := Save(dir, cp1); err != nil {
		t.Fatal(err)
	}
	cp2 := &Checkpoint{SessionID: sid, Name: "beta", Turn: 2, Mode: "ask", Messages: []json.RawMessage{}, ParentID: cp1.ID}
	if err := Save(dir, cp2); err != nil {
		t.Fatal(err)
	}

	m, err := FindByName(dir, sid, "alpha")
	if err != nil || len(m) != 1 {
		t.Fatalf("FindByName: %v %#v", err, m)
	}
	m2, err := FindByTurn(dir, sid, 2)
	if err != nil || len(m2) != 1 {
		t.Fatalf("FindByTurn: %v %#v", err, m2)
	}
}

func TestResolveQueryTurnNumber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sid := "s1"
	cp := &Checkpoint{SessionID: sid, Name: "t", Turn: 5, Mode: "build", Messages: []json.RawMessage{}}
	if err := Save(dir, cp); err != nil {
		t.Fatal(err)
	}
	m, err := ResolveQuery(dir, sid, "5")
	if err != nil || len(m) != 1 || m[0].Turn != 5 {
		t.Fatalf("ResolveQuery: %v %#v", err, m)
	}
}

func TestTreeIndexWrittenWithSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sid := "sess"
	cp := &Checkpoint{SessionID: sid, Name: "n", Turn: 1, Mode: "ask", Messages: []json.RawMessage{}}
	if err := Save(dir, cp); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".codient", "checkpoints", sid, "tree.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}
