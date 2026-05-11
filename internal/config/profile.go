package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

var missingActiveProfileWarnOnce sync.Once

// resolveAndMergeProfile picks the effective profile name (explicit > env >
// active_profile), validates it, clones pc with the profile's overrides
// applied, and returns (selectedName, mergedPC, error). An empty explicit
// string defers to CODIENT_PROFILE then active_profile. When the source is
// active_profile and the name is missing from Profiles, a warning is printed
// and the top-level config is used unchanged.
func resolveAndMergeProfile(pc *PersistentConfig, explicit string) (string, *PersistentConfig, error) {
	selected := strings.TrimSpace(explicit)
	source := "flag"
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("CODIENT_PROFILE"))
		source = "env"
	}
	if selected == "" {
		selected = strings.TrimSpace(pc.ActiveProfile)
		source = "active_profile"
	}
	if selected == "" {
		return "", pc, nil
	}

	if !ProfileNameRe.MatchString(selected) {
		return "", nil, fmt.Errorf("invalid profile name %q (use [a-z0-9_-])", selected)
	}

	if len(pc.Profiles) == 0 {
		if source == "active_profile" {
			missingActiveProfileWarnOnce.Do(func() {
				fmt.Fprintf(os.Stderr, "codient: active_profile %q not found (no profiles defined); using top-level config\n", selected)
			})
			return "", pc, nil
		}
		return "", nil, fmt.Errorf("profile %q not found (no profiles defined in config.json)", selected)
	}

	prof, ok := pc.Profiles[selected]
	if !ok {
		if source == "active_profile" {
			missingActiveProfileWarnOnce.Do(func() {
				fmt.Fprintf(os.Stderr, "codient: active_profile %q not found in profiles; using top-level config\n", selected)
			})
			return "", pc, nil
		}
		return "", nil, fmt.Errorf("profile %q not found in config.json profiles (available: %s)", selected, ProfileNames(pc))
	}

	merged := mergeProfileIntoPersistent(pc, &prof)
	return selected, merged, nil
}

