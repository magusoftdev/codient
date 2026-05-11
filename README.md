# codient

[![CI](https://github.com/magusoftdev/codient/actions/workflows/ci.yml/badge.svg)](https://github.com/magusoftdev/codient/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-informational)](https://github.com/magusoftdev/codient/blob/main/LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/magusoftdev/codient/main?label=Go&logo=go)](https://github.com/magusoftdev/codient/blob/main/go.mod)
[![Latest release](https://img.shields.io/github/v/release/magusoftdev/codient?label=release&logo=github)](https://github.com/magusoftdev/codient/releases/latest)

**codient** is a command-line agent for any **OpenAI-compatible** chat API (local server, cloud provider, etc.). It runs multi-step tool use against your workspace — read and search files, run allowlisted commands, optional HTTPS fetch and web search, and write access when the orchestrator routes a turn into the build path.

Codient runs the **Intent-Driven Orchestrator** on every turn: a tiny supervisor LLM call classifies each user prompt into **QUERY** (read-only Q&A), **DESIGN** (read-only architectural plan), **SIMPLE_FIX** (build), or **COMPLEX_TASK** (plan, then auto-handoff to build), and selects the right tool set, system prompt, and reasoning tier behind the scenes. Configure two reasoning tiers — **`low_reasoning_model`** (supervisor, QUERY answers, SIMPLE_FIX builds) and **`high_reasoning_model`** (DESIGN advice and COMPLEX_TASK planning) — and just type `codient "your prompt"`. There are no manual mode flags or slash commands: pass **`-force`** / **`-yes`** to auto-approve plan -> build hand-offs in non-interactive runs.

When the API returns usage metadata, codient aggregates **prompt and completion tokens** per session and shows **estimated cost** using a built-in pricing table or your `cost_per_mtok` override. See [Token usage and cost estimates](docs/configuration.md#token-usage-and-cost-estimates).

**Repository:** [github.com/magusoftdev/codient](https://github.com/magusoftdev/codient)

**Unity Editor:** the companion [Codient Unity](https://github.com/magusoftdev/codient-unity) package runs this binary in **ACP** mode from the Unity Editor (chat UI, Unity context tools, optional release install). You need a `codient` build whose **`-help`** lists **`-acp`**. See [ACP stdio](docs/usage.md#acp-stdio-agent) and the Unity project README when both repos are checked out side by side.

## Install

You need a running server with OpenAI-style `/v1/chat/completions` (the default in config is `http://127.0.0.1:1234/v1`, typical for local stacks). Prebuilt release installs do not require Go on your PATH; [Go](https://go.dev/dl/) **1.26+** is only required for `go install` or `make install` from a clone (see `go.mod`).

**macOS / Linux:**

```bash
curl -sSfL https://raw.githubusercontent.com/magusoftdev/codient/main/scripts/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/magusoftdev/codient/main/scripts/install.ps1 | iex
```

Both scripts detect your OS and architecture, download the latest release binary, and place it on your `PATH`. Set `CODIENT_INSTALL_DIR` to override the install location (defaults to `~/.local/bin` on Unix, `%LOCALAPPDATA%\codient` on Windows).

**From source** (requires [Go](https://go.dev/dl/) 1.26+):

```bash
go install github.com/magusoftdev/codient/cmd/codient@latest
```

Or clone and build with Make:

```bash
git clone https://github.com/magusoftdev/codient.git
cd codient
make install   # copies built bin/codient to ~/.local/bin (Unix) or %LOCALAPPDATA%\codient (Windows), same as install.sh / install.ps1; set CODIENT_INSTALL_DIR to override
```

Optional tools (ast-grep, Git, GitHub CLI) and a fuller requirements list: [Getting started](docs/getting-started.md).

## Documentation

If you are browsing the [`docs/`](docs/) tree (for example on GitHub), start with [`docs/README.md`](docs/README.md).

| Guide | Contents |
|-------|----------|
| [**Getting started**](docs/getting-started.md) | Requirements and optional tools (install steps are above) |
| [**Configuration**](docs/configuration.md) | Config file, `/config` keys, subprocess sandboxing, auto-check sequence, tokens and cost, environment variables |
| [**Context and integrations**](docs/context-and-integrations.md) | Web search, semantic search, repository map, MCP servers, lifecycle hooks, auto-update |
| [**Usage**](docs/usage.md) | CLI examples, split-screen TUI, `-print` (session resume, JSON `session_id`), BYO remote/CI, images and vision, flags, **`-acp`** (Agent Client Protocol stdio for editors such as Codient Unity), A2A server, slash commands, git workflow, sessions, memory, the Intent-Driven Orchestrator and plan -> build hand-off, sub-agents, streaming |
| [**Development**](docs/development.md) | Building and testing the project |
| [**Feature parity**](docs/feature-parity.md) | Gap list vs. other agents (kept current) |

## License

This project is licensed under the MIT License — see [LICENSE](LICENSE) for details.
