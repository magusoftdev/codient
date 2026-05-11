package codientcli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"codient/internal/config"
	"codient/internal/openaiclient"
)

// handleProfile implements the /profile slash command.
func (s *session) handleProfile(ctx context.Context, args string) error {
	parts := strings.Fields(strings.TrimSpace(args))
	sub := ""
	if len(parts) > 0 {
		sub = strings.ToLower(parts[0])
	}

	switch sub {
	case "", "list":
		return s.profileList()
	case "show":
		name := ""
		if len(parts) > 1 {
			name = parts[1]
		}
		return s.profileShow(name)
	case "diff":
		if len(parts) < 2 {
			return fmt.Errorf("usage: /profile diff <name>")
		}
		return s.profileDiff(parts[1])
	case "save":
		if len(parts) < 2 {
			return fmt.Errorf("usage: /profile save <name> [--force]")
		}
		force := len(parts) > 2 && strings.EqualFold(parts[2], "--force")
		return s.profileSave(parts[1], force)
	case "delete":
		if len(parts) < 2 {
			return fmt.Errorf("usage: /profile delete <name> [--force]")
		}
		force := len(parts) > 2 && strings.EqualFold(parts[2], "--force")
		return s.profileDelete(parts[1], force)
	case "default":
		return s.profileSwap(ctx, "")
	default:
		if !config.ProfileNameRe.MatchString(sub) {
			return fmt.Errorf("unknown profile subcommand %q (use list, show, diff, save, delete, default, or a profile name)", sub)
		}
		return s.profileSwap(ctx, sub)
	}
}

func (s *session) profileList() error {
	names := config.ProfileNamesList(s.cfg.Profiles)
	w := os.Stderr
	active := s.cfg.ActiveProfile
	if active == "" {
		fmt.Fprintf(w, "  Active profile: (default — top-level config)\n")
	} else {
		fmt.Fprintf(w, "  Active profile: %s\n", active)
	}
	if len(names) == 0 {
		fmt.Fprintf(w, "  No profiles configured.\n")
		return nil
	}
	fmt.Fprintf(w, "  Available profiles:\n")
	for _, n := range names {
		marker := "  "
		if n == active {
			marker = "* "
		}
		model := resolvedProfileModel(s.cfg, n)
		if model != "" {
			fmt.Fprintf(w, "    %s%s  (model: %s)\n", marker, n, model)
		} else {
			fmt.Fprintf(w, "    %s%s\n", marker, n)
		}
	}
	return nil
}

func resolvedProfileModel(cfg *config.Config, name string) string {
	p, ok := cfg.Profiles[name]
	if !ok {
		return ""
	}
	if p.Model != nil {
		return *p.Model
	}
	return cfg.Model
}

func (s *session) profileShow(name string) error {
	if name == "" {
		name = s.cfg.ActiveProfile
	}
	if name == "" {
		fmt.Fprintf(os.Stderr, "  Showing effective config (no profile active):\n\n")
		s.printAllConfig()
		return nil
	}
	p, ok := s.cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	w := os.Stderr
	fmt.Fprintf(w, "  Profile %q overrides:\n", name)
	printProfileOverrides(w, &p)
	return nil
}

