package codientcli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"codient/internal/config"
)

type agentBenchScenario struct {
	Name                  string
	Prompt                string
	Files                 map[string]string
	PublicTestCmd         string
	AutoCheckCmd          string
	HiddenVerifier        func(*agentBenchRun) error
	MaxTurns              int
	Timeout               time.Duration
	RepoMapTokens         int
	WantChangedPaths      []string
	ForbiddenChangedPaths []string
	WantTools             []string
	WantAnyToolGroups     [][]string
	AllowExtraChanges     bool
	ExpectNoChanges       bool
}

type agentBenchRun struct {
	Scenario  agentBenchScenario
	Workspace string
	StateDir  string
	Artifacts string
	Env       []string
	Stdout    string
	Stderr    string
	LogPath   string
	Diff      string
	Changed   []string
	Headless  agentBenchHeadlessResult
	Duration  time.Duration
}

type agentBenchHeadlessResult struct {
	Reply         string   `json:"reply"`
	SessionID     string   `json:"session_id,omitempty"`
	Workspace     string   `json:"workspace,omitempty"`
	ToolsUsed     []string `json:"tools_used"`
	FilesModified []string `json:"files_modified,omitempty"`
	ExitReason    string   `json:"exit_reason"`
	Error         string   `json:"error,omitempty"`
}

func agentBenchModuleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" {
		t.Fatal("GOMOD empty")
	}
	return filepath.Dir(mod)
}

