# Configuration

Most settings are stored in `~/.codient/config.json` (unless `CODIENT_STATE_DIR` points elsewhere) and managed via `/config` and `/setup` inside a session. A few environment variables override defaults (see below); everything else is config file or CLI flags.

## First run

Start codient and set your model (and optionally base URL / API key):

```
codient
/config model gpt-4o-mini
/config base_url http://127.0.0.1:1234/v1
/config api_key your-key-here
```

Or run `/setup` for a guided wizard (connection and model selection).

## Config file reference (`~/.codient/config.json`)

All settings live in a single JSON file. Use `/config` to view and edit, or edit the file directly. Omitted fields use built-in defaults. CLI flags (e.g. `-mode`, `-plain`, `-workspace`) override config file values when explicitly passed.

**Example config.json:**

```json
{
  "base_url": "http://127.0.0.1:1234/v1",
  "api_key": "codient",
  "model": "qwen3-coder",
  "mode": "build",
  "search_url": "http://localhost:8888",
  "fetch_allow_hosts": "docs.go.dev,pkg.go.dev",
  "autocheck_cmd": "go build ./...",
  "lint_cmd": "golangci-lint run ./...",
  "test_cmd": "go test ./...",
  "verbose": true
}
```

Omit **`lint_cmd`** and **`test_cmd`** to use [auto-detection](#auto-check-sequence) (same as empty string). Set either to **`off`** to skip that step in the post-edit sequence.

**Per-mode models and endpoints** — Under `models`, you can override `base_url`, `api_key`, and `model` for `plan`, `build`, and `ask`. Any field left out inherits from the top-level connection. Use this for a remote planning API and a local implementation server, for example:

```json
{
  "base_url": "http://127.0.0.1:1234/v1",
  "api_key": "codient",
  "model": "qwen3-coder-30b",
  "models": {
    "plan": {
      "base_url": "https://api.openai.com/v1",
      "api_key": "sk-...",
      "model": "gpt-4.1"
    }
  }
}
```

The interactive `/setup` wizard can also configure a separate plan server after you pick the default model. Slash commands `/config plan_base_url`, `plan_api_key`, `plan_model` (and `build_*`, `ask_*`) mirror these fields.

## `/config` reference

Run `/config` with no arguments to see all current values. `/config <key>` shows one value. `/config <key> <value>` sets and persists.

| Key | Description | Default |
|-----|-------------|---------|
| **Connection** | | |
| `base_url` | API base URL including `/v1` | `http://127.0.0.1:1234/v1` |
| `api_key` | Sent as `Authorization` bearer token | `codient` |
| `model` | Default model id (used by modes that have no override) | *(none — must be set for typical use)* |
| `plan_model`, `build_model`, `ask_model` | Model id for that mode only | inherit `model` |
| `plan_base_url`, `build_base_url`, `ask_base_url` | API base URL for that mode | inherit `base_url` |
| `plan_api_key`, `build_api_key`, `ask_api_key` | API key for that mode | inherit `api_key` |
| **Defaults** | | |
| `mode` | Default mode: `build`, `ask`, or `plan` | `build` |
| `workspace` | Root for workspace tools | *(process working directory)* |
| **Agent limits** | | |
| `max_concurrent` | Max concurrent in-flight completion requests | `3` |
| **Exec** | | |
| `exec_allowlist` | Comma-separated command names allowed as `argv[0]` | `go,git,cmd` (or `sh`) |
| `exec_env_passthrough` | Extra environment variable names forwarded to subprocesses after [secret scrubbing](#subprocess-sandboxing) | *(empty)* |
| `exec_timeout_sec` | Per-command timeout (max 3600) | `120` |
| `exec_max_output_bytes` | Cap on combined stdout+stderr (max 10 MiB) | `262144` |
| `sandbox_mode` | Subprocess isolation: `off` (default), `native` (OS sandbox on Linux/macOS/Windows), `container` (Docker/Podman), `auto` (native if available, else container, else env scrub only) | `off` |
| `sandbox_ro_paths` | Comma-separated extra host paths granted read-only in native/container sandboxes | *(empty)* |
| `sandbox_container_image` | OCI image for `sandbox_mode: container` | `alpine:3.20` (built-in default) |
| **Context** | | |
| `context_window` | Model context window in tokens (`0` = probe server at startup; shown on the welcome banner as **Context**) | `0` |
| `context_reserve` | Tokens reserved for the assistant reply | `4096` |
| **LLM** | | |
| `max_llm_retries` | Retries for transient LLM errors | `2` |
| `max_completion_seconds` | Per-completion timeout (max 3600) | `300` |
| `stream_with_tools` | Stream completions when tools are enabled | `false` |
| **Fetch** | | |
| `fetch_allow_hosts` | Comma-separated hostnames for `fetch_url` (subdomains match) | *(empty)* |
| `fetch_preapproved` | Built-in documentation-domain preset for `fetch_url` | `true` |
| `fetch_max_bytes` | Max response body bytes (max 10 MiB) | `1048576` |
| `fetch_timeout_sec` | Per-fetch timeout (max 300) | `30` |
| `fetch_web_rate_per_sec` | Token-bucket rate limit for `fetch_url` and `web_search` combined (`0` = off, max 100) | `0` |
| `fetch_web_rate_burst` | Burst size for that limiter (`0` with a positive rate defaults to the rate; max 50) | `0` |
| **Search** | | |
| `search_max_results` | Results per `web_search` query (max 10) | `5` |
| **Auto** | | |
| `autocompact_threshold` | Context usage % that triggers compaction (0 disables) | `75` |
| `autocheck_cmd` | **Build** check after mutating file tools in **build** mode (empty = auto-detect from workspace, `off` = skip). Runs first in the auto-check sequence. | *(auto)* |
| `lint_cmd` | **Lint** check in the same sequence (empty = auto-detect, `off` = skip). Runs after build succeeds (**fail-fast**). | *(auto)* |
| `test_cmd` | **Test** check in the same sequence (empty = auto-detect, `off` = skip). Runs after lint succeeds. | *(auto)* |
| **Git (build mode)** | | |
| `git_auto_commit` | After each build turn that changes files, commit with message `codient: turn N` (set `false` for legacy file-restore `/undo` without commits) | `true` |
| `delegate_git_worktrees` | When **`true`**, each **`delegate_task`** sub-agent runs in a **detached git worktree** at **`HEAD`** under `~/.codient/delegate-worktrees/` (requires a git workspace and `git` on `PATH`). Filesystem edits there are **not merged** into the parent workspace. Uncommitted changes in the main tree are **not** visible in the worktree. Sub-agents omit the **`repo_map`** tool for this path (MVP). | `false` |
| `git_protected_branches` | Comma-separated branch names; when the first change lands on one of these, codient creates `codient/<task-slug>` and commits there | `main,master,develop` |
| `checkpoint_auto` | Automatic checkpoints: **`plan`** (after each completed plan phase group), **`all`** (after each build turn that changes files and commits), **`off`** (manual `/checkpoint` only) | `plan` |
| **UI/Output** | | |
| `plain` | Raw assistant text (no markdown/ANSI) | `false` |
| `quiet` | Suppress the welcome banner | `false` |
| `verbose` | Extra session diagnostics | `false` |
| `log` | Default JSONL log path | *(empty)* |
| `stream_reply` | Stream assistant tokens to stdout | `true` |
| `progress` | Force progress output on stderr | `false` |
| `acp_preload_model_on_set_model` | When **`true`** (default), ACP **`session/set_model`** runs a minimal chat completion so local inference servers load the model before the RPC returns; set **`false`** to skip (saves one completion per switch) | `true` |
| **Plan** | | |
| `design_save_dir` | Override directory for saved plans | `<workspace>/.codient/plans` |
| `design_save` | Save plan-mode plans to disk | `true` |
| **Project** | | |
| `project_context` | `off` to skip auto-injected project hints | *(empty)* |
| **Tools** | | |
| `ast_grep` | ast-grep binary path: `auto` (default), explicit path, or `off` to disable | *(auto)* |
| `embedding_model` | Model id for `/v1/embeddings` (same base URL as chat). Enables the `semantic_search` tool; leave empty to disable. The welcome banner shows **Embeddings** (model id, or `off`) | *(empty)* |
| `repo_map_tokens` | Approximate token budget for the **structural repository map** injected into the system prompt and for the `repo_map` tool. **`0`** (default) picks a budget from workspace size; **`-1`** disables the map and the tool | `0` |
| `hooks_enabled` | Enable [lifecycle hooks](context-and-integrations.md#lifecycle-hooks) (`hooks.json` under `~/.codient` and `<workspace>/.codient`) | `false` |
| `cost_per_mtok` | Optional `{"input":N,"output":N}` USD per 1M tokens — overrides built-in pricing for `/cost` and session cost estimates | *(built-in table)* |
| **Update** | | |
| `update_notify` | Show interactive update prompt on REPL startup | `true` |
| **MCP** | | |
| `mcp_servers` | Map of MCP server IDs to connection configs (see [MCP servers](context-and-integrations.md#mcp-model-context-protocol-servers)) | *(empty)* |

## Subprocess sandboxing

Codient **always scrubs** the parent environment before spawning tools, hooks, MCP stdio servers, verification commands, and `run_command` / `run_shell`: known secret patterns (e.g. `*_TOKEN`, cloud prefixes, `SSH_AUTH_SOCK`) are removed. A safe baseline (`PATH`, `HOME`, Go-related vars, etc.) is kept; use **`exec_env_passthrough`** to allow additional variable names.

**`sandbox_mode`** adds OS-level or container isolation on top of scrubbing:

- **`off`** (default): scrubbed env + normal process execution.
- **`native`**: Linux uses Landlock + seccomp via a re-exec helper; macOS uses Seatbelt (`sandbox-exec`); Windows uses a Job Object (resource limits). Requires kernel/runtime support; `config` load fails if unavailable.
- **`container`**: runs commands in Docker or Podman (`--network=none`, workspace mounted at `/workspace`). Requires a container runtime on `PATH`.
- **`auto`**: tries `native`, then `container`, then falls back to scrub-only (a warning is printed when falling back).

Use **`-sandbox <mode>`** on the CLI to override `sandbox_mode` for that process.

**Optional integration tests** (real Docker/Podman) live under `internal/sandbox` with `//go:build integration`. Run with  
`CODIENT_INTEGRATION=1 go test -tags=integration ./internal/sandbox/...` when a container runtime is installed.

## Auto-check sequence

In **build** mode, after successful mutating tools (`write_file`, `str_replace`, etc.), codient runs **build → lint → test** using the resolved `autocheck_cmd`, `lint_cmd`, and `test_cmd` settings. Order is **fail-fast** (if build fails, lint and test are skipped). Each step uses the same timeout cap as the legacy single-command auto-check (bounded by `exec_timeout_sec`, max 60s per step). **Auto-detection** examples: **build** — same as before (`go build ./...`, `cargo check`, `npx tsc --noEmit`, …). **Lint** — `golangci-lint run ./...` when `go.mod` exists and `golangci-lint` is on `PATH`; `cargo clippy -- -D warnings` for Rust; `npm run lint` when `package.json` has a `lint` script; Python: `ruff check .` or `flake8` if that binary is on `PATH`. **Test** — `go test ./...`, `cargo test`, `npm test` when a `test` script exists, or `python -m pytest` when pytest markers are present. Plan-mode **verification** at the end of a plan uses the same resolved build, lint, and test commands.

## Token usage and cost estimates

Codient records **API-reported** token counts from chat completions when the server includes a `usage` object (OpenAI-compatible). Many local inference stacks omit this; cloud APIs usually populate it. Totals are **per REPL session** and include agent turns, `/compact`, the ask-mode verification gate, and **`delegate_task`** sub-agents.

- **`/cost`** (alias **`/tokens`**) — prompt, completion, and total tokens plus an estimated dollar amount when pricing is known.
- **`/status`** — session token totals and estimated cost when available.
- **Progress output** (`-progress` or default TTY stderr) — appends token counts to each completed model round when usage is present.
- **`-log` JSONL** — each `type: "llm"` line may include `prompt_tokens`, `completion_tokens`, and `total_tokens`.

**Pricing:** By default, codient matches your configured **`model`** id against a small built-in table (USD per million input/output tokens). Set **`cost_per_mtok`** in `config.json` to override, for example `{"input": 2.5, "output": 10}`, or in the REPL: **`/config cost_per_mtok 2.5 10`**. Use **`/config cost_per_mtok off`** to clear the override. Estimates are indicative only; use your provider’s billing for authoritative costs.

When `fetch_url` receives `Content-Type: text/html`, the body is converted to simplified markdown (headings, links, lists, code) before being returned.

## Environment variables

| Variable | Description |
|----------|-------------|
| `CODIENT_STATE_DIR` | Directory for `config.json` and related state instead of `~/.codient`. |

Run `codient -version` to print the binary version.

Test infrastructure variables (`CODIENT_INTEGRATION*`) are used by the test suite but are not user configuration.

For defaults and validation details, see [`internal/config/config.go`](../internal/config/config.go).
