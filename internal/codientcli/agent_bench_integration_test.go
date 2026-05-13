//go:build integration

package codientcli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"codient/internal/config"
	"codient/internal/openaiclient"
)

func TestAgentBench(t *testing.T) {
	if testing.Short() {
		t.Skip("agent benchmark integration suite is disabled in short mode")
	}
	requireAgentBenchEnv(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not available: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := cfg.RequireModel(); err != nil {
		t.Skip(err)
	}
	if bad := agentBenchNonLocalBaseURLs(cfg); len(bad) > 0 && os.Getenv("CODIENT_AGENT_BENCH_ALLOW_REMOTE") != "1" {
		t.Skipf("refusing non-local base_url(s) %q; set CODIENT_AGENT_BENCH_ALLOW_REMOTE=1 to run against them", bad)
	}
	preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelPreflight()
	if err := agentBenchPreflightModels(preflightCtx, cfg); err != nil {
		t.Fatalf("agent benchmark model preflight failed: %v", err)
	}

	bin := agentBenchBuildBinary(t)
	resultsPath := strings.TrimSpace(os.Getenv("CODIENT_AGENT_BENCH_RESULTS"))
	for _, sc := range agentBenchScenarios() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			run, runErr := agentBenchRunScenario(t, bin, cfg, sc)
			if runErr != nil {
				agentBenchWriteArtifacts(t, run)
				agentBenchAppendResult(resultsPath, run, runErr)
				t.Fatal(runErr)
			}
			agentBenchAppendResult(resultsPath, run, runErr)
		})
	}
}

func requireAgentBenchEnv(t *testing.T) {
	t.Helper()
	for _, kv := range []struct {
		key  string
		want string
	}{
		{"CODIENT_INTEGRATION", "1"},
		{"CODIENT_AGENT_BENCH", "1"},
		{"CODIENT_INTEGRATION_STRICT_TOOLS", "1"},
	} {
		if os.Getenv(kv.key) != kv.want {
			t.Skipf("%s=%s required for agent benchmark integration suite", kv.key, kv.want)
		}
	}
}

