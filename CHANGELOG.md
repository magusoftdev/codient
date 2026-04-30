# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`/create-rule`** REPL slash command: interactive wizard that writes a Cursor-compatible **`.mdc`** rule under **`<workspace>/.cursor/rules/`** (`description`, optional **`globs`**, **`alwaysApply`**). See [docs/usage.md](docs/usage.md#slash-commands).

## [0.10.0] - 2026-04-29

### Added

- **ACP stdio** (`-acp`): [Agent Client Protocol](https://agentclientprotocol.com/) over NDJSON on stdin/stdout for editor hosts (e.g. **Codient Unity**). Includes `initialize`, `session/new`, `session/prompt`, **`session/set_model`** (with optional model preload and **`session/model_status`** notifications), **`agent/list_models`**, and Unity-shaped workspaces register **`unity_*`** tools that round-trip **`unity/*`** JSON-RPC to the editor. See [docs/usage.md](docs/usage.md#acp-stdio-agent).
- **Subprocess sandboxing:** configurable `sandbox_mode` (`off`, `native`, `container`, `auto`) with env scrubbing for tools, hooks, MCP, and verification commands. See [docs/configuration.md](docs/configuration.md#subprocess-sandboxing).
- **Repository map:** structural workspace overview (paths + top-level symbols) in the system prompt and **`repo_map`** tool; cache under `<workspace>/.codient/index/repomap.gob`. Config **`repo_map_tokens`** (`0` = auto, `-1` = off).

### Changed

- **Documentation:** user guides live under [`docs/`](docs/README.md); root README is a short entry point.
- **Build-turn git summary:** after a build turn with working-tree changes, stderr shows `git diff --stat` vs `HEAD` and lists untracked files (instead of a large unified diff).

### Fixed

- **ACP:** emit assistant text when stream chunks are absent so clients still receive the full reply.

## [0.9.0] - 2026-04-17

### Added

- **Split-screen Bubble Tea TUI** when stdin is a TTY (scrollable output + fixed input; use `-plain` for classic REPL).
- **Build / lint / test auto-check** sequence after mutating file tools in build mode (`autocheck_cmd`, `lint_cmd`, `test_cmd`; empty = auto-detect, `off` = skip).
- **Checkpoints:** named snapshots of conversation + git state under `<workspace>/.codient/checkpoints/<sessionID>/`; slash commands **`/checkpoint`**, **`/checkpoints`**, **`/rollback`**, **`/fork`**, **`/branches`**. Config **`checkpoint_auto`**: `plan` (default), `all`, or `off`.
- **Lifecycle hooks** and welcome context display (`hooks_enabled`, `hooks.json`); see [docs/context-and-integrations.md](docs/context-and-integrations.md#lifecycle-hooks).

### Changed

- Default auto-commit subject uses **`codient: turn N`** (was `codient: turn 1` for every turn).

## [0.8.0] - 2026-04-16

### Added

- **Headless / CI** mode: **`-print`** / **`-p`** with `-output-format` (`text`, `json`, `stream-json`), `-auto-approve`, `-max-turns`, `-max-cost`.

## [0.7.0] - 2026-04-16

### Added

- **Multimodal images** for vision-capable models (`-image`, `/image`, `@image:` paths).
- **Session token usage and cost estimates** when the API returns usage metadata.

## [0.6.0] - 2026-04-16

### Added

- **Git workflow** in build mode: optional per-turn auto-commits (`git_auto_commit`), protected-branch handling (`git_protected_branches`), **`/diff`**, **`/branch`**, **`/pr`**, and **`create_pull_request`** tool.

## [0.5.0] - 2026-04-15

### Added

- **Sub-agent delegation** (`delegate_task`) with per-mode model configuration.

## [0.4.0] - 2026-04-15

### Added

- **MCP (Model Context Protocol)** client and namespaced tools (`mcp__<server>__<tool>`).

## [0.3.0] - 2026-04-15

### Added

- **Self-update:** interactive prompt on REPL startup and **`-update`** flag.
- **Integration tests** (live API) behind `CODIENT_INTEGRATION`.

## [0.2.0] - 2026-04-14

### Changed

- Install scripts and README refresh; removed outdated env references.

## [0.1.3] - 2026-04-14

### Fixed

- Lint fixes.

## [0.1.2] - 2026-04-14

### Fixed

- Lint fixes.

## [0.1.1] - 2026-04-14

### Changed

- CI workflow updates.

## [0.1.0] - 2026-04-14

### Added

- Initial public release: OpenAI-compatible CLI agent with REPL, ask/plan/build modes, workspace tools (read, grep, patch, shell with allowlist), optional HTTPS fetch, web search, semantic search via embeddings, A2A server mode, session persistence, and project instructions (`AGENTS.md`, `.codient/instructions.md`).

[Unreleased]: https://github.com/vaughanb/codient/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/vaughanb/codient/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/vaughanb/codient/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/vaughanb/codient/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/vaughanb/codient/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/vaughanb/codient/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/vaughanb/codient/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/vaughanb/codient/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/vaughanb/codient/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/vaughanb/codient/compare/v0.1.0...v0.2.0
[0.1.3]: https://github.com/vaughanb/codient/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/vaughanb/codient/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/vaughanb/codient/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/vaughanb/codient/releases/tag/v0.1.0
