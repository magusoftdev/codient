package codientcli

import (
	"path/filepath"

	"codient/internal/codeindex"
	"codient/internal/config"
	"codient/internal/sandbox"
	"codient/internal/prompt"
	"codient/internal/repomap"
	"codient/internal/skills"
	"codient/internal/tools"
)

func userSkillsReadLib() string {
	sd, err := config.StateDir()
	if err != nil || sd == "" {
		return ""
	}
	return filepath.Join(sd, skills.UserSkillsSubdir)
}

func fetchOptsFrom(cfg *config.Config, s *session, netLimit *tools.RateLimiter) *tools.FetchOptions {
	opts := &tools.FetchOptions{
		AllowHosts:         append([]string(nil), cfg.FetchAllowHosts...),
		MaxBytes:           cfg.FetchMaxBytes,
		TimeoutSec:         cfg.FetchTimeoutSec,
		IncludePreapproved: cfg.FetchPreapproved,
		RateLimiter:        netLimit,
	}
	interactive := s != nil && s.scanner != nil && stdinIsInteractive()
	if interactive {
		if s.fetchAllow == nil {
			s.fetchAllow = tools.NewSessionFetchAllow()
		}
		opts.Session = s.fetchAllow
		opts.PromptUnknownHost = s.fetchPromptUnknownHost
		opts.PersistFetchHost = s.persistFetchHostToConfig
	}
	if len(opts.AllowHosts) == 0 && opts.PromptUnknownHost == nil && !opts.IncludePreapproved {
		return nil
	}
	return opts
}

func searchOptsFrom(cfg *config.Config, netLimit *tools.RateLimiter) *tools.SearchOptions {
	return &tools.SearchOptions{
		MaxResults:  cfg.SearchMaxResults,
		TimeoutSec:  30,
		RateLimiter: netLimit,
	}
}

func buildRegistry(cfg *config.Config, mode prompt.Mode, s *session, memOpts *tools.MemoryOptions) *tools.Registry {
	netLimit := tools.NewNetworkLimiter(cfg.FetchWebRatePerSec, cfg.FetchWebRateBurst)
	fetch := fetchOptsFrom(cfg, s, netLimit)
	search := searchOptsFrom(cfg, netLimit)
	sgPath := cfg.AstGrep
	var idx *codeindex.Index
	var rm *repomap.Map
	if s != nil {
		idx = s.codeIndex
		rm = s.repoMap
	}
	uskill := userSkillsReadLib()
	var reg *tools.Registry
	switch mode {
	case prompt.ModeAsk:
		reg = tools.DefaultReadOnly(cfg.EffectiveWorkspace(), uskill, fetch, search, sgPath, idx, rm)
	case prompt.ModePlan:
		reg = tools.DefaultReadOnlyPlan(cfg.EffectiveWorkspace(), uskill, fetch, search, sgPath, idx, rm)
	default:
		var execOpts *tools.ExecOptions
		if len(cfg.ExecAllowlist) > 0 {
			execOpts = &tools.ExecOptions{
				TimeoutSeconds:       cfg.ExecTimeoutSeconds,
				MaxOutputBytes:       cfg.ExecMaxOutputBytes,
				EnvPassthrough:       append([]string(nil), cfg.ExecEnvPassthrough...),
				SandboxReadOnlyPaths: append([]string(nil), cfg.SandboxReadOnlyPaths...),
				WorkspaceRoot:        cfg.EffectiveWorkspace(),
				SandboxRunner: sandbox.SelectRunner(cfg.SandboxMode, sandbox.SelectOptions{
					ContainerImage: cfg.SandboxContainerImage,
				}),
			}
			if s != nil {
				execOpts.ProgressWriter = s.progressOut
			}
			if s != nil && s.execAllow != nil {
				execOpts.Session = s.execAllow
				switch {
				case s.scanner != nil:
					execOpts.PromptOnDenied = s.execPromptDenied
				case s.execDeniedACP != nil:
					execOpts.PromptOnDenied = s.execDeniedACP
				}
			} else {
				execOpts.Allowlist = cfg.ExecAllowlist
			}
		}
		reg = tools.Default(cfg.EffectiveWorkspace(), uskill, execOpts, fetch, search, sgPath, idx, rm, memOpts)
	}
	if s != nil && mode == prompt.ModeBuild && !s.acpNoDelegate {
		tools.RegisterCreatePullRequest(reg, s.gitPullRequestContextFn())
	}
	if s != nil && s.mcpMgr != nil {
		tools.RegisterMCPTools(reg, s.mcpMgr)
	}
	// Register delegate_task for the interactive parent session only.
	// Sub-agent registries (built via agentfactory) never get this tool.
	if s != nil && !s.acpNoDelegate {
		tools.RegisterDelegateTask(reg, string(mode), s.delegateTaskFn())
	}
	if s != nil && s.acpCallClient != nil {
		registerUnityACPToolsForMode(reg, mode, s.acpCallClient)
	}
	return reg
}

// buildAgentSystemPrompt assembles the layered agent system message (tools, repo notes, -system).
func buildAgentSystemPrompt(cfg *config.Config, reg *tools.Registry, mode prompt.Mode, userSystem, repoInstructions, projectContext, memory, skillsCatalog string, rm *repomap.Map) string {
	return buildAgentSystemPromptEx(cfg, reg, mode, userSystem, repoInstructions, projectContext, memory, skillsCatalog, rm, false)
}

// buildAgentSystemPromptEx is like buildAgentSystemPrompt but can enable Unity ACP editor guidance when unityACPEditor is true.
func buildAgentSystemPromptEx(cfg *config.Config, reg *tools.Registry, mode prompt.Mode, userSystem, repoInstructions, projectContext, memory, skillsCatalog string, rm *repomap.Map, unityACPEditor bool) string {
	repoMapText := repomap.PromptText(cfg.RepoMapTokens, rm)
	return prompt.Build(prompt.Params{
		Cfg:                    cfg,
		Reg:                    reg,
		Mode:                   mode,
		UserSystem:             userSystem,
		RepoInstructions:       repoInstructions,
		ProjectContext:         projectContext,
		RepoMap:                repoMapText,
		Memory:                 memory,
		SkillsCatalog:          skillsCatalog,
		AutoCheckBuildResolved: effectiveAutoCheckCmd(cfg),
		AutoCheckLintResolved:  effectiveLintCmd(cfg),
		AutoCheckTestResolved:  effectiveTestCmd(cfg),
		UnityACPEditor:         unityACPEditor,
	})
}
