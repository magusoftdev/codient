package projectinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeUnityEditorProject(t *testing.T) {
	root := t.TempDir()
	if LooksLikeUnityEditorProject(root) {
		t.Fatal("empty dir should not match")
	}
	if err := os.MkdirAll(filepath.Join(root, "Assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ProjectSettings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ProjectSettings", "ProjectVersion.txt"), []byte("m_EditorVersion: 6000.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeUnityEditorProject(root) {
		t.Fatal("expected Unity project markers to match")
	}
}
