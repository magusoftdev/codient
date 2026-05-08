# Feature parity vs. industry coding agents

This document supersedes informal “gap list” audits: it records **what codient implements today** and **what is still open** relative to common expectations (Claude Code, Cursor CLI, Codex CLI, Gemini CLI, Aider, etc.). Update it when parity-relevant behavior or docs change.

**Pointers:** [Configuration](configuration.md) · [Usage](usage.md) · [Context and integrations](context-and-integrations.md)

---

## Strengths (unchanged themes)

- Multi-mode architecture (Build / Ask / Plan) with tool scoping per mode  
- OpenAI-compatible APIs  
- File and search tools (read, write, patch, grep, glob, search, semantic search)  
- Embedded web search (searchmux) where configured  
- A2A server (`-a2a`)  
- Memory (global + workspace)  
- Auto-compact and context window handling  
- Repo instructions (`AGENTS.md`, `.codient/instructions.md`)  
- Plan mode, plan store, approval gates  
- Self-update via GitHub releases  

---

## Former “critical gaps” — status

| Theme | Status | Notes / location |
|--------|--------|-------------------|
| **MCP client** | **Done** | `internal/mcpclient`; config `mcp_servers`; tools `mcp__…`. See [Context and integrations](context-and-integrations.md#mcp-model-context-protocol-servers). |
| **Sub-agents / delegation** | **Partial** | `delegate_task` + `internal/subagent`: nested child runner, same workspace, **sequential**. No parallel agents or **git worktrees** per task. See [Usage](usage.md) (sub-agents). |
| **Sandbox / isolation** | **Done (platform-dependent)** | `sandbox_mode`: `off`, `native`, `container`, `auto`. Native: Linux/Darwin/Windows job limits; **container**: Docker/Podman. See [Configuration](configuration.md) (sandboxing). |
| **Multimodal (images)** | **Done** | CLI `-image`, REPL `/image`, `/paste` (clipboard), Ctrl+V in TUI; use vision-capable models. See [Usage](usage.md). |
| **Headless / CI** | **Partial** | `-print`, `-auto-approve`, JSON / `stream-json` (`session_id`, `workspace`, `cost_usd`). Sessions persist under **`.codient/sessions/`**; resume latest or **`-session-id`** for chained runs. **No first-party cloud** — see [Bring-your-own remote](usage.md#bring-your-own-remote-and-background-runs). |

---

## Former “important gaps” — status

| Theme | Status | Notes |
|--------|--------|--------|
| **Git workflow** | **Largely done** | Default `git_auto_commit`, undo/checkpoint integration, branch helpers, `create_pull_request` via `gh` CLI. See [Usage](usage.md) (git workflow). |
| **Per-mode model / URL / key** | **Done** | `build_*` / `ask_*` / `plan_*` overrides; `openaiclient.NewForMode`. [Configuration](configuration.md). |
| **Hooks** | **Done** | `hooks_enabled`, `hooks.json`. [Context and integrations](context-and-integrations.md). |
| **Checkpointing** | **Done** | `/checkpoint`, `/checkpoints`, restore / rollback / fork; tree + conversation branches. [Usage](usage.md). |
| **Auto lint / test** | **Partial** | AutoCheck runs configured build/lint/test after mutations and **injects** output into the conversation. **No** dedicated “parse failures → fix → rerun until green” loop; the **multi-turn agent** must converge. |
| **Cost / token tracking** | **Done** | Session tracker, built-in model pricing + `cost_per_mtok`, `/tokens`, `-max-cost`. [Configuration](configuration.md#token-usage-and-cost-estimates). |

---

## Former “nice-to-have gaps” — status

| Theme | Status |
|--------|--------|
| **IDE / VS Code extension** | **Out of repo** — not in `codient` CLI; editor integrations may live elsewhere (e.g. Unity package). |
| **Named config profiles** | **Not implemented** — rich flat config + per-mode overrides, but no Codex-style named profile switcher. |
| **Voice input** | **Not implemented** |
| **Session sync across devices** | **Not implemented** |
| **Batch / parallel PRs** | **Not implemented** |
| **LSP** | **Not implemented** — ast-grep / indexing; no language server client. |
| **Structural repo map** | **Done** — system prompt injection + `repo_map` tool (`internal/repomap`). |

---

## Remaining work (prioritized summary)

1. **Parallelism + isolation for sub-agents** — Optional git worktrees (or containers) so multiple delegated tasks can run safely in parallel like some competitors.  
2. **Cloud / background execution** — Use your own VPS, CI, tmux, or containers (**[BYO remote](usage.md#bring-your-own-remote-and-background-runs)**); first-party hosted dispatch / sync is still out of scope.  
3. **Named profiles** — Swap preset bundles (approvals, models, autocheck) with one switch.  
4. **Stronger autonomous verify/fix loop** — Optional explicit loop on top of AutoCheck (parse test output, cap retries, “until green”).  
5. **LSP** — Optional client for definition/rename/type-aware refactors.  
6. **IDE extension / watch mode** — Separate deliverable from core CLI.  
7. **Terminal UX** — Word-level or richer diff presentation if desired beyond unified / colored `git diff`.  

---

## How to use this doc

- **Product / roadmap:** Treat “Partial” as the backlog boundary (delegation model, AutoCheck depth, cloud).  
- **Contributors:** When you ship a feature that closes a row above, update this file in the same PR.
