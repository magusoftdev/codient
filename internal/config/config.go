// Package config loads settings from a persistent config file and the environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	defaultBaseURL            = "http://127.0.0.1:1234/v1"
	defaultAPIKey             = "codient"
	defaultMaxToolSteps       = 1000
	defaultMaxConcurrent      = 3
	defaultExecTimeoutSec     = 120
	defaultExecMaxOutBytes    = 256 * 1024
	maxExecTimeoutSec         = 3600
	maxExecMaxOutputBytes     = 10 * 1024 * 1024
	defaultContextReserve     = 4096
	defaultMaxLLMRetries        = 2
	defaultFetchMaxBytes        = 1024 * 1024
	maxFetchMaxBytes            = 10 * 1024 * 1024
	defaultFetchTimeoutSec      = 30
	maxFetchTimeoutSec          = 300
	defaultAutoCompactPct       = 75
	defaultSearchMaxResults     = 5
	maxSearchMaxResults         = 10
)

// Config holds runtime settings.
type Config struct {
	BaseURL       string
	APIKey        string
	Model         string
	MaxToolSteps  int
	MaxConcurrent int
	// Workspace is the root directory for coding tools (read_file, list_dir, search_files, write_file).
	Workspace string
	// ExecAllowlist is a list of lowercase command names (first argv) permitted for run_command and run_shell.
	// When CODIENT_EXEC_ALLOWLIST is unset, defaults to go, git, and the platform shell (cmd or sh); set CODIENT_EXEC_DISABLE=1 to disable.
	ExecAllowlist []string
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
	// StreamWithTools enables SSE token streaming for chat requests that include tools (CODIENT_STREAM_WITH_TOOLS=1).
	// Default false: many local OpenAI-compatible servers omit or mishandle tool_calls in streamed responses.
	StreamWithTools bool
	// FetchAllowHosts lists hostnames allowed for fetch_url (comma-separated CODIENT_FETCH_ALLOW_HOSTS).
	// Subdomains match (e.g. api.example.com when example.com is listed). Empty means fetch_url is not registered.
	FetchAllowHosts []string
	// FetchMaxBytes caps fetch_url response bodies (default 1MiB, max 10MiB).
	FetchMaxBytes int
	// FetchTimeoutSec caps each fetch_url request (default 30, max 300).
	FetchTimeoutSec int
	// SearchBaseURL is the SearXNG base URL for the web_search tool (CODIENT_SEARCH_URL, e.g. "http://localhost:8080").
	// Empty means web_search is not registered. Also persisted via /setup in ~/.codient/config.json.
	SearchBaseURL string
	// SearchMaxResults caps results per web_search query (default 5, max 10; CODIENT_SEARCH_MAX_RESULTS).
	SearchMaxResults int
	// AutoCompactPct is the context usage percentage (0-100) that triggers automatic
	// compaction (LLM-summarize) between turns. 0 disables. Default 75.
	// Set via CODIENT_AUTOCOMPACT_THRESHOLD.
	AutoCompactPct int
	// AutoCheckCmd is the shell command to run after file-editing tools (CODIENT_AUTOCHECK_CMD).
	// Empty triggers auto-detection from workspace markers (go.mod, package.json, etc.).
	// Set to "off" to disable.
	AutoCheckCmd string
}

