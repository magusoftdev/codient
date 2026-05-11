// Package config loads settings from a persistent config file (~/.codient/config.json).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"codient/internal/sandbox"
)

const (
	defaultBaseURL              = "http://127.0.0.1:13305/v1"
	defaultAPIKey               = "codient"
	defaultMaxConcurrent        = 3
	defaultExecTimeoutSec       = 120
	defaultExecMaxOutBytes      = 256 * 1024
	maxExecTimeoutSec           = 3600
	maxExecMaxOutputBytes       = 10 * 1024 * 1024
	defaultContextReserve       = 4096
	defaultMaxLLMRetries        = 2
	defaultFetchMaxBytes        = 1024 * 1024
	maxFetchMaxBytes            = 10 * 1024 * 1024
	defaultFetchTimeoutSec      = 30
	maxFetchTimeoutSec          = 300
	defaultAutoCompactPct       = 75
	defaultSearchMaxResults     = 5
	maxSearchMaxResults         = 10
	defaultMaxCompletionSeconds = 300
	maxMaxCompletionSeconds     = 3600
	// MaxFetchWebRatePerSec and MaxFetchWebRateBurst cap persisted network rate limits (fetch_url + web_search).
	MaxFetchWebRatePerSec = 100
	MaxFetchWebRateBurst  = 50
)

