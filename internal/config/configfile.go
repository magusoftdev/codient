package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const currentSchemaVersion = 2

// PersistentConfig holds all user-configurable settings saved to ~/.codient/config.json.
type PersistentConfig struct {
	// Schema version for migration support.
	SchemaVersion int `json:"schema_version,omitempty"`

	// Connection
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`

	// Default mode
	// Mode is read for backwards compatibility only — the runtime mode is
	// always the Intent-Driven Orchestrator. A non-empty/non-"auto" value
	// triggers a one-time deprecation warning at startup.
	Mode string `json:"mode,omitempty"`

	// Workspace
	Workspace string `json:"workspace,omitempty"`

	// Agent limits
	MaxConcurrent int `json:"max_concurrent,omitempty"`

	// Exec
	ExecAllowlist   string `json:"exec_allowlist,omitempty"`
	ExecDisable     bool   `json:"exec_disable,omitempty"`
	ExecTimeoutSec  int    `json:"exec_timeout_sec,omitempty"`
	ExecMaxOutBytes int    `json:"exec_max_output_bytes,omitempty"`
	// ExecEnvPassthrough is a comma-separated list of extra environment variable names to forward to subprocesses (after scrubbing).
	ExecEnvPassthrough string `json:"exec_env_passthrough,omitempty"`

	// Sandbox: off | native | container | auto (default off). Environment scrubbing applies whenever exec runs.
	SandboxMode           string `json:"sandbox_mode,omitempty"`
	SandboxReadOnlyPaths  string `json:"sandbox_ro_paths,omitempty"`
	SandboxContainerImage string `json:"sandbox_container_image,omitempty"`

	// Context
	ContextWindow  int `json:"context_window,omitempty"`
	ContextReserve int `json:"context_reserve,omitempty"`

	// LLM
	MaxLLMRetries   int  `json:"max_llm_retries,omitempty"`
	StreamWithTools bool `json:"stream_with_tools,omitempty"`

	// Fetch
	FetchAllowHosts  string `json:"fetch_allow_hosts,omitempty"`
	FetchPreapproved *bool  `json:"fetch_preapproved,omitempty"`
	FetchMaxBytes    int    `json:"fetch_max_bytes,omitempty"`
	FetchTimeoutSec  int    `json:"fetch_timeout_sec,omitempty"`
	// MaxCompletionSeconds caps each LLM completion request (default 300, max 3600).
	MaxCompletionSeconds int `json:"max_completion_seconds,omitempty"`
	// FetchWebRatePerSec limits combined fetch_url + web_search (0 = off).
	FetchWebRatePerSec int `json:"fetch_web_rate_per_sec,omitempty"`
	FetchWebRateBurst  int `json:"fetch_web_rate_burst,omitempty"`

	// Search
	SearchMaxResults int `json:"search_max_results,omitempty"`

	// Auto
	AutoCompactPct         int    `json:"autocompact_threshold,omitempty"`
	AutoCheckCmd           string `json:"autocheck_cmd,omitempty"`
	LintCmd                string `json:"lint_cmd,omitempty"`
	TestCmd                string `json:"test_cmd,omitempty"`
	AutoCheckFixMaxRetries int    `json:"autocheck_fix_max_retries,omitempty"`
	// AutoCheckFixStopOnNoProgress defaults to true when the fix loop is
	// active. Use a *bool so that an explicit false in JSON is distinguishable
	// from "not set".
	AutoCheckFixStopOnNoProgress *bool `json:"autocheck_fix_stop_on_no_progress,omitempty"`

	// UI/Output
	Plain   bool `json:"plain,omitempty"`
	Quiet   bool `json:"quiet,omitempty"`
	Verbose bool `json:"verbose,omitempty"`

	// MouseEnabled toggles TUI mouse capture (wheel scroll). When false the
	// terminal handles mouse events itself so native click-and-drag text
	// selection works (default true when omitted).
	MouseEnabled *bool `json:"mouse_enabled,omitempty"`

	// Logging
	LogPath string `json:"log,omitempty"`

	// Streaming
	StreamReply *bool `json:"stream_reply,omitempty"`
	Progress    bool  `json:"progress,omitempty"`

	// AcpPreloadModelOnSetModel: when false, ACP session/set_model skips the warmup chat completion (default true when omitted).
	AcpPreloadModelOnSetModel *bool `json:"acp_preload_model_on_set_model,omitempty"`

	// Plan save
	DesignSaveDir string `json:"design_save_dir,omitempty"`
	DesignSave    *bool  `json:"design_save,omitempty"`
	// PlanTot: when false, disables parallel Tree-of-Thoughts plan generation (default true when omitted).
	PlanTot *bool `json:"plan_tot,omitempty"`

	// Project
	ProjectContext string `json:"project_context,omitempty"`

	// ast-grep: "auto" (default), "off", or explicit path to binary
	AstGrep string `json:"ast_grep,omitempty"`

	// Embedding model for semantic code search (e.g. "text-embedding-3-small"). Empty disables.
	EmbeddingModel string `json:"embedding_model,omitempty"`
	// EmbeddingBaseURL routes /v1/embeddings to a different server than chat (e.g. local LM Studio while chat uses Anthropic). Empty inherits base_url.
	EmbeddingBaseURL string `json:"embedding_base_url,omitempty"`
	// EmbeddingAPIKey is the API key for EmbeddingBaseURL. Empty inherits api_key (only used when EmbeddingBaseURL is set).
	EmbeddingAPIKey string `json:"embedding_api_key,omitempty"`

	// RepoMapTokens caps the structural repo map in the system prompt (0 = auto by workspace size, -1 = off).
	RepoMapTokens int `json:"repo_map_tokens,omitempty"`

	// UpdateNotify opt-out: set to false to suppress the interactive update prompt on startup.
	UpdateNotify *bool `json:"update_notify,omitempty"`

	// Models held per-mode connection overrides (build / ask / plan) in older
	// releases. The runtime no longer honors them — every turn picks
	// LowReasoningModel or HighReasoningModel based on the orchestrator's
	// classification — but the field is still parsed so config.Load can emit
	// a one-time deprecation warning when an old config.json arrives.
	Models map[string]ModeConnectionOverride `json:"models,omitempty"`

	// LowReasoningModel selects the model used for the supervisor (intent
	// classification), QUERY answers, and SIMPLE_FIX implementation. Empty
	// inherits Model. Pair with LowReasoningBaseURL / LowReasoningAPIKey for a
	// fully separate inference endpoint.
	LowReasoningModel   string `json:"low_reasoning_model,omitempty"`
	LowReasoningBaseURL string `json:"low_reasoning_base_url,omitempty"`
	LowReasoningAPIKey  string `json:"low_reasoning_api_key,omitempty"`

	// HighReasoningModel selects the model used for DESIGN advice and
	// COMPLEX_TASK plan generation. Empty inherits Model.
	HighReasoningModel   string `json:"high_reasoning_model,omitempty"`
	HighReasoningBaseURL string `json:"high_reasoning_base_url,omitempty"`
	HighReasoningAPIKey  string `json:"high_reasoning_api_key,omitempty"`

	// MCP servers to connect to at session start.
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty"`

	// Git workflow (build mode): comma-separated branch names that trigger lazy auto-branch (default: main,master,develop).
	GitProtectedBranches string `json:"git_protected_branches,omitempty"`
	// GitAutoCommit defaults to true when omitted: auto-commit after each build turn that changes files.
	GitAutoCommit *bool `json:"git_auto_commit,omitempty"`

	// CheckpointAuto: "plan" (default), "all" (after each build turn with file changes), or "off".
	CheckpointAuto string `json:"checkpoint_auto,omitempty"`

	// CostPerMTok overrides built-in pricing for cost estimates (USD per 1M input/output tokens).
	CostPerMTok *CostPerMTok `json:"cost_per_mtok,omitempty"`

	// HooksEnabled opts into ~/.codient/hooks.json and <workspace>/.codient/hooks.json lifecycle hooks.
	HooksEnabled bool `json:"hooks_enabled,omitempty"`

	// DelegateGitWorktrees runs each delegate_task sub-agent in a detached git worktree at HEAD (default false).
	DelegateGitWorktrees bool `json:"delegate_git_worktrees,omitempty"`

	// DelegateSandboxProfiles defines named sandbox profiles for delegate_task.
	DelegateSandboxProfiles map[string]DelegateSandboxProfile `json:"delegate_sandbox_profiles,omitempty"`
	// DelegateSandboxDefault is the profile name applied when delegate_task omits sandbox_profile.
	DelegateSandboxDefault string `json:"delegate_sandbox_default,omitempty"`

	// ActiveProfile selects a named profile from Profiles at startup (lowest precedence after
	// CODIENT_PROFILE env and -profile CLI flag).
	ActiveProfile string `json:"active_profile,omitempty"`
	// Profiles maps names to sparse setting bundles that override top-level keys.
	Profiles map[string]ProfileOverride `json:"profiles,omitempty"`
}

