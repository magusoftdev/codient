# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Git workflow for **build** mode in a git workspace: optional per-turn auto-commits (`git_auto_commit`), lazy branch creation off configured protected branches (`git_protected_branches`), richer post-turn diff output, slash commands `/diff`, `/branch`, and `/pr` (GitHub CLI), and the **`create_pull_request`** agent tool.
- Config keys **`git_auto_commit`** and **`git_protected_branches`** (see README).

## [0.1.0] - 2026-04-14

### Added

- Initial public release: OpenAI-compatible CLI agent with REPL, ask/plan/build modes, workspace tools (read, grep, patch, shell with allowlist), optional HTTPS fetch, web search, semantic search via embeddings, A2A server mode, session persistence, and project instructions (`AGENTS.md`, `.codient/instructions.md`).

[0.1.0]: https://github.com/vaughanb/codient/releases/tag/v0.1.0