// mergeProfileIntoPersistent returns a shallow copy of pc with the profile's
// non-nil fields applied on top. The Profiles map itself is preserved.
func mergeProfileIntoPersistent(pc *PersistentConfig, prof *ProfileOverride) *PersistentConfig {
	out := *pc // shallow copy

	if prof.BaseURL != nil {
		out.BaseURL = *prof.BaseURL
	}
	if prof.APIKey != nil {
		out.APIKey = *prof.APIKey
	}
	if prof.Model != nil {
		out.Model = *prof.Model
	}
	if prof.LowReasoningModel != nil {
		out.LowReasoningModel = *prof.LowReasoningModel
	}
	if prof.LowReasoningBaseURL != nil {
		out.LowReasoningBaseURL = *prof.LowReasoningBaseURL
	}
	if prof.LowReasoningAPIKey != nil {
		out.LowReasoningAPIKey = *prof.LowReasoningAPIKey
	}
	if prof.HighReasoningModel != nil {
		out.HighReasoningModel = *prof.HighReasoningModel
	}
	if prof.HighReasoningBaseURL != nil {
		out.HighReasoningBaseURL = *prof.HighReasoningBaseURL
	}
	if prof.HighReasoningAPIKey != nil {
		out.HighReasoningAPIKey = *prof.HighReasoningAPIKey
	}
	if prof.EmbeddingModel != nil {
		out.EmbeddingModel = *prof.EmbeddingModel
	}
	if prof.EmbeddingBaseURL != nil {
		out.EmbeddingBaseURL = *prof.EmbeddingBaseURL
	}
	if prof.EmbeddingAPIKey != nil {
		out.EmbeddingAPIKey = *prof.EmbeddingAPIKey
	}
	if prof.AutoCheckCmd != nil {
		out.AutoCheckCmd = *prof.AutoCheckCmd
	}
	if prof.LintCmd != nil {
		out.LintCmd = *prof.LintCmd
	}
	if prof.TestCmd != nil {
		out.TestCmd = *prof.TestCmd
	}
	if prof.AutoCheckFixMaxRetries != nil {
		out.AutoCheckFixMaxRetries = *prof.AutoCheckFixMaxRetries
	}
	if prof.AutoCheckFixStopOnNoProg != nil {
		out.AutoCheckFixStopOnNoProgress = prof.AutoCheckFixStopOnNoProg
	}
	if prof.SandboxMode != nil {
		out.SandboxMode = *prof.SandboxMode
	}
	if prof.SandboxReadOnlyPaths != nil {
		out.SandboxReadOnlyPaths = *prof.SandboxReadOnlyPaths
	}
	if prof.SandboxContainerImage != nil {
		out.SandboxContainerImage = *prof.SandboxContainerImage
	}
	if prof.ExecAllowlist != nil {
		out.ExecAllowlist = *prof.ExecAllowlist
	}
	if prof.ExecEnvPassthrough != nil {
		out.ExecEnvPassthrough = *prof.ExecEnvPassthrough
	}
	if prof.ExecTimeoutSec != nil {
		out.ExecTimeoutSec = *prof.ExecTimeoutSec
	}
	if prof.ExecMaxOutBytes != nil {
		out.ExecMaxOutBytes = *prof.ExecMaxOutBytes
	}
	if prof.MaxConcurrent != nil {
		out.MaxConcurrent = *prof.MaxConcurrent
	}
	if prof.FetchAllowHosts != nil {
		out.FetchAllowHosts = *prof.FetchAllowHosts
	}
	if prof.FetchPreapproved != nil {
		out.FetchPreapproved = prof.FetchPreapproved
	}
	if prof.FetchMaxBytes != nil {
		out.FetchMaxBytes = *prof.FetchMaxBytes
	}
	if prof.FetchTimeoutSec != nil {
		out.FetchTimeoutSec = *prof.FetchTimeoutSec
	}
	if prof.FetchWebRatePerSec != nil {
		out.FetchWebRatePerSec = *prof.FetchWebRatePerSec
	}
	if prof.FetchWebRateBurst != nil {
		out.FetchWebRateBurst = *prof.FetchWebRateBurst
	}
	if prof.SearchMaxResults != nil {
		out.SearchMaxResults = *prof.SearchMaxResults
	}
	if prof.GitAutoCommit != nil {
		out.GitAutoCommit = prof.GitAutoCommit
	}
	if prof.GitProtectedBranches != nil {
		out.GitProtectedBranches = *prof.GitProtectedBranches
	}
	if prof.PlanTot != nil {
		out.PlanTot = prof.PlanTot
	}
	if prof.CostPerMTok != nil {
		out.CostPerMTok = prof.CostPerMTok
	}
	if prof.ContextWindow != nil {
		out.ContextWindow = *prof.ContextWindow
	}
	if prof.ContextReserve != nil {
		out.ContextReserve = *prof.ContextReserve
	}
	if prof.MaxLLMRetries != nil {
		out.MaxLLMRetries = *prof.MaxLLMRetries
	}
	if prof.StreamWithTools != nil {
		out.StreamWithTools = *prof.StreamWithTools
	}
	if prof.MaxCompletionSeconds != nil {
		out.MaxCompletionSeconds = *prof.MaxCompletionSeconds
	}
	if prof.AutoCompactPct != nil {
		out.AutoCompactPct = *prof.AutoCompactPct
	}
	if prof.Plain != nil {
		out.Plain = *prof.Plain
	}
	if prof.Quiet != nil {
		out.Quiet = *prof.Quiet
	}
	if prof.Verbose != nil {
		out.Verbose = *prof.Verbose
	}
	if prof.MouseEnabled != nil {
		out.MouseEnabled = prof.MouseEnabled
	}
	if prof.Progress != nil {
		out.Progress = *prof.Progress
	}
	if prof.StreamReply != nil {
		out.StreamReply = prof.StreamReply
	}

	return &out
}

// ProfileNames returns a sorted, comma-separated list of profile names.
func ProfileNames(pc *PersistentConfig) string {
	if len(pc.Profiles) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(pc.Profiles))
	for n := range pc.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ProfileNamesList returns sorted profile names from a Profiles map.