// Load reads configuration from the persistent config file and environment.
// Core connection settings (base URL, API key, model) come from ~/.codient/config.json.
// Operational settings come from environment variables.
func Load() (*Config, error) {
	pc, err := LoadPersistentConfig()
	if err != nil {
		pc = &PersistentConfig{}
	}

	baseURL := strings.TrimSpace(pc.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := strings.TrimSpace(pc.APIKey)
	if apiKey == "" {
		apiKey = defaultAPIKey
	}
	model := strings.TrimSpace(pc.Model)

	ws := strings.TrimSpace(os.Getenv("CODIENT_WORKSPACE"))
	if ws == "" {
		if wd, err := os.Getwd(); err == nil {
			if abs, err := filepath.Abs(wd); err == nil {
				ws = abs
			} else {
				ws = wd
			}
		}
	}

	execAllowlist := parseExecAllowlist(os.Getenv("CODIENT_EXEC_ALLOWLIST"))
	if strings.TrimSpace(os.Getenv("CODIENT_EXEC_DISABLE")) == "1" {
		execAllowlist = nil
	} else if len(execAllowlist) == 0 {
		execAllowlist = defaultExecAllowlist()
	}

	fetchHosts := parseFetchAllowHosts(os.Getenv("CODIENT_FETCH_ALLOW_HOSTS"))
	fetchMax := getenvInt("CODIENT_FETCH_MAX_BYTES", defaultFetchMaxBytes)
	fetchTimeout := getenvInt("CODIENT_FETCH_TIMEOUT_SEC", defaultFetchTimeoutSec)

	searchBaseURL := strings.TrimSpace(os.Getenv("CODIENT_SEARCH_URL"))
	if searchBaseURL == "" {
		searchBaseURL = strings.TrimSpace(pc.SearchBaseURL)
	}
	searchMaxResults := getenvInt("CODIENT_SEARCH_MAX_RESULTS", defaultSearchMaxResults)

	c := &Config{
		BaseURL:            baseURL,
		APIKey:             apiKey,
		Model:              model,
		MaxToolSteps:       getenvInt("AGENT_MAX_TOOL_STEPS", defaultMaxToolSteps),
		MaxConcurrent:      getenvInt("LLM_MAX_CONCURRENT", defaultMaxConcurrent),
		Workspace:          ws,
		ExecAllowlist:      execAllowlist,
		ExecTimeoutSeconds:  getenvInt("CODIENT_EXEC_TIMEOUT_SEC", defaultExecTimeoutSec),
		ExecMaxOutputBytes:  getenvInt("CODIENT_EXEC_MAX_OUTPUT_BYTES", defaultExecMaxOutBytes),
		ContextWindowTokens: getenvInt("CODIENT_CONTEXT_WINDOW", 0),
		ContextReserveTokens: getenvInt("CODIENT_CONTEXT_RESERVE", defaultContextReserve),
		MaxLLMRetries:       getenvInt("CODIENT_LLM_RETRIES", defaultMaxLLMRetries),
		StreamWithTools:     strings.TrimSpace(os.Getenv("CODIENT_STREAM_WITH_TOOLS")) == "1",
		FetchAllowHosts:     fetchHosts,
		FetchMaxBytes:       fetchMax,
		FetchTimeoutSec:     fetchTimeout,
		SearchBaseURL:       searchBaseURL,
		SearchMaxResults:    searchMaxResults,
		AutoCompactPct:      getenvInt("CODIENT_AUTOCOMPACT_THRESHOLD", defaultAutoCompactPct),
		AutoCheckCmd:        strings.TrimSpace(os.Getenv("CODIENT_AUTOCHECK_CMD")),
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.ExecTimeoutSeconds < 1 {
		c.ExecTimeoutSeconds = defaultExecTimeoutSec
	}
	if c.ExecTimeoutSeconds > maxExecTimeoutSec {
		c.ExecTimeoutSeconds = maxExecTimeoutSec
	}
	if c.ExecMaxOutputBytes < 1 {
		c.ExecMaxOutputBytes = defaultExecMaxOutBytes
	}
	if c.ExecMaxOutputBytes > maxExecMaxOutputBytes {
		c.ExecMaxOutputBytes = maxExecMaxOutputBytes
	}
	if c.MaxToolSteps < 1 {
		return nil, fmt.Errorf("AGENT_MAX_TOOL_STEPS must be at least 1")
	}
	if c.MaxConcurrent < 1 {
		return nil, fmt.Errorf("LLM_MAX_CONCURRENT must be at least 1")
	}
	if c.ContextWindowTokens < 0 {
		c.ContextWindowTokens = 0
	}
	if c.ContextReserveTokens < 0 {
		c.ContextReserveTokens = defaultContextReserve
	}
	if c.MaxLLMRetries < 0 {
		c.MaxLLMRetries = 0
	}
	if c.FetchMaxBytes < 1 {
		c.FetchMaxBytes = defaultFetchMaxBytes
	}
	if c.FetchMaxBytes > maxFetchMaxBytes {
		c.FetchMaxBytes = maxFetchMaxBytes
	}
	if c.FetchTimeoutSec < 1 {
		c.FetchTimeoutSec = defaultFetchTimeoutSec
	}
	if c.FetchTimeoutSec > maxFetchTimeoutSec {
		c.FetchTimeoutSec = maxFetchTimeoutSec
	}
	if c.SearchMaxResults < 1 {
		c.SearchMaxResults = defaultSearchMaxResults
	}
	if c.SearchMaxResults > maxSearchMaxResults {
		c.SearchMaxResults = maxSearchMaxResults
	}
	if c.AutoCompactPct < 0 {
		c.AutoCompactPct = 0
	}
	if c.AutoCompactPct > 100 {
		c.AutoCompactPct = 100
	}
	return c, nil
}

// RequireModel returns an error if no model is configured (needed for chat completions).
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

func getenvInt(key string, def int) int {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// defaultExecAllowlist is used when CODIENT_EXEC_ALLOWLIST is unset and CODIENT_EXEC_DISABLE is not set,
// so run_command / run_shell are registered without extra configuration.
// It includes the platform shell (cmd on Windows, sh on Unix) so run_shell can run mkdir and other builtins.
func defaultExecAllowlist() []string {
	if runtime.GOOS == "windows" {
		return []string{"go", "git", "cmd"}
	}
	return []string{"go", "git", "sh"}
}

// parseExecAllowlist parses CODIENT_EXEC_ALLOWLIST (comma-separated names, no paths).
// Entries are lowercased; ".exe" is stripped for comparison on any OS.
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

// parseFetchAllowHosts parses CODIENT_FETCH_ALLOW_HOSTS (comma-separated hostnames, no schemes or paths).
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
