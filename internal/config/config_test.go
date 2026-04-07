package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoad_StreamWithToolsEnv(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_WORKSPACE", "/w")
	t.Setenv("CODIENT_STREAM_WITH_TOOLS", "1")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.StreamWithTools {
		t.Fatal("expected StreamWithTools when CODIENT_STREAM_WITH_TOOLS=1")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("AGENT_MAX_TOOL_STEPS", "")
	t.Setenv("LLM_MAX_CONCURRENT", "")
	t.Setenv("CODIENT_WORKSPACE", "")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != defaultBaseURL {
		t.Fatalf("BaseURL: got %q", c.BaseURL)
	}
	if c.APIKey != defaultAPIKey {
		t.Fatalf("APIKey: got %q", c.APIKey)
	}
	if c.Model != "" {
		t.Fatalf("Model should be empty by default: got %q", c.Model)
	}
	if c.MaxToolSteps != defaultMaxToolSteps {
		t.Fatalf("MaxToolSteps: got %d", c.MaxToolSteps)
	}
	if c.MaxConcurrent != defaultMaxConcurrent {
		t.Fatalf("MaxConcurrent: got %d", c.MaxConcurrent)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wantWS, err := filepath.Abs(wd)
	if err != nil {
		t.Fatal(err)
	}
	if c.Workspace != wantWS {
		t.Fatalf("Workspace default cwd: got %q want %q", c.Workspace, wantWS)
	}
	if len(c.ExecAllowlist) != 3 || c.ExecAllowlist[0] != "go" || c.ExecAllowlist[1] != "git" {
		t.Fatalf("default ExecAllowlist: %#v", c.ExecAllowlist)
	}
	wantShell := "sh"
	if runtime.GOOS == "windows" {
		wantShell = "cmd"
	}
	if c.ExecAllowlist[2] != wantShell {
		t.Fatalf("default ExecAllowlist shell: got %q want %q", c.ExecAllowlist[2], wantShell)
	}
}

func TestLoad_FromConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_WORKSPACE", "/w")
	t.Setenv("AGENT_MAX_TOOL_STEPS", "5")
	t.Setenv("LLM_MAX_CONCURRENT", "2")

	pc := &PersistentConfig{
		BaseURL: "http://example.com/v1/",
		APIKey:  "secret",
		Model:   "m1",
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://example.com/v1" {
		t.Fatalf("BaseURL trim: got %q", c.BaseURL)
	}
	if c.APIKey != "secret" || c.Model != "m1" {
		t.Fatalf("credentials: %+v", c)
	}
	if c.MaxToolSteps != 5 || c.MaxConcurrent != 2 {
		t.Fatalf("limits: %+v", c)
	}
	if c.Workspace != "/w" {
		t.Fatalf("workspace: got %q", c.Workspace)
	}
}

func TestLoad_InvalidMaxToolSteps(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("AGENT_MAX_TOOL_STEPS", "0")
	t.Setenv("LLM_MAX_CONCURRENT", "1")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_InvalidMaxConcurrent(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("AGENT_MAX_TOOL_STEPS", "1")
	t.Setenv("LLM_MAX_CONCURRENT", "0")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRequireModel(t *testing.T) {
	c := &Config{Model: ""}
	if err := c.RequireModel(); err == nil {
		t.Fatal("expected error")
	}
	if err := c.RequireModel(); err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	c.Model = "x"
	if err := c.RequireModel(); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_ExecDisable(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_EXEC_DISABLE", "1")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ExecAllowlist) != 0 {
		t.Fatalf("expected empty allowlist when disabled: %#v", c.ExecAllowlist)
	}
}

func TestLoad_ExecAllowlist(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_EXEC_ALLOWLIST", "go, Git ,GO.exe, git")
	t.Setenv("CODIENT_EXEC_TIMEOUT_SEC", "45")
	t.Setenv("CODIENT_EXEC_MAX_OUTPUT_BYTES", "4096")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ExecAllowlist) != 2 {
		t.Fatalf("deduped allowlist: %#v", c.ExecAllowlist)
	}
	if c.ExecAllowlist[0] != "go" || c.ExecAllowlist[1] != "git" {
		t.Fatalf("order/content: %#v", c.ExecAllowlist)
	}
	if c.ExecTimeoutSeconds != 45 || c.ExecMaxOutputBytes != 4096 {
		t.Fatalf("exec limits: timeout=%d out=%d", c.ExecTimeoutSeconds, c.ExecMaxOutputBytes)
	}
}

func TestLoad_ExecTimeoutClamp(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_EXEC_TIMEOUT_SEC", "999999")
	t.Setenv("CODIENT_EXEC_MAX_OUTPUT_BYTES", "999999999")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ExecTimeoutSeconds != maxExecTimeoutSec {
		t.Fatalf("timeout clamp: got %d", c.ExecTimeoutSeconds)
	}
	if c.ExecMaxOutputBytes != maxExecMaxOutputBytes {
		t.Fatalf("output clamp: got %d", c.ExecMaxOutputBytes)
	}
}

func TestEffectiveWorkspace(t *testing.T) {
	c := &Config{Workspace: "/a"}
	if c.EffectiveWorkspace() != "/a" {
		t.Fatalf("got %q", c.EffectiveWorkspace())
	}
	c.Workspace = "  "
	if c.EffectiveWorkspace() != "" {
		t.Fatalf("whitespace-only: got %q", c.EffectiveWorkspace())
	}
}

func TestPersistentConfig_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)

	pc := &PersistentConfig{
		BaseURL: "http://myserver:8080/v1",
		APIKey:  "sk-test-key",
		Model:   "my-model-id",
	}
	if err := SavePersistentConfig(pc); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BaseURL != pc.BaseURL {
		t.Fatalf("BaseURL: got %q want %q", loaded.BaseURL, pc.BaseURL)
	}
	if loaded.APIKey != pc.APIKey {
		t.Fatalf("APIKey: got %q want %q", loaded.APIKey, pc.APIKey)
	}
	if loaded.Model != pc.Model {
		t.Fatalf("Model: got %q want %q", loaded.Model, pc.Model)
	}

	// Verify the file on disk is valid JSON.
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("config.json not valid JSON: %v", err)
	}
}