func agentBenchBuildBinary(t *testing.T) string {
	t.Helper()
	name := "codient_agent_bench"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/codient")
	cmd.Dir = agentBenchModuleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func agentBenchWriteFiles(root string, files map[string]string) error {
	files = agentBenchFilesWithGitignore(files)
	for rel, content := range files {
		if strings.TrimSpace(rel) == "" {
			return fmt.Errorf("empty fixture path")
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func agentBenchFilesWithGitignore(files map[string]string) map[string]string {
	out := make(map[string]string, len(files)+1)
	for k, v := range files {
		out[k] = v
	}
	const ignore = ".codient/\ncodient-run.jsonl\n"
	if cur, ok := out[".gitignore"]; ok {
		if !strings.Contains(cur, ".codient/") {
			if cur != "" && !strings.HasSuffix(cur, "\n") {
				cur += "\n"
			}
			cur += ignore
		}
		out[".gitignore"] = cur
	} else {
		out[".gitignore"] = ignore
	}
	return out
}

func agentBenchInitGit(root string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found: %w", err)
	}
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"-c", "user.email=agent-bench@example.test", "-c", "user.name=Agent Bench", "commit", "-m", "baseline"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

func agentBenchRunShell(root, line string, timeout time.Duration) (string, error) {
	return agentBenchRunShellEnv(root, line, timeout, nil)
}

func agentBenchRunShellEnv(root, line string, timeout time.Duration, env []string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	argv := []string{"sh", "-c", line}
	if runtime.GOOS == "windows" {
		argv = []string{"cmd", "/c", line}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	if env != nil {
		cmd.Env = env
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return out.String(), fmt.Errorf("%w\n%s", err, out.String())
		}
		return out.String(), nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return out.String(), fmt.Errorf("command timed out after %v\n%s", timeout, out.String())
	}
}

func agentBenchChangedPaths(root string) ([]string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" || len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[2:])
		if idx := strings.LastIndex(p, " -> "); idx >= 0 {
			p = strings.TrimSpace(p[idx+4:])
		}
		p = filepath.ToSlash(p)
		if p == "" || strings.HasPrefix(p, ".codient/") || p == "codient-run.jsonl" {
			continue
		}
		seen[p] = struct{}{}
	}
	var paths []string
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func agentBenchGitDiff(root string) string {
	cmd := exec.Command("git", "diff", "--no-ext-diff", "--", ".")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("git diff failed: %v\n%s", err, out)
	}
	return string(out)
}

func agentBenchVerifyChangedPaths(changed []string, sc agentBenchScenario) error {
	set := map[string]struct{}{}
	for _, p := range changed {
		set[filepath.ToSlash(p)] = struct{}{}
	}
	if sc.ExpectNoChanges && len(changed) > 0 {
		return fmt.Errorf("expected no workspace changes, got %v", changed)
	}
	for _, p := range sc.WantChangedPaths {
		p = filepath.ToSlash(p)
		if _, ok := set[p]; !ok {
			return fmt.Errorf("expected changed path %q, got %v", p, changed)
		}
	}
	for _, p := range sc.ForbiddenChangedPaths {
		p = filepath.ToSlash(p)
		if _, ok := set[p]; ok {
			return fmt.Errorf("forbidden path changed: %s", p)
		}
	}
	if !sc.AllowExtraChanges && len(sc.WantChangedPaths) > 0 {
		want := map[string]struct{}{}
		for _, p := range sc.WantChangedPaths {
			want[filepath.ToSlash(p)] = struct{}{}
		}
		for _, p := range changed {
			if _, ok := want[filepath.ToSlash(p)]; !ok {
				return fmt.Errorf("unexpected changed path %q; wanted only %v", p, sc.WantChangedPaths)
			}
		}
	}
	return nil
}

func agentBenchVerifyTools(got, want []string) error {
	return agentBenchVerifyToolExpectations(got, want, nil)
}

func agentBenchVerifyToolExpectations(got, want []string, wantAnyGroups [][]string) error {
	if len(want) == 0 {
		if len(wantAnyGroups) == 0 {
			return nil
		}
	}
	have := map[string]struct{}{}
	for _, n := range got {
		have[n] = struct{}{}
	}
	for _, n := range want {
		if _, ok := have[n]; !ok {
			return fmt.Errorf("expected tool %q in tools_used=%v", n, got)
		}
	}
	for _, group := range wantAnyGroups {
		found := ""
		for _, n := range group {
			if _, ok := have[n]; ok {
				found = n
				break
			}
		}
		if found == "" {
			return fmt.Errorf("expected one of tools %v in tools_used=%v", group, got)
		}
	}
	return nil
}

func agentBenchParseHeadlessJSON(s string) (agentBenchHeadlessResult, error) {
	var out agentBenchHeadlessResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		return out, err
	}
	if out.ExitReason == "" {
		return out, errors.New("missing exit_reason")
	}
	return out, nil
}

func agentBenchIsLocalBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func agentBenchNonLocalBaseURLs(cfg *config.Config) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range []string{cfg.BaseURL, cfg.LowReasoning.BaseURL, cfg.HighReasoning.BaseURL} {
		raw = strings.TrimSpace(raw)
		if raw == "" || agentBenchIsLocalBaseURL(raw) {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	sort.Strings(out)
	return out
}

func safeAgentBenchName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			cur := b.String()
			if cur[len(cur)-1] == '-' {
				continue
			}
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "scenario"
	}
	return out
}

func agentBenchConfigMapFromConfig(cfg *config.Config, sc agentBenchScenario) map[string]any {
	repoMapTokens := 4096
	if sc.RepoMapTokens != 0 {
		repoMapTokens = sc.RepoMapTokens
	}
	m := map[string]any{
		"schema_version":        2,
		"base_url":              cfg.BaseURL,
		"api_key":               cfg.APIKey,
		"model":                 cfg.Model,
		"exec_allowlist":        "go,git,sh,cmd",
		"sandbox_mode":          "off",
		"git_auto_commit":       false,
		"design_save":           false,
		"checkpoint_auto":       "off",
		"autocompact_threshold": 0,
		"lint_cmd":              "off",
		"test_cmd":              "off",
		"repo_map_tokens":       repoMapTokens,
		"stream_reply":          false,
		"plain":                 true,
		"quiet":                 true,
	}
	if strings.TrimSpace(sc.AutoCheckCmd) != "" {
		m["autocheck_cmd"] = strings.TrimSpace(sc.AutoCheckCmd)
		m["autocheck_fix_max_retries"] = 1
		m["autocheck_fix_stop_on_no_progress"] = true
	} else {
		m["autocheck_cmd"] = "off"
	}
	if cfg.LowReasoning.BaseURL != "" {
		m["low_reasoning_base_url"] = cfg.LowReasoning.BaseURL
	}
	if cfg.LowReasoning.APIKey != "" {
		m["low_reasoning_api_key"] = cfg.LowReasoning.APIKey
	}
	if cfg.LowReasoning.Model != "" {
		m["low_reasoning_model"] = cfg.LowReasoning.Model
	}
	if cfg.LowReasoning.MaxCompletionTokens > 0 {
		m["low_reasoning_max_completion_tokens"] = cfg.LowReasoning.MaxCompletionTokens
	}
	if cfg.HighReasoning.BaseURL != "" {
		m["high_reasoning_base_url"] = cfg.HighReasoning.BaseURL
	}
	if cfg.HighReasoning.APIKey != "" {
		m["high_reasoning_api_key"] = cfg.HighReasoning.APIKey
	}
	if cfg.HighReasoning.Model != "" {
		m["high_reasoning_model"] = cfg.HighReasoning.Model
	}
	if cfg.DisableIntentHeuristic {
		m["disable_intent_heuristic"] = true
	}
	if cfg.ContextWindowTokens > 0 {
		m["context_window"] = cfg.ContextWindowTokens
	}
	if cfg.ContextReserveTokens > 0 {
		m["context_reserve"] = cfg.ContextReserveTokens
	}
	return m
}

func agentBenchWriteConfig(stateDir string, cfg *config.Config, sc agentBenchScenario) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(agentBenchConfigMapFromConfig(cfg, sc), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "config.json"), b, 0o644)
}