// Config holds runtime settings.
type Config struct {
	BaseURL       string
	APIKey        string
	Model         string
	MaxConcurrent int
	// Workspace is the root directory for coding tools (read_file, list_dir, search_files, write_file).
	Workspace string
	// ExecAllowlist is a list of lowercase command names (first argv) permitted for run_command and run_shell.
	// When unset, defaults to go, git, and the platform shell (cmd or sh); set exec_disable to disable.
	ExecAllowlist []string
	// ExecEnvPassthrough lists extra environment variable names forwarded to subprocesses (after secret scrubbing).
	ExecEnvPassthrough []string
	// SandboxMode selects subprocess isolation: off, native, container, auto (see docs/configuration.md).
	SandboxMode string
	// SandboxReadOnlyPaths are extra host paths granted read-only to the native/container sandbox.
	SandboxReadOnlyPaths []string
	// SandboxContainerImage is the OCI image for sandbox_mode container (optional; default alpine).
	SandboxContainerImage string
	// ExecTimeoutSeconds caps each run_command (default 120, max 3600).
	ExecTimeoutSeconds int
	// ExecMaxOutputBytes truncates combined stdout+stderr (default 256KiB, max 10MiB).
	ExecMaxOutputBytes int
	// ContextWindowTokens is the model's context window in tokens (0 = no limit).
	ContextWindowTokens int
	// ContextReserveTokens is headroom reserved for the model's reply (default 4096).
	ContextReserveTokens int
	// MaxLLMRetries is the number of retries for transient LLM errors (default 2).
	MaxLLMRetries int
	// StreamWithTools enables SSE token streaming for chat requests that include tools.
	// Default false: many local OpenAI-compatible servers omit or mishandle tool_calls in streamed responses.
	StreamWithTools bool
	// FetchAllowHosts lists hostnames allowed for fetch_url from ~/.codient/config.json.
	// Subdomains match. Empty base list still allows fetch_url in interactive REPL when the
	// user can approve unknown hosts, and/or via FetchPreapproved.
	FetchAllowHosts []string
	// FetchPreapproved enables the built-in documentation/code-domain host preset (default true).
	FetchPreapproved bool
	// FetchMaxBytes caps fetch_url response bodies (default 1MiB, max 10MiB).
	FetchMaxBytes int
	// FetchTimeoutSec caps each fetch_url request (default 30, max 300).
	FetchTimeoutSec int
	// MaxCompletionSeconds caps each LLM completion request (default 300, max 3600).
	MaxCompletionSeconds int
	// SearchMaxResults caps results per web_search query (default 5, max 10).
	SearchMaxResults int
	// FetchWebRatePerSec limits combined fetch_url and web_search requests (token bucket). 0 = disabled.
	FetchWebRatePerSec int
	// FetchWebRateBurst is the bucket size for FetchWebRatePerSec. If 0 while rate is set, defaults to rate per second.
	FetchWebRateBurst int
	// AutoCompactPct is the context usage percentage (0-100) that triggers automatic
	// compaction (LLM-summarize) between turns. 0 disables. Default 75.
	AutoCompactPct int
	// AutoCheckCmd is the shell command to run after file-editing tools.
	// Empty triggers auto-detection from workspace markers (go.mod, package.json, etc.).
	// Set to "off" to disable.
	AutoCheckCmd string
	// LintCmd is the lint command after file edits (build mode auto-check sequence).
	// Empty triggers auto-detection; "off" disables.
	LintCmd string
	// TestCmd is the test command after file edits (build mode auto-check sequence).
	// Empty triggers auto-detection; "off" disables.
	TestCmd string
	// AutoCheckFixMaxRetries is the maximum number of fix-loop iterations the
	// runner performs after an auto-check failure within a single user turn.
	// 0 (default) keeps today's single-shot behaviour; >0 enables the loop.
	AutoCheckFixMaxRetries int
	// AutoCheckFixStopOnNoProgress, when true, aborts the fix loop early if
	// the failure signature is identical to the previous attempt (no progress).
	// Defaults to true when the fix loop is active (AutoCheckFixMaxRetries>0).
	AutoCheckFixStopOnNoProgress bool

	// LegacyMode preserves a `mode` field from older config.json files so we
	// can warn the user that it is no longer honored. The runtime mode is
	// always ModeAuto (Intent-Driven Orchestrator).
	LegacyMode string
	// Plain disables markdown/ANSI output.
	Plain bool
	// Quiet suppresses the welcome banner.
	Quiet bool
	// Verbose enables extra diagnostics.
	Verbose bool
	// MouseEnabled controls whether the TUI captures mouse events (wheel
	// scrolling). When false, the terminal handles mouse events itself so
	// native click-and-drag text selection works. Defaults to true.
	MouseEnabled bool
	// LogPath is the default JSONL log path (overridden by -log flag).
	LogPath string
	// StreamReply controls assistant token streaming (nil pointer in PersistentConfig = default true).
	StreamReply bool
	// Progress forces progress output on stderr.
	Progress bool
	// AcpPreloadModelOnSetModel runs a minimal chat completion after ACP session/set_model so local servers load the model before the next user message (default true).
	AcpPreloadModelOnSetModel bool
	// DesignSaveDir overrides the directory for saved implementation plans.
	DesignSaveDir string
	// DesignSave controls whether plan-mode plans are saved to disk (default true).
	DesignSave bool
	// PlanTot enables parallel Tree-of-Thoughts plan generation on selected plan-mode turns (default true).
	PlanTot bool
	// ProjectContext opt-out: "off" to disable auto-detected project hints.
	ProjectContext string
	// AstGrep is the resolved ast-grep binary path, empty if unavailable, or "off" to disable.
	AstGrep string
	// EmbeddingModel is the model name for /v1/embeddings (e.g. "text-embedding-3-small"). Empty disables semantic search.
	EmbeddingModel string
	// EmbeddingBaseURL routes /v1/embeddings to a different server than chat (e.g. local
	// embedding model while chat uses a hosted API that does not implement /v1/embeddings).
	// Empty inherits BaseURL.
	EmbeddingBaseURL string
	// EmbeddingAPIKey is the API key for EmbeddingBaseURL. Empty inherits APIKey.
	// Only consulted when EmbeddingBaseURL is non-empty (otherwise the chat APIKey is reused).
	EmbeddingAPIKey string
	// RepoMapTokens caps the structural repo map injected into the system prompt (estimated tokens).
	// 0 selects a budget from workspace size (see repomap.AutoTokens). -1 disables the repo map and the repo_map tool.
	RepoMapTokens int
	// UpdateNotify controls whether the interactive update prompt is shown on REPL startup (default true).
	UpdateNotify bool
	// LowReasoning is the inference connection used for the supervisor (intent
	// classification), QUERY answers, and SIMPLE_FIX / build-mode implementation.
	// Empty fields inherit the top-level connection.
	LowReasoning ReasoningTier
	// HighReasoning is the inference connection used for DESIGN advice and
	// COMPLEX_TASK plan generation. Empty fields inherit the top-level
	// connection (so a user with one model still works without extra config).
	HighReasoning ReasoningTier
	// MCPServers maps server IDs to their connection config. Nil/empty means no MCP servers.
	MCPServers map[string]MCPServerConfig
	// GitProtectedBranches lists short branch names that trigger lazy auto-branch to codient/<slug> in build mode (default main, master, develop).
	GitProtectedBranches []string
	// GitAutoCommit enables auto-commit after each build turn that changes files (default true).
	GitAutoCommit bool
	// CheckpointAuto controls automatic checkpoints: "plan" (default), "all", or "off".
	CheckpointAuto string

	// CostPerMTok, when set, overrides built-in model pricing for session cost estimates (USD per 1M tokens).
	CostPerMTok *CostPerMTok

	// HooksEnabled loads ~/.codient/hooks.json and <workspace>/.codient/hooks.json (default false).
	HooksEnabled bool
	// DelegateGitWorktrees isolates each delegate_task in a git worktree (detached HEAD); default false.
	DelegateGitWorktrees bool
	// DelegateSandboxProfiles maps admin-defined names to sandbox profiles for delegate_task.
	// The model can only pick among these names, never define new ones.
	DelegateSandboxProfiles map[string]DelegateSandboxProfile
	// DelegateSandboxDefault is the profile name applied when delegate_task omits sandbox_profile.
	// Empty means fall back to the global SandboxMode / SandboxContainerImage.
	DelegateSandboxDefault string

	// ActiveProfile is the name of the currently selected named profile (empty = defaults).
	ActiveProfile string
	// Profiles is preserved from PersistentConfig so mid-session swaps can re-merge.
	Profiles map[string]ProfileOverride
}