func TestPersistentConfig_MissingFile(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())

	pc, err := LoadPersistentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if pc.BaseURL != "" || pc.APIKey != "" || pc.Model != "" {
		t.Fatalf("expected empty defaults, got %+v", pc)
	}
}

func TestLoad_SearchDefaults(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_WORKSPACE", "/w")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SearchBaseURL != "" {
		t.Fatalf("SearchBaseURL should be empty by default: got %q", c.SearchBaseURL)
	}
	if c.SearchMaxResults != defaultSearchMaxResults {
		t.Fatalf("SearchMaxResults default: got %d", c.SearchMaxResults)
	}
}

func TestLoad_SearchEnvVars(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_WORKSPACE", "/w")
	t.Setenv("CODIENT_SEARCH_URL", "http://localhost:8080")
	t.Setenv("CODIENT_SEARCH_MAX_RESULTS", "8")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SearchBaseURL != "http://localhost:8080" {
		t.Fatalf("SearchBaseURL: got %q", c.SearchBaseURL)
	}
	if c.SearchMaxResults != 8 {
		t.Fatalf("SearchMaxResults: got %d", c.SearchMaxResults)
	}
}

func TestLoad_SearchMaxResultsClamp(t *testing.T) {
	t.Setenv("CODIENT_STATE_DIR", t.TempDir())
	t.Setenv("CODIENT_WORKSPACE", "/w")
	t.Setenv("CODIENT_SEARCH_MAX_RESULTS", "50")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SearchMaxResults != maxSearchMaxResults {
		t.Fatalf("SearchMaxResults clamp: got %d want %d", c.SearchMaxResults, maxSearchMaxResults)
	}
}

func TestPersistentConfig_FeedsIntoLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODIENT_STATE_DIR", dir)
	t.Setenv("CODIENT_WORKSPACE", "/tmp/ws")

	if err := SavePersistentConfig(&PersistentConfig{Model: "persisted-model"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "persisted-model" {
		t.Fatalf("model from config file: got %q", c.Model)
	}
	if c.BaseURL != defaultBaseURL {
		t.Fatalf("BaseURL should default: got %q", c.BaseURL)
	}
}