func agentBenchWriteArtifacts(t *testing.T, r *agentBenchRun) {
	t.Helper()
	if r == nil {
		return
	}
	target, err := os.MkdirTemp("", "codient-agent-bench-"+safeAgentBenchName(r.Scenario.Name)+"-failure-*")
	if err != nil {
		t.Logf("agent-bench artifact directory creation failed: %v", err)
		return
	}
	r.Artifacts = target
	_ = os.WriteFile(filepath.Join(target, "prompt.txt"), []byte(r.Scenario.Prompt), 0o644)
	_ = os.WriteFile(filepath.Join(target, "stdout.json"), []byte(r.Stdout), 0o644)
	_ = os.WriteFile(filepath.Join(target, "stderr.txt"), []byte(r.Stderr), 0o644)
	_ = os.WriteFile(filepath.Join(target, "diff.patch"), []byte(r.Diff), 0o644)
	if r.LogPath != "" {
		if b, err := os.ReadFile(r.LogPath); err == nil {
			_ = os.WriteFile(filepath.Join(target, "agent.jsonl"), b, 0o644)
		}
	}
	t.Logf("agent-bench artifacts: %s", target)
}

func agentBenchAppendResult(path string, r *agentBenchRun, runErr error) {
	if strings.TrimSpace(path) == "" || r == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	row := map[string]any{
		"name":        r.Scenario.Name,
		"duration_ms": r.Duration.Milliseconds(),
		"exit_reason": r.Headless.ExitReason,
		"tools_used":  r.Headless.ToolsUsed,
		"changed":     r.Changed,
		"workspace":   r.Workspace,
		"artifacts":   r.Artifacts,
		"passed":      runErr == nil,
	}
	if runErr != nil {
		row["error"] = runErr.Error()
	}
	b, err := json.Marshal(row)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func TestAgentBenchLocalBaseURLGuard(t *testing.T) {
	if !agentBenchIsLocalBaseURL("http://127.0.0.1:13305/v1") {
		t.Fatal("127.0.0.1 should be local")
	}
	if !agentBenchIsLocalBaseURL("http://localhost:11434/v1") {
		t.Fatal("localhost should be local")
	}
	if agentBenchIsLocalBaseURL("https://api.openai.com/v1") {
		t.Fatal("public OpenAI endpoint should require explicit remote opt-in")
	}
	bad := agentBenchNonLocalBaseURLs(&config.Config{
		BaseURL: "http://127.0.0.1:13305/v1",
		HighReasoning: config.ReasoningTier{
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if len(bad) != 1 || bad[0] != "https://api.openai.com/v1" {
		t.Fatalf("unexpected non-local URLs: %v", bad)
	}
}

func TestAgentBenchParseHeadlessJSON(t *testing.T) {
	got, err := agentBenchParseHeadlessJSON(`{"reply":"ok","tools_used":["read_file"],"exit_reason":"complete"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reply != "ok" || len(got.ToolsUsed) != 1 || got.ToolsUsed[0] != "read_file" {
		t.Fatalf("unexpected parse: %#v", got)
	}
}

func TestAgentBenchConfigCopiesConnectionOnly(t *testing.T) {
	cfg := &config.Config{
		BaseURL:   "http://127.0.0.1:13305/v1",
		APIKey:    "secret",
		Model:     "local-model",
		Workspace: "/should/not/copy",
	}
	m := agentBenchConfigMapFromConfig(cfg, agentBenchScenario{AutoCheckCmd: "go test ./..."})
	if m["base_url"] != cfg.BaseURL || m["api_key"] != cfg.APIKey || m["model"] != cfg.Model {
		t.Fatalf("connection fields not copied: %#v", m)
	}
	if _, ok := m["workspace"]; ok {
		t.Fatalf("workspace should not be copied into benchmark config: %#v", m)
	}
	if m["git_auto_commit"] != false || m["design_save"] != false || m["autocheck_cmd"] != "go test ./..." || m["repo_map_tokens"] != 4096 {
		t.Fatalf("benchmark isolation/defaults missing: %#v", m)
	}
	m = agentBenchConfigMapFromConfig(cfg, agentBenchScenario{RepoMapTokens: -1})
	if m["repo_map_tokens"] != -1 {
		t.Fatalf("scenario repo map override missing: %#v", m)
	}
}

func TestAgentBenchFixtureDiffAndVerifier(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":       "module fixture\n\ngo 1.21\n",
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n",
	}
	if err := agentBenchWriteFiles(root, files); err != nil {
		t.Fatal(err)
	}
	if err := agentBenchInitGit(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { println(\"changed\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := agentBenchChangedPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	sc := agentBenchScenario{
		WantChangedPaths:      []string{"main.go"},
		ForbiddenChangedPaths: []string{"main_test.go"},
	}
	if err := agentBenchVerifyChangedPaths(changed, sc); err != nil {
		t.Fatal(err)
	}
	if err := agentBenchVerifyTools([]string{"read_file", "str_replace"}, []string{"str_replace"}); err != nil {
		t.Fatal(err)
	}
	if err := agentBenchVerifyToolExpectations([]string{"grep", "read_file"}, nil, [][]string{{"search_files", "grep"}}); err != nil {
		t.Fatal(err)
	}
}