// ProfileOverride holds a sparse set of settings that override top-level
// PersistentConfig values when the profile is active. Every field is a
// pointer (or omitempty) so that nil/zero means "inherit from top-level".
type ProfileOverride struct {
	BaseURL              *string `json:"base_url,omitempty"`
	APIKey               *string `json:"api_key,omitempty"`
	Model                *string `json:"model,omitempty"`
	LowReasoningModel    *string `json:"low_reasoning_model,omitempty"`
	LowReasoningBaseURL  *string `json:"low_reasoning_base_url,omitempty"`
	LowReasoningAPIKey   *string `json:"low_reasoning_api_key,omitempty"`
	HighReasoningModel   *string `json:"high_reasoning_model,omitempty"`
	HighReasoningBaseURL *string `json:"high_reasoning_base_url,omitempty"`
	HighReasoningAPIKey  *string `json:"high_reasoning_api_key,omitempty"`
	EmbeddingModel       *string `json:"embedding_model,omitempty"`
	EmbeddingBaseURL     *string `json:"embedding_base_url,omitempty"`
	EmbeddingAPIKey      *string `json:"embedding_api_key,omitempty"`

	AutoCheckCmd             *string `json:"autocheck_cmd,omitempty"`
	LintCmd                  *string `json:"lint_cmd,omitempty"`
	TestCmd                  *string `json:"test_cmd,omitempty"`
	AutoCheckFixMaxRetries   *int    `json:"autocheck_fix_max_retries,omitempty"`
	AutoCheckFixStopOnNoProg *bool   `json:"autocheck_fix_stop_on_no_progress,omitempty"`

	SandboxMode           *string `json:"sandbox_mode,omitempty"`
	SandboxReadOnlyPaths  *string `json:"sandbox_ro_paths,omitempty"`
	SandboxContainerImage *string `json:"sandbox_container_image,omitempty"`
	ExecAllowlist         *string `json:"exec_allowlist,omitempty"`
	ExecEnvPassthrough    *string `json:"exec_env_passthrough,omitempty"`
	ExecTimeoutSec        *int    `json:"exec_timeout_sec,omitempty"`
	ExecMaxOutBytes       *int    `json:"exec_max_output_bytes,omitempty"`

	MaxConcurrent *int `json:"max_concurrent,omitempty"`

	FetchAllowHosts    *string `json:"fetch_allow_hosts,omitempty"`
	FetchPreapproved   *bool   `json:"fetch_preapproved,omitempty"`
	FetchMaxBytes      *int    `json:"fetch_max_bytes,omitempty"`
	FetchTimeoutSec    *int    `json:"fetch_timeout_sec,omitempty"`
	FetchWebRatePerSec *int    `json:"fetch_web_rate_per_sec,omitempty"`
	FetchWebRateBurst  *int    `json:"fetch_web_rate_burst,omitempty"`
	SearchMaxResults   *int    `json:"search_max_results,omitempty"`

	GitAutoCommit        *bool   `json:"git_auto_commit,omitempty"`
	GitProtectedBranches *string `json:"git_protected_branches,omitempty"`

	PlanTot     *bool        `json:"plan_tot,omitempty"`
	CostPerMTok *CostPerMTok `json:"cost_per_mtok,omitempty"`

	ContextWindow        *int  `json:"context_window,omitempty"`
	ContextReserve       *int  `json:"context_reserve,omitempty"`
	MaxLLMRetries        *int  `json:"max_llm_retries,omitempty"`
	StreamWithTools      *bool `json:"stream_with_tools,omitempty"`
	MaxCompletionSeconds *int  `json:"max_completion_seconds,omitempty"`
	AutoCompactPct       *int  `json:"autocompact_threshold,omitempty"`

	Plain        *bool `json:"plain,omitempty"`
	Quiet        *bool `json:"quiet,omitempty"`
	Verbose      *bool `json:"verbose,omitempty"`
	MouseEnabled *bool `json:"mouse_enabled,omitempty"`
	Progress     *bool `json:"progress,omitempty"`
	StreamReply  *bool `json:"stream_reply,omitempty"`
}

