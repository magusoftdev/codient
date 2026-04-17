# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Build-turn git summary:** After a build turn with working-tree changes, stderr shows `git diff --stat` relative to `HEAD` and lists untracked files (instead of printing a large unified diff).

### Added

- **Repository map:** structural workspace overview (paths + top-level symbols) injected into the system prompt and available via the **`repo_map`** tool. Config **`repo_map_tokens`** (`0` = auto budget, `-1` = off). Cached under `<workspace>/.codient/index/repomap.gob`.
- **Checkpoints:** named snapshots of conversation + git state under `<workspace>/.codient/checkpoints/<sessionID>/`, with slash commands **`/checkpoint`**, **`/checkpoints`**, **`/rollback`**, **`/fork`**, and **`/branches`**. Config **`checkpoint_auto`**: `plan` (default; checkpoints at each completed plan phase group), `all` (after each build turn with changes), or `off`.
- Git workflow for **build** mode in a git workspace: optional per-turn auto-commits (`git_auto_commit`), lazy branch creation off configured protected branches (`git_protected_branches`), richer post-turn diff output, slash commands `/diff`, `/branch`, and `/pr` (GitHub CLI), and the **`create_pull_request`** agent tool.
- Config keys **`git_auto_commit`** and **`git_protected_branches`** (see README).

## [0.1.0] - 2026-04-14

### Added

- Initial public release: OpenAI-compatible CLI agent with REPL, ask/plan/build modes, workspace tools (read, grep, patch, shell with allowlist), optional HTTPS fetch, web search, semantic search via embeddings, A2A server mode, session persistence, and project instructions (`AGENTS.md`, `.codient/instructions.md`).

[0.1.0]: https://github.com/vaughanb/codient/releases/tag/v0.1.0
