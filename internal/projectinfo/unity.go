package projectinfo

import (
	"os"
	"path/filepath"
	"strings"
)

// LooksLikeUnityEditorProject reports whether root looks like a Unity project
// (Assets/ plus ProjectSettings/ProjectVersion.txt). Used to tailor ACP prompts
// when the Codient Unity editor is the client.
func LooksLikeUnityEditorProject(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	assets := filepath.Join(root, "Assets")
	st, err := os.Stat(assets)
	if err != nil || !st.IsDir() {
		return false
	}
	ver := filepath.Join(root, "ProjectSettings", "ProjectVersion.txt")
	_, err = os.Stat(ver)
	return err == nil
}
