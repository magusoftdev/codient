# codient

[![CI](https://github.com/vaughanb/codient/actions/workflows/ci.yml/badge.svg)](https://github.com/vaughanb/codient/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-informational)](https://github.com/vaughanb/codient/blob/main/LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/vaughanb/codient/main?label=Go&logo=go)](https://github.com/vaughanb/codient/blob/main/go.mod)
[![Latest release](https://img.shields.io/github/v/release/vaughanb/codient?label=release&logo=github)](https://github.com/vaughanb/codient/releases/latest)

**codient** is a command-line agent for any **OpenAI-compatible** chat API (local server, cloud provider, etc.). It runs multi-step tool use against your workspace—read and search files, run allowlisted commands, optional HTTPS fetch and web search, and write access in **build** mode. **Ask** and **plan** modes use a read-only tool set and different system prompts: ask for exploration, plan for structured implementation plans with clarifying questions.

When the API returns usage metadata, codient aggregates **prompt and completion tokens** per session and shows **estimated cost** using a built-in pricing table or your `cost_per_mtok` override. See [Token usage and cost estimates](docs/configuration.md#token-usage-and-cost-estimates).

**Repository:** [github.com/vaughanb/codient](https://github.com/vaughanb/codient)

## Documentation

If you are browsing the [`docs/`](docs/) tree (for example on GitHub), start with [`docs/README.md`](docs/README.md).

| Guide | Contents |
|-------|----------|
| [**Getting started**](docs/getting-started.md) | Requirements, installation (install scripts and building from source) |
| [**Configuration**](docs/configuration.md) | Config file, `/config` keys, subprocess sandboxing, auto-check sequence, tokens and cost, environment variables |
| [**Context and integrations**](docs/context-and-integrations.md) | Web search, semantic search, repository map, MCP servers, lifecycle hooks, auto-update |
| [**Usage**](docs/usage.md) | CLI examples, split-screen TUI, `-print`, images and vision, flags, A2A server, slash commands, git workflow, sessions, memory, plan mode, sub-agents, streaming |
| [**Development**](docs/development.md) | Building and testing the project |

## License

This project is licensed under the MIT License — see [LICENSE](LICENSE) for details.
