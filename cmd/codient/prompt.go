package main

import (
	"codient/internal/config"
	"codient/internal/prompt"
	"codient/internal/tools"
)

func fetchOptsFrom(cfg *config.Config) *tools.FetchOptions {
	if len(cfg.FetchAllowHosts) == 0 {
		return nil
	}
	return &tools.FetchOptions{
		AllowHosts: cfg.FetchAllowHosts,
		MaxBytes:   cfg.FetchMaxBytes,
		TimeoutSec: cfg.FetchTimeoutSec,
	}
}

func searchOptsFrom(cfg *config.Config) *tools.SearchOptions {
	if cfg.SearchBaseURL == "" {
		return nil
	}
	return &tools.SearchOptions{
		BaseURL:    cfg.SearchBaseURL,
		MaxResults: cfg.SearchMaxResults,
		TimeoutSec: 30,
	}
}

func buildRegistry(cfg *config.Config, mode prompt.Mode, s *session) *tools.Registry {
	fetch := fetchOptsFrom(cfg)
	search := searchOptsFrom(cfg)
	if mode == prompt.ModeAsk {
		return tools.DefaultReadOnly(cfg.EffectiveWorkspace(), fetch, search)
	}
	if mode == prompt.ModePlan {
		return tools.DefaultReadOnlyPlan(cfg.EffectiveWorkspace(), fetch, search)
	}
	var execOpts *tools.ExecOptions
	if len(cfg.ExecAllowlist) > 0 {
		execOpts = &tools.ExecOptions{
			TimeoutSeconds: cfg.ExecTimeoutSeconds,
			MaxOutputBytes: cfg.ExecMaxOutputBytes,
		}
		if s != nil {
			execOpts.ProgressWriter = s.progressOut
		}
		if s != nil && s.execAllow != nil {
			execOpts.Session = s.execAllow
			if s.scanner != nil {
				execOpts.PromptOnDenied = s.execPromptDenied
			}
		} else {
			execOpts.Allowlist = cfg.ExecAllowlist
		}
	}
	return tools.Default(cfg.EffectiveWorkspace(), execOpts, fetch, search)
}

// buildAgentSystemPrompt assembles the layered agent system message (tools, repo notes, -system).
func buildAgentSystemPrompt(cfg *config.Config, reg *tools.Registry, mode prompt.Mode, userSystem, repoInstructions, projectContext, autoCheckResolved string) string {
	return prompt.Build(prompt.Params{
		Cfg:               cfg,
		Reg:               reg,
		Mode:              mode,
		UserSystem:        userSystem,
		RepoInstructions:  repoInstructions,
		ProjectContext:    projectContext,
		AutoCheckResolved: autoCheckResolved,
	})
}