func printProfileOverrides(w *os.File, p *config.ProfileOverride) {
	if p.BaseURL != nil {
		fmt.Fprintf(w, "    base_url:              %s\n", *p.BaseURL)
	}
	if p.APIKey != nil {
		masked := *p.APIKey
		if len(masked) > 4 {
			masked = masked[:4] + strings.Repeat("*", len(masked)-4)
		}
		fmt.Fprintf(w, "    api_key:               %s\n", masked)
	}
	if p.Model != nil {
		fmt.Fprintf(w, "    model:                 %s\n", *p.Model)
	}
	if p.LowReasoningModel != nil {
		fmt.Fprintf(w, "    low_reasoning_model:   %s\n", *p.LowReasoningModel)
	}
	if p.LowReasoningBaseURL != nil {
		fmt.Fprintf(w, "    low_reasoning_base_url: %s\n", *p.LowReasoningBaseURL)
	}
	if p.HighReasoningModel != nil {
		fmt.Fprintf(w, "    high_reasoning_model:  %s\n", *p.HighReasoningModel)
	}
	if p.HighReasoningBaseURL != nil {
		fmt.Fprintf(w, "    high_reasoning_base_url: %s\n", *p.HighReasoningBaseURL)
	}
	if p.EmbeddingModel != nil {
		fmt.Fprintf(w, "    embedding_model:       %s\n", *p.EmbeddingModel)
	}
	if p.AutoCheckCmd != nil {
		fmt.Fprintf(w, "    autocheck_cmd:         %s\n", *p.AutoCheckCmd)
	}
	if p.LintCmd != nil {
		fmt.Fprintf(w, "    lint_cmd:              %s\n", *p.LintCmd)
	}
	if p.TestCmd != nil {
		fmt.Fprintf(w, "    test_cmd:              %s\n", *p.TestCmd)
	}
	if p.SandboxMode != nil {
		fmt.Fprintf(w, "    sandbox_mode:          %s\n", *p.SandboxMode)
	}
	if p.ExecAllowlist != nil {
		fmt.Fprintf(w, "    exec_allowlist:        %s\n", *p.ExecAllowlist)
	}
	if p.ExecTimeoutSec != nil {
		fmt.Fprintf(w, "    exec_timeout_sec:      %d\n", *p.ExecTimeoutSec)
	}
	if p.GitAutoCommit != nil {
		fmt.Fprintf(w, "    git_auto_commit:       %v\n", *p.GitAutoCommit)
	}
	if p.PlanTot != nil {
		fmt.Fprintf(w, "    plan_tot:              %v\n", *p.PlanTot)
	}
	if p.ContextWindow != nil {
		fmt.Fprintf(w, "    context_window:        %d\n", *p.ContextWindow)
	}
	if p.Plain != nil {
		fmt.Fprintf(w, "    plain:                 %v\n", *p.Plain)
	}
	if p.Verbose != nil {
		fmt.Fprintf(w, "    verbose:               %v\n", *p.Verbose)
	}
	if p.StreamReply != nil {
		fmt.Fprintf(w, "    stream_reply:          %v\n", *p.StreamReply)
	}
}

func (s *session) profileDiff(name string) error {
	p, ok := s.cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	w := os.Stderr
	fmt.Fprintf(w, "  Keys that would change by switching to %q:\n", name)
	changes := diffProfileOverrides(s.cfg, &p)
	if len(changes) == 0 {
		fmt.Fprintf(w, "    (no changes)\n")
		return nil
	}
	for _, c := range changes {
		fmt.Fprintf(w, "    %s: %s -> %s\n", c.key, c.current, c.override)
	}
	return nil
}

type profileDiffEntry struct {
	key      string
	current  string
	override string
}