// DelegateSandboxProfile describes container isolation settings for a delegate_task invocation.
type DelegateSandboxProfile struct {
	Image         string   `json:"image,omitempty"`
	NetworkPolicy string   `json:"network_policy,omitempty"`
	MaxMemoryMB   int      `json:"max_memory_mb,omitempty"`
	MaxCPUPercent int      `json:"max_cpu_percent,omitempty"`
	MaxProcesses  int      `json:"max_processes,omitempty"`
	ReadOnlyPaths []string `json:"read_only_paths,omitempty"`
	EnvPassthrough []string `json:"env_passthrough,omitempty"`
	// LongLived keeps a single container running for the delegate's lifetime
	// instead of spawning one per run_command (avoids losing caches between
	// build and test steps). Default false.
	LongLived bool `json:"long_lived,omitempty"`
}

// CostPerMTok holds optional USD per million input/output tokens for cost display.
type CostPerMTok struct {
	Input  float64 `json:"input,omitempty"`
	Output float64 `json:"output,omitempty"`
}

// ReasoningTier holds an optional connection override for one of the two
// reasoning tiers consulted by the Intent-Driven Orchestrator. Empty fields
// inherit the top-level Config connection.
type ReasoningTier struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Reasoning tier identifiers accepted by ConnectionForTier.
const (
	TierLow  = "low"
	TierHigh = "high"
)

// ConnectionForTier returns the effective base URL, API key, and model for the
// given reasoning tier ("low" or "high"). Empty fields fall back to the
// top-level connection. Unknown tiers fall through to the defaults.
func (c *Config) ConnectionForTier(tier string) (baseURL, apiKey, model string) {
	var ov ReasoningTier
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case TierLow:
		ov = c.LowReasoning
	case TierHigh:
		ov = c.HighReasoning
	}
	baseURL = strings.TrimSpace(ov.BaseURL)
	apiKey = strings.TrimSpace(ov.APIKey)
	model = strings.TrimSpace(ov.Model)
	if baseURL == "" {
		baseURL = c.BaseURL
	}
	if apiKey == "" {
		apiKey = c.APIKey
	}
	if model == "" {
		model = c.Model
	}
	return
}

// ConnectionForMode returns the effective base URL, API key, and model for the
// given internal mode. The orchestrator picks the mode per turn (build / ask /
// plan); this helper maps that choice onto the matching reasoning tier
// (build / ask -> low, plan -> high) and returns the resolved connection.
// Unknown modes fall back to the top-level defaults.
func (c *Config) ConnectionForMode(mode string) (baseURL, apiKey, model string) {
	tier := tierForMode(mode)
	if tier != "" {
		return c.ConnectionForTier(tier)
	}
	return c.BaseURL, c.APIKey, c.Model
}

// tierForMode maps a mode string onto the supervisor / implementation reasoning
// tier. Returns "" for unrecognized modes so ConnectionForMode falls through to
// the top-level defaults.
func tierForMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "build", "ask", "":
		return TierLow
	case "plan", "design":
		return TierHigh
	default:
		return ""
	}
}