func ProfileNamesList(profiles map[string]ProfileOverride) []string {
	if len(profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ApplyProfileToConfig reloads the persistent config from disk, merges the
// named profile, and overwrites cfg's fields with the resolved values. Pass
// an empty name to revert to top-level defaults (ignoring active_profile and
// CODIENT_PROFILE). Returns the effective profile name (may be empty).
func ApplyProfileToConfig(cfg *Config, name string) (string, error) {
	pc, err := LoadPersistentConfig()
	if err != nil {
		return "", fmt.Errorf("reload config: %w", err)
	}

	var selected string
	var merged *PersistentConfig
	if name == "" {
		// Explicit revert to defaults — skip env/active_profile resolution.
		selected = ""
		merged = pc
	} else {
		selected, merged, err = resolveAndMergeProfile(pc, name)
		if err != nil {
			return "", err
		}
	}

	rebuilt, err := buildConfigFromPersistent(merged, selected)
	if err != nil {
		return "", err
	}

	// Preserve per-machine / per-session fields that should not be overwritten by a profile swap.
	rebuilt.Workspace = cfg.Workspace

	*cfg = *rebuilt
	return selected, nil
}

// buildConfigFromPersistent is extracted from Load to allow reuse by ApplyProfileToConfig.
func buildConfigFromPersistent(pc *PersistentConfig, selectedProfile string) (*Config, error) {
	baseURL := strings.TrimSpace(pc.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := strings.TrimSpace(pc.APIKey)
	if apiKey == "" {
		apiKey = defaultAPIKey
	}
	model := strings.TrimSpace(pc.Model)

	ws := strings.TrimSpace(pc.Workspace)

	execAllowlist := parseExecAllowlist(pc.ExecAllowlist)
	if pc.ExecDisable {
		execAllowlist = nil
	} else if len(execAllowlist) == 0 {
		execAllowlist = defaultExecAllowlist()
	}

	fetchHosts := parseFetchAllowHosts(pc.FetchAllowHosts)
	fetchPreapproved := true
	if pc.FetchPreapproved != nil {
		fetchPreapproved = *pc.FetchPreapproved
	}

	maxConcurrent := pc.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	execTimeout := pc.ExecTimeoutSec
	if execTimeout == 0 {
		execTimeout = defaultExecTimeoutSec
	}
	execMaxOut := pc.ExecMaxOutBytes
	if execMaxOut == 0 {
		execMaxOut = defaultExecMaxOutBytes
	}
	contextReserve := pc.ContextReserve
	if contextReserve == 0 {
		contextReserve = defaultContextReserve
	}
	maxLLMRetries := pc.MaxLLMRetries
	if maxLLMRetries == 0 {
		maxLLMRetries = defaultMaxLLMRetries
	}
	fetchMax := pc.FetchMaxBytes
	if fetchMax == 0 {
		fetchMax = defaultFetchMaxBytes
	}
	fetchTimeout := pc.FetchTimeoutSec
	if fetchTimeout == 0 {
		fetchTimeout = defaultFetchTimeoutSec
	}
	maxCompletion := pc.MaxCompletionSeconds
	if maxCompletion == 0 {
		maxCompletion = defaultMaxCompletionSeconds
	}
	searchMaxResults := pc.SearchMaxResults
	if searchMaxResults == 0 {
		searchMaxResults = defaultSearchMaxResults
	}
	fetchWebRate := pc.FetchWebRatePerSec
	if fetchWebRate < 0 {
		fetchWebRate = 0
	}
	if fetchWebRate > MaxFetchWebRatePerSec {
		fetchWebRate = MaxFetchWebRatePerSec
	}
	fetchWebBurst := pc.FetchWebRateBurst
	if fetchWebBurst < 0 {
		fetchWebBurst = 0
	}
	if fetchWebRate > 0 && fetchWebBurst == 0 {
		fetchWebBurst = fetchWebRate
	}
	if fetchWebBurst > MaxFetchWebRateBurst {
		fetchWebBurst = MaxFetchWebRateBurst
	}
	autoCompactPct := pc.AutoCompactPct
	if autoCompactPct == 0 {
		autoCompactPct = defaultAutoCompactPct
	}
	streamReply := true
	if pc.StreamReply != nil {
		streamReply = *pc.StreamReply
	}
	designSave := true
	if pc.DesignSave != nil {
		designSave = *pc.DesignSave
	}
	updateNotify := true
	if pc.UpdateNotify != nil {
		updateNotify = *pc.UpdateNotify
	}
	gitAutoCommit := true
	if pc.GitAutoCommit != nil {
		gitAutoCommit = *pc.GitAutoCommit
	}
	acpPreloadModelOnSetModel := true
	if pc.AcpPreloadModelOnSetModel != nil {
		acpPreloadModelOnSetModel = *pc.AcpPreloadModelOnSetModel
	}
	planTot := true
	if pc.PlanTot != nil {
		planTot = *pc.PlanTot
	}
	mouseEnabled := true
	if pc.MouseEnabled != nil {
		mouseEnabled = *pc.MouseEnabled
	}
	protectedBranches := ParseGitProtectedBranches(pc.GitProtectedBranches)
	if len(protectedBranches) == 0 {
		protectedBranches = []string{"main", "master", "develop"}
	}
	checkpointAuto := strings.TrimSpace(strings.ToLower(pc.CheckpointAuto))
	if checkpointAuto == "" {
		checkpointAuto = "plan"
	}
	if checkpointAuto != "plan" && checkpointAuto != "all" && checkpointAuto != "off" {
		checkpointAuto = "plan"
	}
	execEnvPassthrough := parseCommaListPreserveCase(pc.ExecEnvPassthrough)
	sandboxRO := parseSandboxPaths(pc.SandboxReadOnlyPaths)
	sandboxMode := strings.TrimSpace(strings.ToLower(pc.SandboxMode))
	if sandboxMode == "" {
		sandboxMode = "off"
	}
	sandboxImg := strings.TrimSpace(pc.SandboxContainerImage)

	delegateProfiles, delegateDefault, delegateErr := parseDelegateSandboxProfiles(pc.DelegateSandboxProfiles, pc.DelegateSandboxDefault)
	if delegateErr != nil {
		return nil, delegateErr
	}

	c := &Config{
		BaseURL:                   baseURL,
		APIKey:                    apiKey,
		Model:                     model,
		MaxConcurrent:             maxConcurrent,
		Workspace:                 ws,
		HooksEnabled:              pc.HooksEnabled,
		DelegateGitWorktrees:      pc.DelegateGitWorktrees,
		DelegateSandboxProfiles:   delegateProfiles,
		DelegateSandboxDefault:    delegateDefault,
		ExecAllowlist:             execAllowlist,
		ExecEnvPassthrough:        execEnvPassthrough,
		ExecTimeoutSeconds:        execTimeout,
		ExecMaxOutputBytes:        execMaxOut,
		SandboxMode:               sandboxMode,
		SandboxReadOnlyPaths:      sandboxRO,
		SandboxContainerImage:     sandboxImg,
		ContextWindowTokens:       pc.ContextWindow,
		ContextReserveTokens:      contextReserve,
		MaxLLMRetries:             maxLLMRetries,
		StreamWithTools:           pc.StreamWithTools,
		FetchAllowHosts:           fetchHosts,
		FetchPreapproved:          fetchPreapproved,
		FetchMaxBytes:             fetchMax,
		FetchTimeoutSec:           fetchTimeout,
		MaxCompletionSeconds:      maxCompletion,
		SearchMaxResults:          searchMaxResults,
		FetchWebRatePerSec:        fetchWebRate,
		FetchWebRateBurst:         fetchWebBurst,
		AutoCompactPct:            autoCompactPct,
		AutoCheckCmd:                 strings.TrimSpace(pc.AutoCheckCmd),
		LintCmd:                     strings.TrimSpace(pc.LintCmd),
		TestCmd:                     strings.TrimSpace(pc.TestCmd),
		AutoCheckFixMaxRetries:      pc.AutoCheckFixMaxRetries,
		AutoCheckFixStopOnNoProgress: pc.AutoCheckFixStopOnNoProgress == nil || *pc.AutoCheckFixStopOnNoProgress,
		LegacyMode:                  strings.TrimSpace(pc.Mode),
		Plain:                     pc.Plain,
		Quiet:                     pc.Quiet,
		Verbose:                   pc.Verbose,
		MouseEnabled:              mouseEnabled,
		LogPath:                   strings.TrimSpace(pc.LogPath),
		StreamReply:               streamReply,
		Progress:                  pc.Progress,
		AcpPreloadModelOnSetModel: acpPreloadModelOnSetModel,
		DesignSaveDir:             strings.TrimSpace(pc.DesignSaveDir),
		DesignSave:                designSave,
		PlanTot:                   planTot,
		ProjectContext:            strings.TrimSpace(pc.ProjectContext),
		AstGrep:                   strings.TrimSpace(pc.AstGrep),
		EmbeddingModel:            strings.TrimSpace(pc.EmbeddingModel),
		EmbeddingBaseURL:          strings.TrimRight(strings.TrimSpace(pc.EmbeddingBaseURL), "/"),
		EmbeddingAPIKey:           strings.TrimSpace(pc.EmbeddingAPIKey),
		RepoMapTokens:             pc.RepoMapTokens,
		UpdateNotify:              updateNotify,
		LowReasoning: ReasoningTier{
			BaseURL: strings.TrimSpace(pc.LowReasoningBaseURL),
			APIKey:  strings.TrimSpace(pc.LowReasoningAPIKey),
			Model:   strings.TrimSpace(pc.LowReasoningModel),
		},
		HighReasoning: ReasoningTier{
			BaseURL: strings.TrimSpace(pc.HighReasoningBaseURL),
			APIKey:  strings.TrimSpace(pc.HighReasoningAPIKey),
			Model:   strings.TrimSpace(pc.HighReasoningModel),
		},
		MCPServers:           pc.MCPServers,
		GitProtectedBranches: protectedBranches,
		GitAutoCommit:        gitAutoCommit,
		CheckpointAuto:       checkpointAuto,
		CostPerMTok:          pc.CostPerMTok,
		ActiveProfile:        selectedProfile,
		Profiles:             pc.Profiles,
	}
	if err := ValidateSandbox(c); err != nil {
		return nil, err
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
	if c.MaxConcurrent < 1 {
		return nil, fmt.Errorf("max_concurrent must be at least 1")
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
	if c.MaxCompletionSeconds < 1 {
		c.MaxCompletionSeconds = defaultMaxCompletionSeconds
	}
	if c.MaxCompletionSeconds > maxMaxCompletionSeconds {
		c.MaxCompletionSeconds = maxMaxCompletionSeconds
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
	if c.RepoMapTokens < -1 {
		c.RepoMapTokens = 0
	}
	c.maybeWarnDeprecatedModeModels(pc)
	c.maybeWarnDeprecatedMode()
	return c, nil
}
