# Configuration

Most settings are stored in `~/.codient/config.json` (unless `CODIENT_STATE_DIR` points elsewhere) and managed via `/config` and `/setup` inside a session. A few environment variables override defaults (see below); everything else is config file or CLI flags.

## First run

Start codient and set your model (and optionally base URL / API key):

```
codient
/config model gpt-4o-mini
/config base_url http://127.0.0.1:13305/v1
/config api_key your-key-here
```

Or run `/setup` for a guided wizard (connection and model selection).

## Config file reference (`~/.codient/config.json`)

All settings live in a single JSON file. Use `/config` to view and edit, or edit the file directly. Omitted fields use built-in defaults. CLI flags (e.g. `-plain`, `-workspace`, `-force`) override config file values when explicitly passed.

**Example config.json:**

```json
{
  "base_url": "http://127.0.0.1:13305/v1",
  "api_key": "codient",
  "model": "qwen3-coder",
  "search_url": "http://localhost:8888",
  "fetch_allow_hosts": "docs.go.dev,pkg.go.dev",
  "autocheck_cmd": "go build ./...",
  "lint_cmd": "golangci-lint run ./...",
  "test_cmd": "go test ./...",
  "verbose": true
}
```

Omit **`lint_cmd`** and **`test_cmd`** to use [auto-detection](#auto-check-sequence) (same as empty string). Set either to **`off`** to skip that step in the post-edit sequence.

**Reasoning tiers (Intent-Driven Orchestrator)** — There is only one mode (auto). The orchestrator picks one of two reasoning tiers per turn:

- **Low** — supervisor classifier, **QUERY** (ask path), and **SIMPLE_FIX** (build path).
- **High** — **DESIGN** (plan path) and **COMPLEX_TASK** planning before the auto plan -> build hand-off.

Configure each tier with **`low_reasoning_model`** and **`high_reasoning_model`** (and optional `_base_url` / `_api_key` siblings):

```json
{
  "base_url": "http://127.0.0.1:13305/v1",
  "api_key": "codient",
  "model": "qwen3-coder-30b",
  "low_reasoning_model": "qwen3-coder-7b",
  "high_reasoning_model": "qwen3-coder-30b",
  "high_reasoning_base_url": "https://api.openai.com/v1",
  "high_reasoning_api_key": "sk-..."
}
```

With no overrides, both tiers fall back to the top-level `base_url` / `api_key` / `model`. The `/setup` wizard walks through both tiers after you pick the default chat model.

**Backward compatibility:** older configs that contained a top-level **`mode`** key (or a per-mode **`models`** override map for `build` / `ask` / `plan`) still load. Codient logs a one-time deprecation notice on startup, ignores those values, and uses the auto-only path. Remove them from `config.json` to silence the warning.

## Named profiles

Bundle a set of settings under a name and switch with one flag, env var, or slash command. Profiles are sparse: only the keys you specify override the top-level defaults. Existing configs without profiles keep working unchanged.

**Resolution order:**

1. Built-in defaults.
2. Top-level keys in `config.json` (existing behavior).
3. Selected profile's overrides (new layer).
4. CLI flags (`-plain`, `-workspace`, etc. — existing `flag.Visit` path).

**Profile selection precedence:** `-profile <name>` flag > `CODIENT_PROFILE` env var > `active_profile` field in `config.json`. Unknown names from the flag or env are hard errors (exit 2). An `active_profile` pointing at a missing name warns once and falls back to top-level keys.

**Example config.json with profiles:**

```json
{
  "base_url": "http://127.0.0.1:13305/v1",
  "api_key": "codient",
  "model": "qwen3-coder-30b",
  "active_profile": "local",
  "profiles": {
    "local": {
      "low_reasoning_model": "qwen3-coder-7b",
      "high_reasoning_model": "qwen3-coder-30b",
      "lint_cmd": "off",
      "test_cmd": "off"
    },
    "frontier": {
      "base_url": "https://api.openai.com/v1",
      "api_key": "sk-...",
      "model": "gpt-5-codex",
      "high_reasoning_model": "gpt-5-pro",
      "low_reasoning_model": "gpt-5-mini",
      "plan_tot": true
    },
    "ci-strict": {
      "autocheck_cmd": "go build ./...",
      "lint_cmd": "golangci-lint run ./...",
      "test_cmd": "go test -race ./...",
      "sandbox_mode": "container",
      "exec_allowlist": "go,git",
      "git_auto_commit": false
    }
  }
}
```

**Overridable keys:** `base_url`, `api_key`, `model`, all `low_reasoning_*` / `high_reasoning_*` / `embedding_*` keys, `autocheck_cmd`, `lint_cmd`, `test_cmd`, `autocheck_fix_max_retries`, `autocheck_fix_stop_on_no_progress`, `sandbox_mode`, `sandbox_ro_paths`, `sandbox_container_image`, `exec_allowlist`, `exec_env_passthrough`, `exec_timeout_sec`, `exec_max_output_bytes`, `max_concurrent`, all `fetch_*` keys, `search_max_results`, `git_auto_commit`, `git_protected_branches`, `plan_tot`, `cost_per_mtok`, `context_window`, `context_reserve`, `max_llm_retries`, `stream_with_tools`, `max_completion_seconds`, `autocompact_threshold`, `plain`, `quiet`, `verbose`, `mouse_enabled`, `progress`, `stream_reply`.

**Intentionally excluded:** `workspace`, `mcp_servers`, `delegate_sandbox_profiles`, `delegate_sandbox_default`, `delegate_git_worktrees`, `hooks_enabled`, `update_notify`, `log`, `design_save_dir`, `design_save`, `project_context`, `ast_grep`, `repo_map_tokens`, `checkpoint_auto`, `acp_preload_model_on_set_model`. These are per-machine wiring rather than per-workflow choices.

**Profile names** must match `[a-z0-9_-]+`.

### CLI

```
codient -profile frontier          # select a profile at startup
codient -list-profiles             # list configured profiles and exit
CODIENT_PROFILE=ci-strict codient -p -prompt "…"  # env var selection
```

### `/profile` slash command

| Form | Behaviour |
|------|-----------|
| `/profile` | Show active profile and available names |
| `/profile list` | Same as bare `/profile` |
| `/profile <name>` | Mid-session swap: re-merge from saved config, rebuild client + registry + system prompt |
| `/profile default` | Revert to top-level-only config (clears `active_profile`) |
| `/profile diff <name>` | Print keys that would change vs the current effective config |
| `/profile show [name]` | Print profile overrides (defaults to active) |
| `/profile save <name>` | Save the current session config as a sparse delta; refuses overwrite without `--force` |
| `/profile delete <name>` | Remove a profile; refuses if active unless `--force` |

### ACP (editor integration)

- **`initialize` response** includes `"profiles": true` in `agentCapabilities`, plus `activeProfile` and `profiles` list.
- **`agent/list_profiles`** returns `{active, profiles: [{name, model}]}`.
- **`session/new`** accepts an optional `"profile"` param.
- **`session/set_profile`** `{sessionId, profile}` → applies mid-session, emits `session/profile_changed` notification.

### `/setup` wizard

At the end of `/setup`, an optional prompt lets you save the current configuration as a named profile.

## `/config` reference

Run `/config` with no arguments to see all current values. `/config <key>` shows one value. `/config <key> <value>` sets and persists.

| Key | Description | Default |
|-----|-------------|---------|
| **Connection** | | |
| `base_url` | API base URL including `/v1` | `http://127.0.0.1:13305/v1` |
| `api_key` | Sent as `Authorization` bearer token | `codient` |
| `model` | Default model id (used by tiers that have no override) | *(none — must be set for typical use)* |
| `low_reasoning_model` | Model id used by the orchestrator supervisor, **QUERY** (ask path), and **SIMPLE_FIX** (build path) turns | inherit `model` |
| `low_reasoning_base_url`, `low_reasoning_api_key` | Connection overrides for the low-reasoning tier | inherit `base_url` / `api_key` |
| `high_reasoning_model` | Model id used for **DESIGN** (plan path) turns and **COMPLEX_TASK** planning | inherit `model` |
| `high_reasoning_base_url`, `high_reasoning_api_key` | Connection overrides for the high-reasoning tier | inherit `base_url` / `api_key` |
| **Defaults** | | |
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
| `autocheck_fix_max_retries` | Maximum fix-loop iterations after an auto-check failure within a single turn. `0` = single-shot (no retry loop, today's default). `> 0` = the runner re-runs the failing step after the model edits and stops when the step passes, the cap is reached, or no progress is detected. | `0` |
| `autocheck_fix_stop_on_no_progress` | When the fix loop is active, abort early if the failure signature is identical between consecutive attempts. | `true` |
| **Git (build path)** | | |
| `git_auto_commit` | After each build-path turn that changes files, commit with message `codient: turn N` (set `false` for legacy file-restore `/undo` without commits) | `true` |
| `delegate_git_worktrees` | When **`true`**, each **`delegate_task`** sub-agent runs in a **detached git worktree** at **`HEAD`** under `~/.codient/delegate-worktrees/` (requires a git workspace and `git` on `PATH`). Filesystem edits there are **not merged** into the parent workspace. Uncommitted changes in the main tree are **not** visible in the worktree. Sub-agents omit the **`repo_map`** tool for this path (MVP). | `false` |
| `delegate_sandbox_profiles` | Map of named sandbox profiles for `delegate_task` sub-agents. Each profile can specify `image`, `network_policy` (`none`/`bridge`/`host`), `max_memory_mb`, `max_cpu_percent`, `max_processes`, `read_only_paths`, `env_passthrough`, and `long_lived` (keep one container per delegate lifetime). The model can only pick among admin-defined names, never define new ones. See [Delegate sandbox profiles](#delegate-sandbox-profiles). | *(empty)* |
| `delegate_sandbox_default` | Profile name from `delegate_sandbox_profiles` applied when `delegate_task` omits `sandbox_profile`. Empty = use global `sandbox_mode`. | *(empty)* |
| `git_protected_branches` | Comma-separated branch names; when the first change lands on one of these, codient creates `codient/<task-slug>` and commits there | `main,master,develop` |
| `checkpoint_auto` | Automatic checkpoints: **`plan`** (after each completed plan phase group), **`all`** (after each build turn that changes files and commits), **`off`** (manual `/checkpoint` only) | `plan` |
| **UI/Output** | | |
| `plain` | Raw assistant text (skip markdown rendering; use for streaming tokens or logs without glamour) | `false` |
| `quiet` | Suppress the welcome banner | `false` |
| `verbose` | Extra session diagnostics | `false` |
| `mouse_enabled` | TUI mouse capture: enables wheel scrolling but prevents native click-and-drag text selection. Set **`false`** (or pass **`-mouse=false`**) when you need to copy text out of the chat window on a terminal that doesn't honor Shift+drag. Keyboard scrolling (**Page Up/Down**, **Alt+Up/Down**) continues to work either way | `true` |
| `log` | Default JSONL log path | *(empty)* |
| `stream_reply` | When **`plain`** is on: stream assistant tokens to stdout as they arrive. Styled (non-plain) sessions render the full reply with markdown once per turn instead of streaming raw tokens | `true` |
| `progress` | Force progress output on stderr | `false` |
| `acp_preload_model_on_set_model` | When **`true`** (default), ACP **`session/set_model`** runs a minimal chat completion so local inference servers load the model before the RPC returns; set **`false`** to skip (saves one completion per switch) | `true` |
| **Plan** | | |
| `design_save_dir` | Override directory for saved plans | `<workspace>/.codient/plans` |
| `design_save` | Save plan-path plans to disk | `true` |
| `plan_tot` | Parallel Tree-of-Thoughts plan generation on selected plan-path turns (see [usage](usage.md#parallel-plan-generation-tree-of-thoughts)) | `true` |
| **Project** | | |
| `project_context` | `off` to skip auto-injected project hints | *(empty)* |
| **Tools** | | |
| `ast_grep` | ast-grep binary path: `auto` (default), explicit path, or `off` to disable | *(auto)* |
| `embedding_model` | Model id for `/v1/embeddings`. Enables the `semantic_search` tool; leave empty to disable. The welcome banner shows **Embeddings** (model id, or `off`) | *(empty)* |
| `embedding_base_url` | Optional base URL for `/v1/embeddings` when chat targets a server that does not implement embeddings (e.g. Anthropic / Claude). Leave empty to inherit `base_url` | *(inherit `base_url`)* |
| `embedding_api_key` | API key for `embedding_base_url`. Only used when `embedding_base_url` is also set; otherwise the chat `api_key` is reused | *(inherit `api_key`)* |
| `repo_map_tokens` | Approximate token budget for the **structural repository map** injected into the system prompt and for the `repo_map` tool. **`0`** (default) picks a budget from workspace size; **`-1`** disables the map and the tool | `0` |
| `hooks_enabled` | Enable [lifecycle hooks](context-and-integrations.md#lifecycle-hooks) (`hooks.json` under `~/.codient` and `<workspace>/.codient`) | `false` |
| `cost_per_mtok` | Optional `{"input":N,"output":N}` USD per 1M tokens — overrides built-in pricing for `/cost` and session cost estimates | *(built-in table)* |
| **Update** | | |
| `update_notify` | Show interactive update prompt on REPL startup | `true` |
| **MCP** | | |
| `mcp_servers` | Map of MCP server IDs to connection configs (see [MCP servers](context-and-integrations.md#mcp-model-context-protocol-servers)) | *(empty)* |

### Local models and tool calling

When tools are enabled and **`stream_with_tools`** is **`false`** (default), codient uses non-streaming completions so local OpenAI-compatible servers are less likely to drop **`tool_calls`** over SSE. If the model returns recognized intent-only prose once without **`tool_calls`** or XML `<tool_call>` markup, codient injects a single follow-up user message (stderr progress shows `requesting tool calls…`) so the next completion can emit real tool calls.

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

## Delegate sandbox profiles

When `delegate_sandbox_profiles` is set, the model can pass `sandbox_profile` to `delegate_task` to select a named profile. Each profile overrides the global `sandbox_mode` for that sub-agent's `run_command` invocations.

Example config:

```json
{
  "delegate_sandbox_profiles": {
    "go-build": {
      "image": "golang:1.22",
      "long_lived": true,
      "max_memory_mb": 1024,
      "max_cpu_percent": 50
    },
    "node": {
      "image": "node:20",
      "network_policy": "bridge"
    }
  },
  "delegate_sandbox_default": "go-build"
}
```

Profile fields:

| Field | Description | Default |
|-------|-------------|---------|
| `image` | OCI image for the container | `alpine:3.20` |
| `network_policy` | Container network: `none`, `bridge`, `host` | `none` |
| `max_memory_mb` | Memory limit in MiB | *(unlimited)* |
| `max_cpu_percent` | CPU cap as a percentage (1–100) | *(unlimited)* |
| `max_processes` | PID limit | *(unlimited)* |
| `read_only_paths` | Extra host paths granted read-only | *(empty)* |
| `env_passthrough` | Extra environment variable names forwarded | *(empty)* |
| `long_lived` | Keep a single container running for the delegate's lifetime (`docker run -d` + `docker exec` per command) instead of a fresh container per `run_command`. Preserves build caches and intermediate files across commands. | `false` |

Profile names must match `[a-z0-9_-]+`. When `delegate_sandbox_default` is set, delegates that omit `sandbox_profile` use that profile. Otherwise they fall back to the global `sandbox_mode`.

The `long_lived` flag only takes effect when the delegate is in **build** mode. Read-only modes (`ask`, `plan`) don't execute `run_command`.

LLM calls (`fetch_url`, `web_search`) stay in the parent process and never enter the container.

## Auto-check sequence

When the orchestrator routes a turn into the **build** path (SIMPLE_FIX or post-handoff COMPLEX_TASK), after successful mutating tools (`write_file`, `str_replace`, etc.) codient runs **build → lint → test** using the resolved `autocheck_cmd`, `lint_cmd`, and `test_cmd` settings. Order is **fail-fast** (if build fails, lint and test are skipped). Each step uses the same timeout cap as the legacy single-command auto-check (bounded by `exec_timeout_sec`, max 60s per step). **Auto-detection** examples: **build** — same as before (`go build ./...`, `cargo check`, `npx tsc --noEmit`, …). **Lint** — `golangci-lint run ./...` when `go.mod` exists and `golangci-lint` is on `PATH`; `cargo clippy -- -D warnings` for Rust; `npm run lint` when `package.json` has a `lint` script; Python: `ruff check .` or `flake8` if that binary is on `PATH`. **Test** — `go test ./...`, `cargo test`, `npm test` when a `test` script exists, or `python -m pytest` when pytest markers are present. Plan-path **verification** at the end of a plan uses the same resolved build, lint, and test commands.

On failure, combined stdout and stderr from the failing step are injected as a user message so the model can fix issues before finishing the turn. Output is truncated by **`exec_max_output_bytes`**.

### Fix loop

By default (`autocheck_fix_max_retries = 0`) auto-check is single-shot: the failure is injected once and the multi-turn agent loop handles convergence. Setting `autocheck_fix_max_retries` to a positive integer enables an explicit **fix loop** within each user turn:

1. After a mutating tool batch triggers auto-check and a step fails, the runner counts the attempt and injects the failure body with an `[auto-check fix attempt N/M]` suffix.
2. The model edits files → the next tool batch triggers auto-check again.
3. If the checks pass, the loop state resets and the turn continues normally.
4. If `autocheck_fix_stop_on_no_progress` is `true` (default) and the **failure signature** (a stable hash of the parsed output) is identical to the previous attempt, the runner stops the loop and tells the model to report what is still failing and stop editing.
5. If the attempt count reaches the cap, the runner emits a `max retries exhausted` notice and stops the loop.

The fix loop runs in every Runner that has `AutoCheck` attached: the REPL, `-print`, ACP, and delegated sub-agents. Only build-path turns are affected (plan and ask paths never attach `AutoCheck`).

**Failure parsing:** Auto-check output is parsed by a language-aware parser registry. A Go-test parser extracts `--- FAIL: TestName`, file:line locations, and `FAIL\tpkg` lines; other languages use an opaque default (SHA-256 hash of the output body).

```json
{
  "autocheck_fix_max_retries": 3,
  "autocheck_fix_stop_on_no_progress": true
}
```

### Unity projects (ACP / C#)

For [Codient Unity](https://github.com/magusoftdev/codient-unity), set the agent **workspace** to the **Unity project root** (the folder that contains `Assets` and `ProjectSettings`), same as the Unity package uses for `codient -acp`.

- **Auto-detect:** If `ProjectSettings/ProjectVersion.txt` and an `Assets` directory exist and there is at least one `*.sln` in the project root, the default build step is `dotnet build "<name>.sln" -v minimal`. If several `.sln` files exist, codient prefers `<folderName>.sln` when it matches the project directory name; otherwise it picks the lexicographically first file. **`dotnet`** must be on `PATH`. Unity usually creates the solution after you open the project in the Editor once (or via your normal project setup).
- **No `.sln` yet:** Auto-detect leaves `autocheck_cmd` empty—set **`autocheck_cmd`** yourself (for example after generating the solution), or use a **batchmode** compile entry point your team already uses:

```json
{
  "autocheck_cmd": "\"/path/to/Unity\" -batchmode -nographics -quit -projectPath \"/path/to/MyGame\" -logFile -"
}
```

Adjust flags and add **`-executeMethod`** (or similar) if your project compiles via a custom Editor script. Keep commands within the **60s per step** budget or raise **`exec_timeout_sec`** (still capped at 60s for auto-check steps).

## Token usage and cost estimates

Codient records **API-reported** token counts from chat completions when the server includes a `usage` object (OpenAI-compatible). Many local inference stacks omit this; cloud APIs usually populate it. Totals are **per REPL session** and include agent turns, the orchestrator supervisor call, `/compact`, the ask-path verification gate, and **`delegate_task`** sub-agents.

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
