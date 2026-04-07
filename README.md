# codient

**codient** is a command-line agent for any **OpenAI-compatible** chat API (local server, cloud provider, etc.). It runs multi-step tool use against your workspace—read and search files, run allowlisted commands, optional HTTPS fetch and web search, and write access in **build** mode. **Ask** and **plan** modes use a read-only tool set and different system prompts: ask for exploration, plan for structured implementation designs with clarifying questions.

**Repository:** [github.com/vaughanb/codient](https://github.com/vaughanb/codient)

## Requirements

- [Go](https://go.dev/dl/) 1.26+ (see `go.mod`)
- A running server exposing OpenAI-style `/v1/chat/completions` (default base URL `http://127.0.0.1:1234/v1`; typical for local stacks)

## Install

```bash
git clone https://github.com/vaughanb/codient.git
cd codient
go install ./cmd/codient
```

Or build with Make:

```bash
make install   # installs codient to $(go env GOPATH)/bin
# or
make build     # outputs ./bin/codient
```

## Configuration

Core connection settings are stored under `~/.codient/config.json` (unless `CODIENT_STATE_DIR` points elsewhere) and managed via `/config` and `/setup` inside a session. You do not need environment variables for a minimal setup.

### Persistent config file (`config.json`)

| JSON field | Description | Set via |
|------------|-------------|---------|
| `base_url` | API base URL including `/v1` | `/config base_url …`, `/setup` |
| `api_key` | Sent as `Authorization` bearer token | `/config api_key …`, `/setup` |
| `model` | Model id for chat completions | `/config model …`, `/setup`, `/model` |
| `search_url` | SearXNG base URL for `web_search` | `/setup` (web search step), or `CODIENT_SEARCH_URL` |

`/config` only edits `base_url`, `api_key`, and `model`. Web search URL is configured in `/setup` or with `CODIENT_SEARCH_URL`.

### First run

Start codient and set your model (and optionally base URL / API key):

```
codient
/config model gpt-4o-mini
/config base_url http://127.0.0.1:1234/v1
/config api_key your-key-here
```

Or run `/setup` for a guided wizard (connection, model, optional SearXNG).

### `/config` reference

| Key | Description | Default |
|-----|-------------|---------|
| `model` | Model id for chat completions (e.g. `gpt-4o-mini`) | *(none — must be set)* |
| `base_url` | API base URL including `/v1` | `http://127.0.0.1:1234/v1` |
| `api_key` | Sent as `Authorization` bearer token | `codient` |

Run `/config` with no arguments to see current values.

### Environment variables

**Workspace and state**

| Variable | Description |
|----------|-------------|
| `CODIENT_WORKSPACE` | Root for workspace tools. If unset, the process working directory is used. Overridden by `-workspace`. |
| `CODIENT_STATE_DIR` | Directory for `config.json` instead of `~/.codient`. |
| `CODIENT_MODE` | Default mode if `-mode` omitted: `build`, `ask`, or `plan` (legacy `design` is accepted) |

**LLM and agent limits**

| Variable | Description |
|----------|-------------|
| `CODIENT_CONTEXT_WINDOW` | Model context window in tokens (`0` = no explicit limit; server may still be probed after model changes) |
| `CODIENT_CONTEXT_RESERVE` | Tokens reserved for the assistant reply when estimating usage (default 4096) |
| `CODIENT_LLM_RETRIES` | Retries for transient LLM errors (default 2) |
| `AGENT_MAX_TOOL_STEPS` | Maximum tool rounds per user turn (default 1000) |
| `LLM_MAX_CONCURRENT` | Max concurrent in-flight completion requests (default 3) |
| `CODIENT_STREAM_WITH_TOOLS` | Set to `1` to stream chat completions even when tools are enabled (many local servers mishandle this; default off) |

**Exec (`run_command` / `run_shell`)**

| Variable | Description |
|----------|-------------|
| `CODIENT_EXEC_ALLOWLIST` | Comma-separated command names allowed as `argv[0]` (default: `go`, `git`, and platform shell `cmd` or `sh`) |
| `CODIENT_EXEC_DISABLE` | Set to `1` to disable exec tools entirely |
| `CODIENT_EXEC_TIMEOUT_SEC` | Per-command timeout (default 120, max 3600) |
| `CODIENT_EXEC_MAX_OUTPUT_BYTES` | Cap on combined stdout+stderr (default 256 KiB, max 10 MiB) |

**Web search and fetch**

| Variable | Description |
|----------|-------------|
| `CODIENT_SEARCH_URL` | SearXNG base URL (e.g. `http://localhost:8080`). Enables `web_search`. Also stored as `search_url` in config after `/setup`. |
| `CODIENT_SEARCH_MAX_RESULTS` | Results per query (default 5, max 10) |
| `CODIENT_FETCH_ALLOW_HOSTS` | Comma-separated hostnames allowed for `fetch_url` (HTTPS GET). Subdomains match. Empty disables the tool. |
| `CODIENT_FETCH_MAX_BYTES` | Max response body bytes (default 1 MiB, max 10 MiB) |
| `CODIENT_FETCH_TIMEOUT_SEC` | Per-fetch timeout (default 30, max 300) |

When `fetch_url` receives `Content-Type: text/html`, the body is converted to simplified markdown (headings, links, lists, code) before being returned.

**Quality-of-life and output**

| Variable | Description |
|----------|-------------|
| `CODIENT_AUTOCHECK_CMD` | After successful file edits in build mode, run this shell command in the workspace. Empty: auto-detect from `go.mod`, `package.json`, etc. Set to `off` to disable. |
| `CODIENT_AUTOCOMPACT_THRESHOLD` | Context usage percent (0–100) that triggers automatic history compaction between turns. `0` disables. Default 75. |
| `CODIENT_LOG` | Default JSONL log path (overridden by `-log`) |
| `CODIENT_PLAIN` | Set to `1` for plain assistant text (no markdown styling) |
| `CODIENT_PROGRESS` | `1` forces progress on stderr; `0` disables even if `-progress` is set |
| `CODIENT_STREAM_REPLY` | `0` / `1` overrides `-stream-reply` for TTY streaming |
| `CODIENT_QUIET` | `1` skips the welcome banner |
| `CODIENT_VERBOSE` | `1` enables extra session diagnostics |

**Plan mode and designs**

| Variable | Description |
|----------|-------------|
| `CODIENT_DESIGN_SAVE_DIR` | Override directory for saved implementation designs (default `<workspace>/.codient/designs`) |
| `CODIENT_DESIGN_SAVE` | Set to `0` to disable writing design markdown files |

**Project context**

| Variable | Description |
|----------|-------------|
| `CODIENT_PROJECT_CONTEXT` | Set to `off` to skip auto-injected project hints from workspace markers |

For defaults and validation details, see [`internal/config/config.go`](internal/config/config.go).

## Usage

```bash
# Ping the server
codient -ping

# List models and tools
codient -list-models
codient -list-tools

# Start an interactive session (default when stdin is a TTY)
codient

# One-shot prompt (no interactive session)
codient -prompt "Summarize README.md"

# Start a session with an initial prompt
codient -prompt "Help me understand the repo layout"

# Start in plan mode
codient -mode plan -prompt "Design a small Go CLI for managing todos"

# Force a fresh session (skip resume)
codient -new-session

# Different workspace root
codient -workspace /path/to/repo
```

Use `-help` for all flags. Notable options:

- **`-mode`** — `build` (default), `ask`, or `plan`
- **`-workspace`** — workspace root (overrides `CODIENT_WORKSPACE` and cwd)
- **`-new-session`** — start fresh instead of resuming the latest session
- **`-repl`** — explicit REPL (default when stdin is a TTY)
- **`-system`** — optional extra system prompt merged into the default tool prompt
- **`-stream` / `-stream-reply`** — streaming behavior (`CODIENT_STREAM_REPLY` can override reply streaming)
- **`-plain`** — raw assistant text (or `CODIENT_PLAIN=1`)
- **`-progress`** — agent progress on stderr (`CODIENT_PROGRESS` can force or disable)
- **`-log` / `CODIENT_LOG`** — append JSONL events (LLM rounds, tools)
- **`-goal` / `-task-file`** — merged into the first user turn as a task directive
- **`-design-save-dir` / `CODIENT_DESIGN_SAVE_DIR`** — where to save completed designs
- **`-a2a` / `-a2a-addr`** — run an [A2A](https://github.com/a2aproject/A2A) protocol server instead of the interactive CLI (default listen `:8080`)

### A2A server

To expose codient as an Agent-to-Agent HTTP server:

```bash
codient -a2a -a2a-addr :8080
```

Use the same config (model, base URL, API key, workspace) as the CLI. See [`internal/a2aserver/`](internal/a2aserver/) for protocol details.

### Slash commands

Inside a session you can use slash commands to control the agent:

| Command | Description |
|---------|-------------|
| `/build` (or `/b`) | Switch to build mode (full write tools) |
| `/plan` (or `/p`; also `/design`, `/d`) | Switch to plan mode (read-only, structured implementation design) |
| `/ask` (or `/a`) | Switch to ask mode (read-only Q&A) |
| `/config [key] [value]` | View or set `base_url`, `api_key`, or `model` (saved to disk) |
| `/setup` | Guided setup wizard for API connection, model selection, and optional web search |
| `/compact` | Summarize conversation history to save context space |
| `/model <name>` | Switch to a different model (shortcut for `/config model`) |
| `/workspace <path>` | Change the workspace directory |
| `/tools` | List tools available in current mode |
| `/status` | Show session state (mode, model, turns, tokens, auto-check, exec policy) |
| `/log [path]` | Show logging status or enable JSONL logging to a file |
| `/undo` | Undo the last build turn using git (restore modified files, remove new files from that turn). Requires a git repo. Use `/undo all` to revert every tracked turn in the stack. |
| `/new` (or `/n`) | Start a brand new session (fresh ID, history, and design namespace) |
| `/clear` | Reset conversation history (same session) |
| `/help` (or `/h`, `/?`) | Show available commands |
| `/exit` (or `/quit`, `/q`) | Quit the session |

### Session persistence

Session state (conversation history, mode, model) is saved under `<workspace>/.codient/sessions/` after each turn. Starting codient again in the same workspace resumes the latest session. Use `-new-session` to start fresh.

### Plan mode and saved designs

In **plan** mode, when the assistant's reply includes a **Ready to implement** section, codient saves the markdown under the workspace (by default `.codient/designs/<sessionID>/`). Designs are scoped to the session that created them. Filenames are `{task-slug}_{date-time}_{nanoseconds}.md` so runs never collide. The task slug comes from `-goal`, else `-task-file` basename, else the first line of your first message.

### Streaming

Assistant text can stream to the terminal as it is generated (`-stream-reply`, default on for TTYs). In plan mode with styled markdown, the turn that produces the full design after a blocking question is buffered once so the reply can be rendered with full markdown formatting.

## Development

```bash
make check       # vet + unit tests only (no live LLM; safe for CI)
make test-unit   # same tests as check, without vet
make test        # full suite: unit tests + live integration (needs ~/.codient/config.json model + API)
```

`make test` sets `CODIENT_INTEGRATION=1`, `CODIENT_INTEGRATION_STRICT_TOOLS=1`, and `CODIENT_INTEGRATION_RUN_COMMAND=1`, and runs `go test -tags=integration` with a 90-minute timeout so workspace tools, strict tool-calling, and `run_command` are all exercised.

Lighter integration runs (see `make help`):

```bash
make test-integration         # live API only (CODIENT_INTEGRATION=1)
make test-integration-strict  # + strict tool tests (no run_command test unless you set CODIENT_INTEGRATION_RUN_COMMAND=1 yourself)
```

## License

No license file is set in this repository yet; add one if you intend to open-source under specific terms.
