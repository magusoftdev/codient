# codient

**codient** is a command-line agent for any **OpenAI-compatible** chat API (local server, cloud provider, etc.). It runs multi-step tool use against your workspace—read and search files, run allowlisted commands, and optional write access in **build** mode. **Ask** and **plan** modes use a read-only tool set and different system prompts: ask for exploration, plan for structured implementation designs with clarifying questions.

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

Core connection settings are stored in `~/.codient/config.json` and managed via the `/config` slash command inside a session. No environment variables are needed for basic usage.

### First run

Start codient and set your model (and optionally base URL / API key):

```
codient
/config model gpt-4o-mini
/config base_url http://127.0.0.1:1234/v1
/config api_key your-key-here
```

These values are saved to disk and loaded automatically on subsequent runs.

### `/config` reference

| Key | Description | Default |
|-----|-------------|---------|
| `model` | Model id for chat completions (e.g. `gpt-4o-mini`) | *(none — must be set)* |
| `base_url` | API base URL including `/v1` | `http://127.0.0.1:1234/v1` |
| `api_key` | Sent as `Authorization` bearer token | `codient` |

Run `/config` with no arguments to see current values. The config file location can be overridden with `CODIENT_STATE_DIR`.

### Additional environment variables

Operational settings for power users remain as environment variables:

| Variable | Description |
|----------|-------------|
| `CODIENT_WORKSPACE` | Root for workspace tools. If unset, the current working directory is used. |
| `CODIENT_MODE` | Default mode if `-mode` omitted: `build`, `ask`, or `plan` (the legacy value `design` is still accepted) |
| `CODIENT_CONTEXT_WINDOW` | Model context window in tokens (0 = no limit; enables auto-truncation) |
| `CODIENT_LLM_RETRIES` | Retries for transient LLM errors (default 2) |
| `CODIENT_AUTOCHECK_CMD` | After successful file edits in build mode, run this shell command in the workspace (e.g. `go test ./...`). Empty: auto-detect from `go.mod`, `package.json`, etc. Set to `off` to disable. |
| `CODIENT_SEARCH_URL` | Base URL for a [SearXNG](https://docs.searxng.org/) instance (e.g. `http://localhost:8080`). Enables the `web_search` tool. Empty means `web_search` is not registered. Also configurable via `/setup`. |
| `CODIENT_SEARCH_MAX_RESULTS` | Results per `web_search` query (default 5, max 10). |

See `internal/config/config.go` for tool limits, exec allowlist, and other options.

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
```

Use `-help` for all flags. Notable options:

- **`-mode`** — `build` (default), `ask`, or `plan`
- **`-new-session`** — start fresh instead of resuming the latest session
- **`-repl`** — explicit REPL flag (default when stdin is a TTY; kept for backward compatibility)
- **`-log` / `CODIENT_LOG`** — append JSONL events (LLM rounds, tools)
- **`-goal` / `-task-file`** — merged into the first user turn as a task directive
- **`-design-save-dir` / `CODIENT_DESIGN_SAVE_DIR`** — where to save completed designs (default `<workspace>/.codient/designs`)
- **`CODIENT_DESIGN_SAVE=0`** — disable writing design files

### Slash commands

Inside a session you can use slash commands to control the agent:

| Command | Description |
|---------|-------------|
| `/build` (or `/b`) | Switch to build mode (full write tools) |
| `/plan` (or `/p`; also `/design`, `/d`) | Switch to plan mode (read-only, structured implementation design) |
| `/ask` (or `/a`) | Switch to ask mode (read-only Q&A) |
| `/config [key] [value]` | View or set connection settings (base_url, api_key, model) |
| `/setup` | Guided setup wizard for API connection, model selection, and web search |
| `/compact` | Summarize conversation history to save context space |
| `/model <name>` | Switch to a different model (shortcut for `/config model`) |
| `/workspace <path>` | Change the workspace directory |
| `/tools` | List tools available in current mode |
| `/status` | Show session state (mode, model, turns, tokens) |
| `/log <path>` | Enable or change JSONL logging |
| `/undo` | Discard all unstaged changes (`git checkout -- .`) |
| `/new` (or `/n`) | Start a brand new session (fresh ID, history, and design namespace) |
| `/clear` | Reset conversation history (same session) |
| `/help` (or `/h`, `/?`) | Show available commands |
| `/exit` (or `/quit`, `/q`) | Quit the session |

### Session persistence

Session state (conversation history, mode, model) is automatically saved to `<workspace>/.codient/sessions/` after each turn. When you start codient again in the same workspace, it resumes the latest session. Use `-new-session` to start fresh.

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