// ModeConnectionOverride holds optional per-mode overrides for base_url, api_key, and model.
// Empty fields inherit from the top-level connection settings.
type ModeConnectionOverride struct {
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
}

// MCPServerConfig describes a single MCP server connection.
// Set Command for stdio transport or URL for Streamable HTTP transport.
type MCPServerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// StateDir returns the codient state directory (~/.codient, or CODIENT_STATE_DIR if set).
func StateDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("CODIENT_STATE_DIR")); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codient"), nil
}

func configFilePath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// migrateConfig migrates a config from an older schema version to the current version.
// Returns an error if the config is from a newer version than this binary supports.
func migrateConfig(pc *PersistentConfig) error {
	if pc.SchemaVersion > currentSchemaVersion {
		return fmt.Errorf("config file is from a newer version of codient (schema version %d); this binary supports up to version %d — please upgrade codient", pc.SchemaVersion, currentSchemaVersion)
	}
	// Version 0 → 1: no structural changes, just add version field.
	if pc.SchemaVersion < 1 {
		pc.SchemaVersion = 1
	}
	// Version 1 → 2: adds profiles and active_profile (additive).
	if pc.SchemaVersion < 2 {
		pc.SchemaVersion = 2
	}
	return nil
}

// LoadPersistentConfig reads ~/.codient/config.json.
// Returns a zero-value struct (not an error) if the file does not exist.
func LoadPersistentConfig() (*PersistentConfig, error) {
	path, err := configFilePath()
	if err != nil {
		return &PersistentConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PersistentConfig{}, nil
		}
		return nil, err
	}
	var pc PersistentConfig
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := migrateConfig(&pc); err != nil {
		return nil, err
	}
	return &pc, nil
}

