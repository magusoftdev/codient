# Feature parity vs. industry coding agents

This document supersedes informal “gap list” audits: it records **what codient implements today** and **what is still open** relative to common expectations (Claude Code, Cursor CLI, Codex CLI, Gemini CLI, Aider, etc.). Update it when parity-relevant behavior or docs change.

**Pointers:** [Configuration](configuration.md) · [Usage](usage.md) · [Context and integrations](context-and-integrations.md)

---

## Strengths (unchanged themes)

- Intent-Driven Orchestrator (auto-only): supervisor LLM picks the internal path (ask / plan / build / plan -> auto-handoff to build) per turn, with per-path tool scoping  
- Two-tier reasoning model selection (`low_reasoning_model` / `high_reasoning_model`)  
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
| **Sub-agents / delegation** | **Done** | `delegate_task` + `internal/subagent`: nested child runner that the model can fan out via multiple tool calls in a single round. The agent runner executes those tool calls concurrently (`internal/agent/runner.go`, `sync.WaitGroup` around `runOneTool`), and **`delegate_git_worktrees=true`** gives each delegated task its own `git worktree` under the state directory (`internal/codientcli/session.go::delegateTaskFn`, `internal/gitutil.AddDelegateWorktree`). **`delegate_sandbox_profiles`** defines named container isolation profiles (per-delegate image, network policy, resource caps, optional long-lived container sessions). The model picks a profile via `sandbox_profile` on `delegate_task`. Symlink-escape hardening on file tools prevents traversal outside the worktree. See [Configuration](configuration.md#delegate-sandbox-profiles). |
| **Sandbox / isolation** | **Done (platform-dependent)** | `sandbox_mode`: `off`, `native`, `container`, `auto`. Native: Linux/Darwin/Windows job limits; **container**: Docker/Podman. See [Configuration](configuration.md) (sandboxing). |
| **Multimodal (images)** | **Done** | CLI `-image`, REPL `/image`, `/paste` (clipboard), Ctrl+V in TUI; use vision-capable models. See [Usage](usage.md). |
| **File references (`@path`)** | **Done** | `@path/to/file.go` inlines file contents into the user message. Drag-and-drop detection auto-prefixes pasted paths. See [Usage](usage.md#file-references-path). |
| **Headless / CI** | **Done** | `-print`, `-auto-approve`, JSON / `stream-json` (`session_id`, `workspace`, `cost_usd`). Sessions persist under **`.codient/sessions/`**; resume latest or **`-session-id`** for chained runs. First-party hosted dispatch is an explicit **non-goal** (see [Non-goals](#non-goals)); host codient yourself with [Bring-your-own remote](usage.md#bring-your-own-remote-and-background-runs). |
| **Plan -> build handoff** | **Done** | The orchestrator drives the transition automatically on COMPLEX_TASK turns: after a `Ready to implement` plan-path reply, codient injects a structured implementation directive and the next user message implements without an extra approval menu (gated by an interactive prompt or `-force` / `-yes` in non-interactive runs). See [Usage](usage.md#plan-build-hand-off). Aligned with Claude Code, Cursor, Codex CLI. |

---

## Former “important gaps” — status

| Theme | Status | Notes |
|--------|--------|--------|
| **Git workflow** | **Largely done** | Default `git_auto_commit`, undo/checkpoint integration, branch helpers, `create_pull_request` via `gh` CLI. See [Usage](usage.md) (git workflow). |
| **Per-tier model / URL / key** | **Done** | `low_reasoning_*` / `high_reasoning_*` overrides routed through `config.ConnectionForTier` and `openaiclient.NewForTier`. The orchestrator uses the low tier for QUERY / SIMPLE_FIX (and the supervisor itself) and the high tier for DESIGN / COMPLEX_TASK planning. [Configuration](configuration.md). |
| **Hooks** | **Done** | `hooks_enabled`, `hooks.json`. [Context and integrations](context-and-integrations.md). |
| **Checkpointing** | **Done** | `/checkpoint`, `/checkpoints`, restore / rollback / fork; tree + conversation branches. [Usage](usage.md). |
| **Auto lint / test** | **Done** | AutoCheck runs configured build/lint/test after mutations (fail-fast). Optional **fix loop** (`autocheck_fix_max_retries > 0`): the runner re-runs the failing step after each model edit, tracks a per-language failure signature for no-progress detection, and stops at the configured cap. Parser registry: Go-test parser + opaque default. See [Configuration](configuration.md#fix-loop). |
| **Cost / token tracking** | **Done** | Session tracker, built-in model pricing + `cost_per_mtok`, `/tokens`, `-max-cost`. [Configuration](configuration.md#token-usage-and-cost-estimates). |

---

## Former “nice-to-have gaps” — status

| Theme | Status | Notes |
|--------|--------|--------|
| **IDE / VS Code extension** | **Out of repo** | Not in `codient` CLI; editor integrations may live elsewhere (e.g. Unity package). |
| **Named config profiles** | **Done** | `-profile`, `CODIENT_PROFILE`, `/profile` slash command, ACP RPCs (`agent/list_profiles`, `session/set_profile`). See [Configuration](configuration.md#named-profiles). |
| **Voice input** | **Not implemented** | |
| **Session sync across devices** | **Non-goal (first-party)** | Roll your own with `SessionEnd` hooks that `rclone` / `aws s3 sync` **`<workspace>/.codient/sessions/`** to storage you own — see [Bring-your-own remote](usage.md#bring-your-own-remote-and-background-runs). |
| **Batch / parallel PRs** | **Not implemented** | |
| **LSP** | **Not implemented** | ast-grep / indexing; no language server client. |
| **Structural repo map** | **Done** | System prompt injection + `repo_map` tool (`internal/repomap`). |

---

## Remaining work (prioritized summary)

1. **LSP** — Optional client for definition/rename/type-aware refactors.  
2. **IDE extension / watch mode** — Separate deliverable from core CLI.  
3. **Terminal UX** — Word-level or richer diff presentation if desired beyond unified / colored `git diff`.  

---

## Non-goals

Codient is an **open-source, self-hosted** coding agent. The following are explicit non-goals — they will **not** be built as first-party features:

- **First-party hosted dispatch / cloud agents** — codient itself will not run a managed control plane that accepts prompts + repo URLs and dispatches runs on infrastructure operated by the project. Run codient on your own VPS, CI, container, or workstation; see [Bring-your-own remote](usage.md#bring-your-own-remote-and-background-runs).
- **First-party session sync across devices** — no codient-operated cloud will mirror **`.codient/sessions/`** between machines. Wire up your own object-storage sync via [lifecycle hooks](context-and-integrations.md#lifecycle-hooks) (e.g. `SessionEnd` running `rclone` or `aws s3 sync`).
- **Auth / multi-tenant / billing** — no codient-operated account system. API credentials remain in your local `~/.codient/config.json` (or `CODIENT_STATE_DIR`) and go directly to the provider you configured.

These items are listed for clarity so contributors and users don't expect them on the roadmap.

---

## How to use this doc

- **Product / roadmap:** Treat “Partial” as the backlog boundary (delegation model, AutoCheck depth). Items in [Non-goals](#non-goals) are intentionally out of scope and should not be added to the backlog.  
- **Contributors:** When you ship a feature that closes a row above, update this file in the same PR.