func diffProfileOverrides(cfg *config.Config, p *config.ProfileOverride) []profileDiffEntry {
	var out []profileDiffEntry
	add := func(key, cur, ov string) {
		if cur != ov {
			out = append(out, profileDiffEntry{key, cur, ov})
		}
	}
	addBool := func(key string, cur bool, ov *bool) {
		if ov != nil && *ov != cur {
			out = append(out, profileDiffEntry{key, fmt.Sprintf("%v", cur), fmt.Sprintf("%v", *ov)})
		}
	}
	addInt := func(key string, cur int, ov *int) {
		if ov != nil && *ov != cur {
			out = append(out, profileDiffEntry{key, fmt.Sprintf("%d", cur), fmt.Sprintf("%d", *ov)})
		}
	}
	if p.BaseURL != nil {
		add("base_url", cfg.BaseURL, *p.BaseURL)
	}
	if p.Model != nil {
		add("model", cfg.Model, *p.Model)
	}
	if p.LowReasoningModel != nil {
		add("low_reasoning_model", cfg.LowReasoning.Model, *p.LowReasoningModel)
	}
	if p.HighReasoningModel != nil {
		add("high_reasoning_model", cfg.HighReasoning.Model, *p.HighReasoningModel)
	}
	if p.EmbeddingModel != nil {
		add("embedding_model", cfg.EmbeddingModel, *p.EmbeddingModel)
	}
	if p.AutoCheckCmd != nil {
		add("autocheck_cmd", cfg.AutoCheckCmd, *p.AutoCheckCmd)
	}
	if p.LintCmd != nil {
		add("lint_cmd", cfg.LintCmd, *p.LintCmd)
	}
	if p.TestCmd != nil {
		add("test_cmd", cfg.TestCmd, *p.TestCmd)
	}
	if p.SandboxMode != nil {
		add("sandbox_mode", cfg.SandboxMode, *p.SandboxMode)
	}
	if p.ExecAllowlist != nil {
		add("exec_allowlist", strings.Join(cfg.ExecAllowlist, ","), *p.ExecAllowlist)
	}
	addBool("git_auto_commit", cfg.GitAutoCommit, p.GitAutoCommit)
	addBool("plan_tot", cfg.PlanTot, p.PlanTot)
	addInt("context_window", cfg.ContextWindowTokens, p.ContextWindow)
	addBool("plain", cfg.Plain, p.Plain)
	addBool("verbose", cfg.Verbose, p.Verbose)

	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// profileSwap switches the active profile mid-session, rebuilding all subsystems once.
func (s *session) profileSwap(ctx context.Context, name string) error {
	prevWS := s.cfg.Workspace

	selected, err := config.ApplyProfileToConfig(s.cfg, name)
	if err != nil {
		return err
	}
	s.cfg.Workspace = prevWS

	// Persist only the active_profile change — reload the original
	// PersistentConfig so we don't overwrite top-level keys with
	// merged (profile-applied) values.
	pc, err := config.LoadPersistentConfig()
	if err != nil {
		return fmt.Errorf("reload config for save: %w", err)
	}
	pc.ActiveProfile = selected
	if err := config.SavePersistentConfig(pc); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.cfg.ActiveProfile = selected

	// Rebuild all subsystems once.
	s.client = openaiclient.NewForTier(s.cfg, config.TierLow)
	s.installRegistry(buildRegistry(s.cfg, s.mode, s, s.memOpts))
	s.rebuildSystemPrompt()
	s.startCodeIndex(ctx)
	s.cfg.ContextWindowTokens = 0
	s.probeAndSetContext(ctx)
	s.sendTUIChrome()

	if selected == "" {
		fmt.Fprintf(os.Stderr, "codient: switched to default config (no profile)\n")
	} else {
		fmt.Fprintf(os.Stderr, "codient: switched to profile %q\n", selected)
	}
	return nil
}

// profileSave snapshots the current cfg (minus inherited defaults) into the
// named profile and persists to disk.
func (s *session) profileSave(name string, force bool) error {
	if !config.ProfileNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q (use [a-z0-9_-])", name)
	}
	pc, err := config.LoadPersistentConfig()
	if err != nil {
		return err
	}
	if _, exists := pc.Profiles[name]; exists && !force {
		return fmt.Errorf("profile %q already exists (use /profile save %s --force to overwrite)", name, name)
	}

	delta := buildProfileDelta(s.cfg)
	if pc.Profiles == nil {
		pc.Profiles = make(map[string]config.ProfileOverride)
	}
	pc.Profiles[name] = delta
	pc.ActiveProfile = name
	if err := config.SavePersistentConfig(pc); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.cfg.ActiveProfile = name
	s.cfg.Profiles = pc.Profiles
	fmt.Fprintf(os.Stderr, "codient: saved profile %q and set as active\n", name)
	return nil
}