// EmbeddingConnection returns the effective base URL and API key for /v1/embeddings.
// EmbeddingBaseURL overrides BaseURL when set; EmbeddingAPIKey overrides APIKey only when
// EmbeddingBaseURL is also set (so a custom key without a custom base URL is ignored — the
// chat key is reused). When neither override is configured the chat connection is used,
// preserving the historical behavior.
func (c *Config) EmbeddingConnection() (baseURL, apiKey string) {
	baseURL = strings.TrimRight(strings.TrimSpace(c.EmbeddingBaseURL), "/")
	if baseURL == "" {
		return c.BaseURL, c.APIKey
	}
	apiKey = strings.TrimSpace(c.EmbeddingAPIKey)
	if apiKey == "" {
		apiKey = c.APIKey
	}
	return
}

// Load reads configuration from the persistent config file.
// All settings come from ~/.codient/config.json with built-in defaults.
// CLI flags override config values (handled by the caller via flag.Visit).
func Load() (*Config, error) {
	return LoadWithProfile("")
}

// LoadWithProfile reads the persistent config and merges the named profile.
// An empty profileOverride defers to CODIENT_PROFILE env, then active_profile
// in config.json. A non-empty profileOverride is treated as an explicit
// selection (errors on unknown names).
func LoadWithProfile(profileOverride string) (*Config, error) {
	pc, err := LoadPersistentConfig()
	if err != nil {
		pc = &PersistentConfig{}
	}

	selectedProfile, merged, err := resolveAndMergeProfile(pc, profileOverride)
	if err != nil {
		return nil, err
	}

	c, err := buildConfigFromPersistent(merged, selectedProfile)
	if err != nil {
		return nil, err
	}

	// buildConfigFromPersistent leaves Workspace empty when pc.Workspace is
	// empty; Load is the only entry that defaults to cwd.
	if strings.TrimSpace(c.Workspace) == "" {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			if abs, absErr := filepath.Abs(wd); absErr == nil {
				c.Workspace = abs
			} else {
				c.Workspace = wd
			}
		}
	}
	return c, nil
}

var (
	deprecatedModeModelsWarnOnce sync.Once
	deprecatedModeWarnOnce       sync.Once
)

// maybeWarnDeprecatedModeModels emits a one-shot stderr warning when an old
// config.json still has a `models` block (per-mode connection overrides).
// The runtime no longer honors per-mode model overrides — every turn picks
// `low_reasoning_model` or `high_reasoning_model` based on the orchestrator's
// classification — but we keep the deprecation note so users notice that
// their override is silently ignored.
func (c *Config) maybeWarnDeprecatedModeModels(pc *PersistentConfig) {
	if pc == nil || len(pc.Models) == 0 {
		return
	}
	deprecatedModeModelsWarnOnce.Do(func() {
		fmt.Fprintln(os.Stderr, "codient: 'models' (per-mode model overrides) is no longer honored; configure 'low_reasoning_model' and 'high_reasoning_model' instead.")
	})
}

func (c *Config) maybeWarnDeprecatedMode() {
	m := strings.ToLower(strings.TrimSpace(c.LegacyMode))
	if m == "" || m == "auto" {
		return
	}
	deprecatedModeWarnOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "codient: 'mode': %q is no longer honored — every session runs the Intent-Driven Orchestrator. Remove the field from config.json to silence this warning.\n", c.LegacyMode)
	})
}

// RequireModel returns an error if no model is configured.
func (c *Config) RequireModel() error {
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("no model configured — use /config model <name> to set one (use -list-models to see available ids)")
	}
	return nil
}

// EffectiveWorkspace returns the resolved workspace directory (defaults to cwd at startup when unset).
func (c *Config) EffectiveWorkspace() string {
	return strings.TrimSpace(c.Workspace)
}