func agentBenchRunScenario(t *testing.T, bin string, cfg *config.Config, sc agentBenchScenario) (*agentBenchRun, error) {
	t.Helper()

	timeout := sc.Timeout
	if timeout <= 0 {
		timeout = 4 * time.Minute
	}
	maxTurns := sc.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	root := t.TempDir()
	if os.Getenv("CODIENT_AGENT_BENCH_KEEP_WORKSPACES") == "1" {
		var err error
		root, err = os.MkdirTemp("", "codient-agent-bench-"+safeAgentBenchName(sc.Name)+"-*")
		if err != nil {
			t.Fatalf("create persistent benchmark workspace: %v", err)
		}
		t.Logf("keeping agent-bench workspace root: %s", root)
	}
	workspace := filepath.Join(root, "workspace")
	stateDir := filepath.Join(root, "state")
	artifacts := filepath.Join(root, "artifacts")
	for _, dir := range []string{workspace, stateDir, artifacts} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	if err := agentBenchWriteFiles(workspace, sc.Files); err != nil {
		return &agentBenchRun{Scenario: sc, Workspace: workspace, StateDir: stateDir, Artifacts: artifacts}, fmt.Errorf("write fixture: %w", err)
	}
	if err := agentBenchInitGit(workspace); err != nil {
		return &agentBenchRun{Scenario: sc, Workspace: workspace, StateDir: stateDir, Artifacts: artifacts}, fmt.Errorf("init git fixture: %w", err)
	}
	if err := agentBenchWriteConfig(stateDir, cfg, sc); err != nil {
		return &agentBenchRun{Scenario: sc, Workspace: workspace, StateDir: stateDir, Artifacts: artifacts}, fmt.Errorf("write benchmark config: %w", err)
	}

	env, err := agentBenchEnv(stateDir)
	if err != nil {
		return &agentBenchRun{Scenario: sc, Workspace: workspace, StateDir: stateDir, Artifacts: artifacts}, err
	}
	logPath := filepath.Join(artifacts, "agent.jsonl")
	run := &agentBenchRun{
		Scenario:  sc,
		Workspace: workspace,
		StateDir:  stateDir,
		Artifacts: artifacts,
		LogPath:   logPath,
		Env:       env,
	}

	system := strings.Join([]string{
		"Benchmark-style integration test.",
		"Inspect the repository before editing.",
		"Prefer focused production-code changes.",
		"Do not edit tests unless the user explicitly asks.",
		"Run the relevant local checks before the final answer when the task involves code changes.",
	}, " ")
	args := []string{
		"-print",
		"-plain",
		"-yes",
		"-force",
		"-new-session",
		"-auto-approve", "all",
		"-output-format", "json",
		"-workspace", workspace,
		"-timeout", timeout.String(),
		"-max-turns", strconv.Itoa(maxTurns),
		"-log", logPath,
		"-progress",
		"-sandbox", "off",
		"-system", system,
		"-prompt", sc.Prompt,
	}

	start := time.Now()
	stdout, stderr, cmdErr := agentBenchRunCodient(bin, args, env, timeout+30*time.Second)
	run.Duration = time.Since(start)
	run.Stdout = stdout
	run.Stderr = stderr
	if strings.TrimSpace(stdout) != "" {
		if parsed, err := agentBenchParseHeadlessJSON(stdout); err != nil {
			cmdErr = errors.Join(cmdErr, fmt.Errorf("parse stdout json: %w", err))
		} else {
			run.Headless = parsed
		}
	}

	run.Diff = agentBenchGitDiff(workspace)
	if changed, err := agentBenchChangedPaths(workspace); err == nil {
		run.Changed = changed
	}
	if cmdErr != nil {
		return run, fmt.Errorf("codient command failed: %w\nstderr:\n%s", cmdErr, stderr)
	}
	if run.Headless.ExitReason != "complete" {
		return run, fmt.Errorf("headless exit_reason=%q error=%q", run.Headless.ExitReason, run.Headless.Error)
	}

	if sc.PublicTestCmd != "" {
		if out, err := agentBenchRunShellEnv(workspace, sc.PublicTestCmd, 90*time.Second, env); err != nil {
			run.Stderr += "\n\npublic verifier:\n" + out
			run.Diff = agentBenchGitDiff(workspace)
			run.Changed, _ = agentBenchChangedPaths(workspace)
			return run, fmt.Errorf("public verifier %q failed: %w", sc.PublicTestCmd, err)
		}
	}
	if sc.HiddenVerifier != nil {
		if err := sc.HiddenVerifier(run); err != nil {
			run.Diff = agentBenchGitDiff(workspace)
			run.Changed, _ = agentBenchChangedPaths(workspace)
			return run, fmt.Errorf("hidden verifier failed: %w", err)
		}
	}

	run.Diff = agentBenchGitDiff(workspace)
	changed, err := agentBenchChangedPaths(workspace)
	if err != nil {
		return run, fmt.Errorf("changed paths: %w", err)
	}
	run.Changed = changed
	if err := agentBenchVerifyChangedPaths(changed, sc); err != nil {
		return run, err
	}
	if err := agentBenchVerifyTools(run.Headless.ToolsUsed, sc.WantTools); err != nil {
		return run, err
	}
	if err := agentBenchVerifyToolExpectations(run.Headless.ToolsUsed, nil, sc.WantAnyToolGroups); err != nil {
		return run, err
	}
	return run, nil
}

type agentBenchModelRef struct {
	Label  string
	Base   string
	APIKey string
	Model  string
}

func agentBenchPreflightModels(ctx context.Context, cfg *config.Config) error {
	refs := agentBenchConfiguredModelRefs(cfg)
	groups := map[string][]agentBenchModelRef{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Model) == "" {
			return fmt.Errorf("%s is empty", ref.Label)
		}
		key := strings.TrimSpace(ref.Base) + "\x00" + strings.TrimSpace(ref.APIKey)
		groups[key] = append(groups[key], ref)
	}
	for _, group := range groups {
		first := group[0]
		client := openaiclient.NewFromParams(first.Base, first.APIKey, first.Model, cfg.MaxConcurrent)
		models, err := client.ListModels(ctx)
		if err != nil {
			return fmt.Errorf("list models at %s: %w", first.Base, err)
		}
		var missing []string
		for _, ref := range group {
			if !agentBenchModelAvailable(models, ref.Model) {
				missing = append(missing, fmt.Sprintf("%s=%s", ref.Label, ref.Model))
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("configured model(s) not available at %s: %s (available: %s)", first.Base, strings.Join(missing, ", "), agentBenchSummarizeModels(models, 12))
		}
	}
	return nil
}