// buildProfileDelta computes a sparse ProfileOverride capturing only the
// keys that differ from built-in defaults.
func buildProfileDelta(cfg *config.Config) config.ProfileOverride {
	var p config.ProfileOverride

	setStr := func(dst **string, val, def string) {
		if val != def {
			v := val
			*dst = &v
		}
	}
	setBool := func(dst **bool, val, def bool) {
		if val != def {
			v := val
			*dst = &v
		}
	}
	setInt := func(dst **int, val, def int) {
		if val != def {
			v := val
			*dst = &v
		}
	}

	setStr(&p.BaseURL, cfg.BaseURL, "http://127.0.0.1:13305/v1")
	setStr(&p.APIKey, cfg.APIKey, "codient")
	if cfg.Model != "" {
		p.Model = &cfg.Model
	}
	if cfg.LowReasoning.Model != "" {
		p.LowReasoningModel = &cfg.LowReasoning.Model
	}
	if cfg.LowReasoning.BaseURL != "" {
		p.LowReasoningBaseURL = &cfg.LowReasoning.BaseURL
	}
	if cfg.LowReasoning.APIKey != "" {
		p.LowReasoningAPIKey = &cfg.LowReasoning.APIKey
	}
	if cfg.HighReasoning.Model != "" {
		p.HighReasoningModel = &cfg.HighReasoning.Model
	}
	if cfg.HighReasoning.BaseURL != "" {
		p.HighReasoningBaseURL = &cfg.HighReasoning.BaseURL
	}
	if cfg.HighReasoning.APIKey != "" {
		p.HighReasoningAPIKey = &cfg.HighReasoning.APIKey
	}
	if cfg.EmbeddingModel != "" {
		p.EmbeddingModel = &cfg.EmbeddingModel
	}
	if cfg.EmbeddingBaseURL != "" {
		p.EmbeddingBaseURL = &cfg.EmbeddingBaseURL
	}
	if cfg.EmbeddingAPIKey != "" {
		p.EmbeddingAPIKey = &cfg.EmbeddingAPIKey
	}
	if cfg.AutoCheckCmd != "" {
		p.AutoCheckCmd = &cfg.AutoCheckCmd
	}
	if cfg.LintCmd != "" {
		p.LintCmd = &cfg.LintCmd
	}
	if cfg.TestCmd != "" {
		p.TestCmd = &cfg.TestCmd
	}
	setStr(&p.SandboxMode, cfg.SandboxMode, "off")
	setBool(&p.GitAutoCommit, cfg.GitAutoCommit, true)
	setBool(&p.PlanTot, cfg.PlanTot, true)
	setInt(&p.ContextWindow, cfg.ContextWindowTokens, 0)
	setBool(&p.Plain, cfg.Plain, false)
	setBool(&p.Quiet, cfg.Quiet, false)
	setBool(&p.Verbose, cfg.Verbose, false)
	setBool(&p.StreamReply, cfg.StreamReply, true)
	setBool(&p.Progress, cfg.Progress, false)

	return p
}

// profileDelete removes a named profile from the persistent config.
func (s *session) profileDelete(name string, force bool) error {
	if !config.ProfileNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	pc, err := config.LoadPersistentConfig()
	if err != nil {
		return err
	}
	if _, ok := pc.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	if s.cfg.ActiveProfile == name && !force {
		return fmt.Errorf("profile %q is currently active (use /profile delete %s --force)", name, name)
	}
	delete(pc.Profiles, name)
	if pc.ActiveProfile == name {
		pc.ActiveProfile = ""
	}
	if err := config.SavePersistentConfig(pc); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.cfg.Profiles = pc.Profiles
	if s.cfg.ActiveProfile == name {
		s.cfg.ActiveProfile = ""
	}
	fmt.Fprintf(os.Stderr, "codient: deleted profile %q\n", name)
	return nil
}