// ValidateSandbox checks SandboxMode and runtime availability (native/container).
func ValidateSandbox(c *Config) error {
	sm := strings.TrimSpace(strings.ToLower(c.SandboxMode))
	if sm == "" {
		c.SandboxMode = "off"
		sm = "off"
	} else {
		c.SandboxMode = sm
	}
	if !sandbox.ModeIsValid(sm) {
		return fmt.Errorf("invalid sandbox_mode %q (use off, native, container, auto)", c.SandboxMode)
	}
	runner := sandbox.SelectRunner(sm, sandbox.SelectOptions{ContainerImage: strings.TrimSpace(c.SandboxContainerImage)})
	if sm == "native" && !runner.Available() {
		return fmt.Errorf("sandbox_mode native is not available on this system (try auto or off)")
	}
	if sm == "container" && !runner.Available() {
		return fmt.Errorf("sandbox_mode container requires docker or podman in PATH")
	}
	return nil
}

// defaultExecAllowlist is used when exec_allowlist is unset and exec_disable is not set,
// so run_command / run_shell are registered without extra configuration.
// It includes the platform shell (cmd on Windows, sh on Unix) so run_shell can run mkdir and other builtins.
func defaultExecAllowlist() []string {
	if runtime.GOOS == "windows" {
		return []string{"go", "git", "cmd"}
	}
	return []string{"go", "git", "sh"}
}

// ParseExecAllowlistString parses a comma-separated exec allowlist string.
// Entries are lowercased; ".exe" is stripped for comparison on any OS.
func ParseExecAllowlistString(s string) []string {
	return parseExecAllowlist(s)
}

// ParseFetchAllowHostsString parses a comma-separated fetch allow hosts string.
func ParseFetchAllowHostsString(s string) []string {
	return parseFetchAllowHosts(s)
}

// ParseExecEnvPassthroughString parses comma-separated environment variable names for subprocess passthrough.
func ParseExecEnvPassthroughString(s string) []string {
	return parseCommaListPreserveCase(s)
}

// ParseSandboxReadOnlyPathsString parses comma-separated paths for sandbox read-only mounts.
func ParseSandboxReadOnlyPathsString(s string) []string {
	return parseSandboxPaths(s)
}

func parseExecAllowlist(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		name := strings.TrimSpace(strings.ToLower(part))
		if name == "" {
			continue
		}
		name = strings.TrimSuffix(name, ".exe")
		name = strings.TrimSuffix(name, ".bat")
		name = strings.TrimSuffix(name, ".cmd")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// ParseGitProtectedBranches parses comma-separated git branch names (lowercased, trimmed).
func ParseGitProtectedBranches(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		b := strings.ToLower(strings.TrimSpace(part))
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

func parseFetchAllowHosts(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		h := strings.ToLower(strings.TrimSpace(part))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func parseCommaListPreserveCase(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// ProfileNameRe validates profile / delegate-profile names: lowercase alphanumeric, hyphens, underscores.
var ProfileNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// parseDelegateSandboxProfiles validates admin-defined delegate sandbox profiles.
func parseDelegateSandboxProfiles(profiles map[string]DelegateSandboxProfile, defaultName string) (map[string]DelegateSandboxProfile, string, error) {
	if len(profiles) == 0 {
		return nil, "", nil
	}
	out := make(map[string]DelegateSandboxProfile, len(profiles))
	for name, p := range profiles {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, "", fmt.Errorf("delegate_sandbox_profiles: profile name must not be empty")
		}
		if !ProfileNameRe.MatchString(name) {
			return nil, "", fmt.Errorf("delegate_sandbox_profiles: profile name %q contains invalid character (use [a-z0-9_-])", name)
		}
		if np := strings.TrimSpace(strings.ToLower(p.NetworkPolicy)); np != "" {
			if !sandbox.NetworkPolicyIsValid(np) {
				return nil, "", fmt.Errorf("delegate_sandbox_profiles[%s]: invalid network_policy %q (use none, bridge, host)", name, p.NetworkPolicy)
			}
			p.NetworkPolicy = np
		}
		if p.MaxMemoryMB < 0 {
			p.MaxMemoryMB = 0
		}
		if p.MaxCPUPercent < 0 || p.MaxCPUPercent > 100 {
			p.MaxCPUPercent = 0
		}
		if p.MaxProcesses < 0 {
			p.MaxProcesses = 0
		}
		out[name] = p
	}
	defaultName = strings.TrimSpace(defaultName)
	if defaultName != "" {
		if _, ok := out[defaultName]; !ok {
			return nil, "", fmt.Errorf("delegate_sandbox_default %q does not match any profile in delegate_sandbox_profiles", defaultName)
		}
	}
	return out, defaultName, nil
}

func parseSandboxPaths(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