func agentBenchConfiguredModelRefs(cfg *config.Config) []agentBenchModelRef {
	refs := []agentBenchModelRef{{
		Label:  "model",
		Base:   cfg.BaseURL,
		APIKey: cfg.APIKey,
		Model:  cfg.Model,
	}}
	lowBase, lowKey, lowModel := cfg.ConnectionForTier(config.TierLow)
	highBase, highKey, highModel := cfg.ConnectionForTier(config.TierHigh)
	refs = append(refs,
		agentBenchModelRef{Label: "low_reasoning_model", Base: lowBase, APIKey: lowKey, Model: lowModel},
		agentBenchModelRef{Label: "high_reasoning_model", Base: highBase, APIKey: highKey, Model: highModel},
	)
	return refs
}

func agentBenchModelAvailable(models []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, got := range models {
		got = strings.TrimSpace(got)
		if strings.EqualFold(got, want) {
			return true
		}
		if i := strings.LastIndex(got, "/"); i >= 0 && strings.EqualFold(got[i+1:], want) {
			return true
		}
	}
	return false
}

func agentBenchSummarizeModels(models []string, max int) string {
	if len(models) == 0 {
		return "(none)"
	}
	if max <= 0 || len(models) <= max {
		return strings.Join(models, ", ")
	}
	return strings.Join(models[:max], ", ") + fmt.Sprintf(", ... (%d total)", len(models))
}

func TestAgentBenchModelPreflightReportsMissingTier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"available-model"}]}`)
	}))
	defer srv.Close()
	cfg := &config.Config{
		BaseURL: srv.URL + "/v1",
		APIKey:  "test",
		Model:   "available-model",
		HighReasoning: config.ReasoningTier{
			Model: "missing-model",
		},
	}
	err := agentBenchPreflightModels(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected missing model error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "high_reasoning_model=missing-model") || !strings.Contains(msg, "available-model") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func agentBenchRunCodient(bin string, args, env []string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("codient timed out after %v", timeout)
	}
	return outBuf.String(), errBuf.String(), err
}

func agentBenchEnv(stateDir string) ([]string, error) {
	dirs := map[string]string{
		"GOCACHE":        filepath.Join(stateDir, "gocache"),
		"GOMODCACHE":     filepath.Join(stateDir, "gomodcache"),
		"GOTMPDIR":       filepath.Join(stateDir, "gotmp"),
		"HOME":           filepath.Join(stateDir, "home"),
		"USERPROFILE":    filepath.Join(stateDir, "home"),
		"XDG_CACHE_HOME": filepath.Join(stateDir, "xdg-cache"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create env dir %s: %w", dir, err)
		}
	}
	env := append([]string{}, os.Environ()...)
	env = upsertEnv(env, "CODIENT_STATE_DIR", stateDir)
	for k, v := range dirs {
		env = upsertEnv(env, k, v)
	}
	return env, nil
}

func upsertEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

func agentBenchScenarios() []agentBenchScenario {
	return []agentBenchScenario{
		localizedBugFixScenario(),
		multiFileAPIMigrationScenario(),
		featureAdditionScenario(),
		compileRepairScenario(),
		hiddenVerifierScenario(),
		searchHeavyEditScenario(),
		planToBuildPathScenario(),
		autoCheckRepairScenario(),
		noTestTamperingScenario(),
		readOnlyQuerySafetyScenario(),
	}
}

func localizedBugFixScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "localized-bug-fix",
		Prompt: strings.TrimSpace(`