// SavePersistentConfig writes the config atomically to ~/.codient/config.json.
func SavePersistentConfig(pc *PersistentConfig) error {
	// Always stamp the current schema version.
	pc.SchemaVersion = currentSchemaVersion

	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config state dir: %w", err)
	}
	path, err := configFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ConfigToPersistent builds a PersistentConfig from the runtime Config for saving.
func ConfigToPersistent(cfg *Config) *PersistentConfig {
	pc := &PersistentConfig{
		BaseURL:               cfg.BaseURL,
		APIKey:                cfg.APIKey,
		Model:                 cfg.Model,
		Workspace:             cfg.Workspace,
		MaxConcurrent:         cfg.MaxConcurrent,
		ExecAllowlist:         strings.Join(cfg.ExecAllowlist, ","),
		ExecEnvPassthrough:    strings.Join(cfg.ExecEnvPassthrough, ","),
		ExecTimeoutSec:        cfg.ExecTimeoutSeconds,
		ExecMaxOutBytes:       cfg.ExecMaxOutputBytes,
		SandboxMode:           cfg.SandboxMode,
		SandboxReadOnlyPaths:  strings.Join(cfg.SandboxReadOnlyPaths, ","),
		SandboxContainerImage: cfg.SandboxContainerImage,
		ContextWindow:         cfg.ContextWindowTokens,
		ContextReserve:        cfg.ContextReserveTokens,
		MaxLLMRetries:         cfg.MaxLLMRetries,
		StreamWithTools:       cfg.StreamWithTools,
		FetchAllowHosts:       strings.Join(cfg.FetchAllowHosts, ","),
		FetchMaxBytes:         cfg.FetchMaxBytes,
		FetchTimeoutSec:       cfg.FetchTimeoutSec,
		FetchWebRatePerSec:    cfg.FetchWebRatePerSec,
		FetchWebRateBurst:     cfg.FetchWebRateBurst,
		SearchMaxResults:      cfg.SearchMaxResults,
		AutoCompactPct:        cfg.AutoCompactPct,
		AutoCheckCmd:           cfg.AutoCheckCmd,
		LintCmd:               cfg.LintCmd,
		TestCmd:               cfg.TestCmd,
		AutoCheckFixMaxRetries: cfg.AutoCheckFixMaxRetries,
		Plain:                 cfg.Plain,
		Quiet:                 cfg.Quiet,
		Verbose:               cfg.Verbose,
		LogPath:               cfg.LogPath,
		Progress:              cfg.Progress,
		DesignSaveDir:         cfg.DesignSaveDir,
		ProjectContext:        cfg.ProjectContext,
		AstGrep:               cfg.AstGrep,
		EmbeddingModel:        cfg.EmbeddingModel,
		EmbeddingBaseURL:      cfg.EmbeddingBaseURL,
		EmbeddingAPIKey:       cfg.EmbeddingAPIKey,
		RepoMapTokens:         cfg.RepoMapTokens,
		LowReasoningModel:     cfg.LowReasoning.Model,
		LowReasoningBaseURL:   cfg.LowReasoning.BaseURL,
		LowReasoningAPIKey:    cfg.LowReasoning.APIKey,
		HighReasoningModel:    cfg.HighReasoning.Model,
		HighReasoningBaseURL:  cfg.HighReasoning.BaseURL,
		HighReasoningAPIKey:   cfg.HighReasoning.APIKey,
		MCPServers:            cfg.MCPServers,
		GitProtectedBranches:  strings.Join(cfg.GitProtectedBranches, ","),
	}
	if !cfg.GitAutoCommit {
		f := false
		pc.GitAutoCommit = &f
	}
	if !cfg.FetchPreapproved {
		f := false
		pc.FetchPreapproved = &f
	}
	if !cfg.StreamReply {
		f := false
		pc.StreamReply = &f
	}
	if !cfg.DesignSave {
		f := false
		pc.DesignSave = &f
	}
	if !cfg.UpdateNotify {
		f := false
		pc.UpdateNotify = &f
	}
	if !cfg.AcpPreloadModelOnSetModel {
		f := false
		pc.AcpPreloadModelOnSetModel = &f
	}
	if !cfg.PlanTot {
		f := false
		pc.PlanTot = &f
	}
	if !cfg.MouseEnabled {
		f := false
		pc.MouseEnabled = &f
	}
	if !cfg.AutoCheckFixStopOnNoProgress {
		f := false
		pc.AutoCheckFixStopOnNoProgress = &f
	}
	pc.CostPerMTok = cfg.CostPerMTok
	pc.HooksEnabled = cfg.HooksEnabled
	pc.DelegateGitWorktrees = cfg.DelegateGitWorktrees
	pc.CheckpointAuto = cfg.CheckpointAuto
	pc.DelegateSandboxProfiles = cfg.DelegateSandboxProfiles
	pc.DelegateSandboxDefault = cfg.DelegateSandboxDefault
	pc.ActiveProfile = cfg.ActiveProfile
	pc.Profiles = cfg.Profiles
	return pc
}

// AppendPersistentFetchHost adds host to fetch_allow_hosts in ~/.codient/config.json if not already present.
func AppendPersistentFetchHost(host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	pc, err := LoadPersistentConfig()
	if err != nil {
		return err
	}
	cur := parseFetchAllowHosts(pc.FetchAllowHosts)
	for _, h := range cur {
		if h == host {
			return nil
		}
	}
	cur = append(cur, host)
	pc.FetchAllowHosts = strings.Join(cur, ",")
	return SavePersistentConfig(pc)
}
