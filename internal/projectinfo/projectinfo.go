// Package projectinfo detects the project's language, framework, and key
// tooling from workspace marker files and returns a concise summary for
// injection into the agent system prompt.
package projectinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxFileRead = 4096
	maxOutput   = 1024
)

// Detect scans workspaceRoot for marker files and returns a short multi-line
// summary of the project's stack. Returns "" if the workspace is empty or no
// markers are found.
func Detect(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return ""
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return ""
	}

	var lines []string
	add := func(kvs ...string) {
		lines = append(lines, kvs...)
	}

	detectGo(root, add)
	detectNode(root, add)
	detectRust(root, add)
	detectPython(root, add)
	detectMeta(root, add)

	if len(lines) == 0 {
		return ""
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxOutput {
		out = out[:maxOutput]
	}
	return out
}

// detectGo extracts module path and Go version from go.mod.
func detectGo(root string, add func(...string)) {
	data := readHead(filepath.Join(root, "go.mod"))
	if data == "" {
		return
	}
	mod := firstMatch(reGoModule, data)
	ver := firstMatch(reGoVersion, data)
	lang := "Go"
	if ver != "" {
		lang += " (" + ver + ")"
	}
	add("Language: " + lang)
	if mod != "" {
		add("Module: " + mod)
	}
}

var (
	reGoModule  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	reGoVersion = regexp.MustCompile(`(?m)^go\s+([\d.]+)`)
)

// detectNode extracts package name, key framework deps, TS flag, and package manager.
func detectNode(root string, add func(...string)) {
	data := readHead(filepath.Join(root, "package.json"))
	if data == "" {
		return
	}
	var pkg struct {
		Name         string                     `json:"name"`
		Dependencies map[string]json.RawMessage `json:"dependencies"`
		DevDeps      map[string]json.RawMessage `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(data), &pkg) != nil {
		return
	}
	ts := fileExists(filepath.Join(root, "tsconfig.json"))
	lang := "JavaScript"
	if ts {
		lang = "TypeScript"
	}
	add("Language: " + lang)
	if pkg.Name != "" {
		add("Package: " + pkg.Name)
	}

	allDeps := mergeMaps(pkg.Dependencies, pkg.DevDeps)
	var frameworks []string
	for _, kw := range nodeFrameworkKeywords {
		if _, ok := allDeps[kw]; ok {
			frameworks = append(frameworks, kw)
		}
	}
	if len(frameworks) > 0 {
		add("Frameworks: " + strings.Join(frameworks, ", "))
	}

	pm := detectPackageManager(root)
	if pm != "" {
		add("Package manager: " + pm)
	}
}

var nodeFrameworkKeywords = []string{
	"next", "react", "vue", "nuxt", "svelte", "angular",
	"express", "fastify", "hono", "nestjs", "remix",
	"vite", "webpack", "esbuild", "tailwindcss",
}

func detectPackageManager(root string) string {
	switch {
	case fileExists(filepath.Join(root, "bun.lockb")) || fileExists(filepath.Join(root, "bun.lock")):
		return "bun"
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(root, "yarn.lock")):
		return "yarn"
	case fileExists(filepath.Join(root, "package-lock.json")):
		return "npm"
	default:
		return ""
	}
}

// detectRust extracts crate name and edition from Cargo.toml.
func detectRust(root string, add func(...string)) {
	data := readHead(filepath.Join(root, "Cargo.toml"))
	if data == "" {
		return
	}
	edition := firstMatch(reCargoEdition, data)
	lang := "Rust"
	if edition != "" {
		lang += " (edition " + edition + ")"
	}
	add("Language: " + lang)
	if name := firstMatch(reCargoName, data); name != "" {
		add("Crate: " + name)
	}
}

var (
	reCargoName    = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	reCargoEdition = regexp.MustCompile(`(?m)^edition\s*=\s*"([^"]+)"`)
)

// detectPython checks pyproject.toml, then requirements.txt as fallback.
func detectPython(root string, add func(...string)) {
	data := readHead(filepath.Join(root, "pyproject.toml"))
	if data != "" {
		add("Language: Python")
		if name := firstMatch(rePyName, data); name != "" {
			add("Package: " + name)
		}
		if backend := firstMatch(rePyBuildBackend, data); backend != "" {
			add("Build backend: " + backend)
		}
		return
	}
	if fileExists(filepath.Join(root, "requirements.txt")) {
		add("Language: Python")
	}
}

var (
	rePyName         = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	rePyBuildBackend = regexp.MustCompile(`(?m)^build-backend\s*=\s*"([^"]+)"`)
)

// detectMeta looks for cross-cutting build/infra signals.
func detectMeta(root string, add func(...string)) {
	var build []string
	if fileExists(filepath.Join(root, "Makefile")) {
		build = append(build, "Make")
	}
	if fileExists(filepath.Join(root, "CMakeLists.txt")) {
		build = append(build, "CMake")
	}
	if len(build) > 0 {
		add("Build: " + strings.Join(build, ", "))
	}

	var infra []string
	if fileExists(filepath.Join(root, "Dockerfile")) || fileExists(filepath.Join(root, "docker-compose.yml")) || fileExists(filepath.Join(root, "docker-compose.yaml")) {
		infra = append(infra, "Docker")
	}
	if hasGitHubActions(root) {
		infra = append(infra, "GitHub Actions")
	}
	if len(infra) > 0 {
		add("Infra: " + strings.Join(infra, ", "))
	}
}

func hasGitHubActions(root string) bool {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
			return true
		}
	}
	return false
}

// --- helpers ---

func readHead(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, maxFileRead)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func mergeMaps(a, b map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