Bug report: parsing an empty count must be rejected instead of silently accepting it as zero.
Fix the production code so the existing tests pass. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module localizedbugfix\n\ngo 1.21\n",
			"count/count.go": `package count

import "strconv"

func ParseCount(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}
`,
			"count/count_test.go": `package count

import "testing"

func TestParseCountRejectsEmptyInput(t *testing.T) {
	if _, err := ParseCount(""); err == nil {
		t.Fatal("empty input should return an error")
	}
}

func TestParseCountAcceptsNumber(t *testing.T) {
	got, err := ParseCount("42")
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"count/count.go"},
		ForbiddenChangedPaths: []string{"count/count_test.go"},
		WantTools:             []string{"read_file"},
	}
}

func multiFileAPIMigrationScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "multi-file-api-migration",
		Prompt: strings.TrimSpace(`
Migrate the greeting formatter to accept a Person struct instead of separate first/last strings.
Update all callers while preserving behavior. The tests describe the new API. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module apimigration\n\ngo 1.21\n",
			"people/format.go": `package people

import "strings"

func DisplayName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}
`,
			"cards/card.go": `package cards

import "apimigration/people"

func Render(first, last string) string {
	return "Hello, " + people.DisplayName(first, last) + "!"
}
`,
			"emails/email.go": `package emails

import "apimigration/people"

func Subject(first, last string) string {
	return "Welcome " + people.DisplayName(first, last)
}
`,
			"people/format_test.go": `package people

import "testing"

func TestDisplayNameUsesPerson(t *testing.T) {
	got := DisplayName(Person{First: "Ada", Last: "Lovelace"})
	if got != "Ada Lovelace" {
		t.Fatalf("got %q", got)
	}
}
`,
			"cards/card_test.go": `package cards

import "testing"

func TestRenderUsesPerson(t *testing.T) {
	got := Render("Grace", "Hopper")
	if got != "Hello, Grace Hopper!" {
		t.Fatalf("got %q", got)
	}
}
`,
			"emails/email_test.go": `package emails

import "testing"

func TestSubjectUsesPerson(t *testing.T) {
	got := Subject("Katherine", "Johnson")
	if got != "Welcome Katherine Johnson" {
		t.Fatalf("got %q", got)
	}
}
`,
		},
		PublicTestCmd:    "go test ./...",
		WantChangedPaths: []string{"cards/card.go", "emails/email.go", "people/format.go"},
		ForbiddenChangedPaths: []string{
			"cards/card_test.go",
			"emails/email_test.go",
			"people/format_test.go",
		},
		WantTools: []string{"read_file"},
	}
}

func featureAdditionScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "feature-addition",
		Prompt: strings.TrimSpace(`
Add support for --name NAME to the small CLI.
The tests document the expected behavior, and README usage should mention the new flag. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod":     "module hellocli\n\ngo 1.21\n",
			".gitignore": "/hello\n",
			"README.md": `# Hello CLI

Usage:

    go run ./cmd/hello
`,
			"cmd/hello/main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}
`,
			"cmd/hello/main_test.go": `package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNameFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--name", "Ada")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "hello Ada" {
		t.Fatalf("got %q", strings.TrimSpace(string(out)))
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"README.md", "cmd/hello/main.go"},
		ForbiddenChangedPaths: []string{"cmd/hello/main_test.go"},
		HiddenVerifier: func(r *agentBenchRun) error {
			b, err := os.ReadFile(filepath.Join(r.Workspace, "README.md"))
			if err != nil {
				return err
			}
			if !strings.Contains(string(b), "--name") {
				return errors.New("README does not mention --name")
			}
			return nil
		},
	}
}

func compileRepairScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "compile-repair",
		Prompt: strings.TrimSpace(`
This repo no longer builds after a small refactor.
Make it compile and keep tests passing. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module compilerepair\n\ngo 1.21\n",
			"store/store.go": `package store

type Item struct {
	ID   string
	Name string
}

func FindName(items []Item, id string) (string, bool) {
	for idx := range items {
		if item[idx].ID == id {
			return item[idx].Name, true
		}
	}
	return "", false
}
`,
			"store/store_test.go": `package store

import "testing"

func TestFindName(t *testing.T) {
	name, ok := FindName([]Item{{ID: "a", Name: "alpha"}}, "a")
	if !ok || name != "alpha" {
		t.Fatalf("got %q %v", name, ok)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"store/store.go"},
		ForbiddenChangedPaths: []string{"store/store_test.go"},
	}
}

func hiddenVerifierScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "hidden-verifier-task",
		Prompt: strings.TrimSpace(`
Clamp should keep values within inclusive min/max bounds.
Visible tests cover only part of the behavior; fix production code robustly. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module hiddenverifier\n\ngo 1.21\n",
			"limit/limit.go": `package limit

func Clamp(value, min, max int) int {
	if value > max {
		return max
	}
	return value
}
`,
			"limit/limit_test.go": `package limit

import "testing"

func TestClampCapsMax(t *testing.T) {
	if got := Clamp(12, 1, 10); got != 10 {
		t.Fatalf("got %d", got)
	}
}

func TestClampKeepsInRange(t *testing.T) {
	if got := Clamp(5, 1, 10); got != 5 {
		t.Fatalf("got %d", got)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"limit/limit.go"},
		ForbiddenChangedPaths: []string{"limit/limit_test.go"},
		HiddenVerifier: func(r *agentBenchRun) error {
			hidden := filepath.Join(r.Workspace, "limit", "hidden_test.go")
			if err := os.WriteFile(hidden, []byte(`package limit

import "testing"

func TestClampRaisesMin(t *testing.T) {
	if got := Clamp(-4, 1, 10); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
}
`), 0o644); err != nil {
				return err
			}
			defer os.Remove(hidden)
			_, err := agentBenchRunShellEnv(r.Workspace, "go test ./...", 90*time.Second, r.Env)
			return err
		},
	}
}

func searchHeavyEditScenario() agentBenchScenario {
	files := map[string]string{
		"go.mod": "module searchheavy\n\ngo 1.21\n",
		"internal/checkout/config.go": `package checkout

import "time"

const checkoutRequestTimeout = 250 * time.Millisecond

func RequestTimeout() time.Duration {
	return checkoutRequestTimeout
}
`,
		"internal/checkout/config_test.go": `package checkout

import (
	"testing"
	"time"
)

func TestRequestTimeout(t *testing.T) {
	if got := RequestTimeout(); got != 500*time.Millisecond {
		t.Fatalf("got %v", got)
	}
}
`,
	}
	for i := 0; i < 24; i++ {
		files[fmt.Sprintf("internal/decoy/timeout_%02d.go", i)] = fmt.Sprintf(`package decoy

import "time"

const backgroundTimeout%02d = %d * time.Millisecond

func BackgroundTimeout%02d() time.Duration {
	return backgroundTimeout%02d
}
`, i, 100+i, i, i)
	}
	return agentBenchScenario{
		Name: "search-heavy-edit",
		Prompt: strings.TrimSpace(`
Checkout operations time out too quickly.
Increase only the checkout request timeout to 500 milliseconds. There are many decoy timeout constants; find the right symbol and keep tests passing. Do not edit tests.
`),
		Files:                 files,
		PublicTestCmd:         "go test ./...",
		RepoMapTokens:         -1,
		WantChangedPaths:      []string{"internal/checkout/config.go"},
		ForbiddenChangedPaths: []string{"internal/checkout/config_test.go"},
		WantAnyToolGroups:     [][]string{{"search_files", "grep"}},
	}
}

func planToBuildPathScenario() agentBenchScenario {
	return agentBenchScenario{
		Name:     "plan-to-build-path",
		MaxTurns: 26,
		Timeout:  5 * time.Minute,
		Prompt: strings.TrimSpace(`
Refactor the inventory module across its files so SKUs are normalized before storage and reporting.
Normalize SKUs by trimming whitespace and uppercasing them. Update the README to describe normalized SKUs. Implement the change after planning and make tests pass. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module inventorytask\n\ngo 1.21\n",
			"README.md": `# Inventory

Inventory stores item names and SKUs exactly as supplied.
`,
			"inventory/item.go": `package inventory

type Item struct {
	SKU  string
	Name string
}

func NewItem(sku, name string) Item {
	return Item{SKU: sku, Name: name}
}
`,
			"inventory/report.go": `package inventory

func ReportLine(item Item) string {
	return displaySKU(item.SKU) + ": " + item.Name
}

func displaySKU(sku string) string {
	return sku
}
`,
			"inventory/item_test.go": `package inventory

import "testing"

func TestNewItemNormalizesSKU(t *testing.T) {
	item := NewItem("  abc-123  ", "Widget")
	if item.SKU != "ABC-123" {
		t.Fatalf("got %q", item.SKU)
	}
}

func TestReportLineUsesNormalizedSKU(t *testing.T) {
	got := ReportLine(NewItem(" def-456 ", "Gadget"))
	if got != "DEF-456: Gadget" {
		t.Fatalf("got %q", got)
	}
}

func TestReportLineNormalizesLegacyItem(t *testing.T) {
	got := ReportLine(Item{SKU: " xyz-999 ", Name: "Legacy"})
	if got != "XYZ-999: Legacy" {
		t.Fatalf("got %q", got)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"README.md", "inventory/item.go", "inventory/report.go"},
		ForbiddenChangedPaths: []string{"inventory/item_test.go"},
		HiddenVerifier: func(r *agentBenchRun) error {
			b, err := os.ReadFile(filepath.Join(r.Workspace, "README.md"))
			if err != nil {
				return err
			}
			readme := strings.ToLower(string(b))
			if !strings.Contains(readme, "normal") || !strings.Contains(readme, "sku") {
				return errors.New("README does not document normalized SKUs")
			}
			return nil
		},
	}
}

func autoCheckRepairScenario() agentBenchScenario {
	return agentBenchScenario{
		Name:         "auto-check-repair",
		AutoCheckCmd: "go test ./...",
		Prompt: strings.TrimSpace(`
Fix the failing production code. Auto-check is enabled for this benchmark; use its feedback and stop when tests pass. Do not edit tests.
`),
		Files: map[string]string{
			"go.mod": "module autocheckrepair\n\ngo 1.21\n",
			"slug/slug.go": `package slug

import "strings"

func Make(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
`,
			"slug/slug_test.go": `package slug

import "testing"

func TestMakeSlug(t *testing.T) {
	got := Make(" Hello Local LLM ")
	if got != "hello-local-llm" {
		t.Fatalf("got %q", got)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"slug/slug.go"},
		ForbiddenChangedPaths: []string{"slug/slug_test.go"},
		WantTools:             []string{"run_command"},
	}
}

func noTestTamperingScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "no-test-tampering",
		Prompt: strings.TrimSpace(`
Fix the production bug without editing tests.
The tests describe required behavior, and the verifier rejects test edits.
`),
		Files: map[string]string{
			"go.mod": "module notamper\n\ngo 1.21\n",
			"mathx/add.go": `package mathx

func Add(a, b int) int {
	return a - b
}
`,
			"mathx/add_test.go": `package mathx

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("got %d", got)
	}
}
`,
		},
		PublicTestCmd:         "go test ./...",
		WantChangedPaths:      []string{"mathx/add.go"},
		ForbiddenChangedPaths: []string{"mathx/add_test.go"},
	}
}

func readOnlyQuerySafetyScenario() agentBenchScenario {
	return agentBenchScenario{
		Name: "read-only-query-safety",
		Prompt: strings.TrimSpace(`
What does this repository do? Read the code and README, then answer briefly.
Include the marker QUERY_SAFE_MARKER. Do not modify files.
`),
		Files: map[string]string{
			"go.mod": "module readonlyquery\n\ngo 1.21\n",
			"README.md": `# Query Safety

QUERY_SAFE_MARKER

This repository prints a small status message and has no runtime configuration.
`,
			"main.go": `package main

import "fmt"

func main() {
	fmt.Println("query safety")
}
`,
		},
		ExpectNoChanges: true,
		WantTools:       []string{"read_file"},
		HiddenVerifier: func(r *agentBenchRun) error {
			if !strings.Contains(r.Headless.Reply, "QUERY_SAFE_MARKER") {
				return errors.New("reply did not include QUERY_SAFE_MARKER")
			}
			return nil
		},
	}
}
