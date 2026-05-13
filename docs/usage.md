# Usage

```bash
# Ping the server
codient -ping

# List models and tools
codient -list-models
codient -list-tools
codient -list-profiles
codient -profile frontier

# Start an interactive session (default when stdin is a TTY)
codient

# Just describe what you want — the Intent-Driven Orchestrator picks the right path
codient "How does the agent loop decide when to stop?"      # routed to ask
codient "Design a plugin system for tools"                  # routed to plan
codient "Fix the typo in README.md"                         # routed to build
codient "Refactor session.go to split executeTurn"          # plan, then build
codient -force "Refactor session.go to split executeTurn"   # plan, then build (no confirmation)

# One-shot prompt (no interactive session)
codient -prompt "Summarize README.md"

# Start a session with an initial prompt
codient -prompt "Help me understand the repo layout"

# Force a fresh session (skip resume)
codient -new-session

# Different workspace root
codient -workspace /path/to/repo

# Attach images (vision-capable chat models), single-shot or first REPL turn
codient -image ./screenshot.png -prompt "What error is this?"
codient -image a.png,b.png -prompt "Compare these mockups"
```

## Intent-Driven Orchestrator

Codient runs the orchestrator on **every** turn — there are no manual mode flags or slash commands. Each user turn classifies the prompt as one of four categories and routes it through the matching internal mode, tool registry, and reasoning tier:

| Category | Internal mode | Notes |
|----------|---------------|-------|
| **QUERY** | ask path (low tier) | Read-only Q&A, no write tools. |
| **DESIGN** | plan path (high tier) | Structured plan, no implementation. |
| **SIMPLE_FIX** | build path (low tier) | Goes straight to write tools. |
| **COMPLEX_TASK** | plan -> build | Plan first, then auto hand off into build (interactive prompt or **`-force`**; **`-print`** without **`-force`** stops at the plan). |

Classification has three tiers:

1. **Heuristic fast path** — a deterministic pattern matcher (`heuristicQuickClassify`) checks the prompt first. High-confidence patterns like "create a plan …", "please fix the typo on line 42", "refactor X across every file", or "what does Y do?" map directly to a category and skip the LLM entirely. The status line shows `codient: intent: SIMPLE_FIX (heuristic) — polite imperative: fix`. Disable with `disable_intent_heuristic: true` in [`config.json`](configuration.md) to consult the model on every turn.
2. **Supervisor LLM** — when the heuristic doesn't fire, a tiny JSON-only request to the **low** reasoning tier (`internal/intent.IdentifyIntent`) returns `{"category":"COMPLEX_TASK","reasoning":"multi-file refactor"}` and drives the routing. Configure tiers with **`low_reasoning_model`** / **`high_reasoning_model`** (and optional `_base_url` / `_api_key` siblings).
3. **Heuristic fallback** — when the supervisor LLM fails to produce parseable JSON, the same heuristic re-runs on the original prompt and either picks a confident category or defaults to `QUERY` (read-only, safe). The status line shows `(fallback)` with diagnostic per-attempt info.

The classification is always logged as `intent: <CATEGORY>` with an optional `(heuristic)` or `(fallback)` tag identifying which tier produced the decision. In `-print` you also get a one-line `intent:` notice on stderr before the turn runs.

There is no escape hatch: every REPL turn, every `-print` invocation, and every ACP **`session/prompt`** runs through the orchestrator. Use **`-force`** / **`-yes`** to auto-approve plan -> build hand-offs in non-interactive runs. Older configs that contained a top-level **`mode`** key (or per-mode **`models`** overrides) still load: codient logs a one-time deprecation notice, ignores the value, and uses the auto-only path.

## Split-screen TUI

When stdin is a TTY and `-plain` is **not** set, codient launches a Bubble Tea split-screen interface: a scrollable output viewport on top and a fixed **input panel** at the bottom. This keeps the user's typing completely separate from agent output — background events like the semantic index completion will never corrupt the input line.

| Area | Behaviour |
|------|-----------|
| **Viewport** (top) | Shows the full session, color-coded by speaker: **boxed user messages** (rounded outline with a **codient-blue** left accent — the leftmost stop of the welcome logo gradient), streamed assistant replies, and structured agent activity (intent lines, tool intents, per-tool results, round summaries) with **codient-purple** "● " bullets (the rightmost stop of the same gradient, matching the welcome banner's border). Slash commands (`/…`) stay as a simple echoed line without the box. All content is **word-wrapped** to the viewport width (with a hard-wrap fallback for very long tokens) so nothing overflows the terminal edge. |
| **Todo column** (right) | When the model uses **todo_write**, a narrow **Todo** panel lists tasks and statuses (hidden if the terminal is very narrow). Todos are saved in the session JSON and restored on resume. |
| **Status bar** | Displays "Agent is working…" during turns; a plain separator otherwise. |
| **Input panel** (bottom) | Codient-blue accent strip and a simple **`> `** prompt in the same hue (the orchestrator picks the internal mode behind the scenes — there is no manual mode label, and the blue matches the user-message bubbles above). A multi-line text area that wraps to the panel width and grows up to **8 visible rows** as you type (longer messages scroll inside the panel), a line with **model · backend**, then a right-aligned hint: **exact** prompt tokens and **context %** when the API returns `usage` and **`context_window`** is set; if the server omits usage, an **estimated** total (**`~`…**) from system + tools + history (same heuristic as `/compact`). **`—`** only when nothing can be computed. Press **Enter** to submit, **Ctrl+J** (or **Alt+Enter** / **Shift+Enter** on terminals that distinguish them) to insert a newline without submitting, **Ctrl+V** to paste a clipboard image (same as **`/paste`**; see [Images and vision](#images-and-vision)), **Ctrl+C** or **Escape** while the agent is working to **interrupt the current turn** (cancels the in-flight request and returns to the prompt), and **Ctrl+C** while idle to quit. |

**Assistant intent:** model pre-tool prose and streaming **reasoning** / **reasoning_content** deltas appear as **codient-purple ● intent lines** — the same agent accent used by the welcome banner border, regardless of which internal mode the orchestrator picked for the turn. **Ctrl+T** toggles a shorter extract when a block would be very long (compact vs fuller wrap).

**Editing the input:** the input panel is a true multi-line editor. **Up/Down** move the cursor between lines, **Home/End** jump to the start or end of the current line, and **Backspace/Delete**, **Ctrl+W** (delete word left), and **Ctrl+U** (delete to start of line) all work as in standard readline-style editors. Long lines word-wrap to the panel width — the box height grows automatically and shrinks back to one row after submission.

**Scrolling:** the conversation viewport scrolls with **Page Up/Page Down** (half a page), **Alt+Up/Alt+Down** (three lines), or the mouse wheel. (Plain **Up/Down** and **Home/End** now belong to the input editor; use the modifier or paging keys for transcript navigation.)

**Selecting and copying text:** the TUI captures mouse events so wheel scrolling works, which prevents native click-and-drag selection. In most modern terminals (xterm, GNOME Terminal, Konsole, alacritty, kitty, wezterm, iTerm2) **hold Shift** while dragging to bypass the capture; Windows Terminal uses **Alt+drag**. To copy the selection use **Ctrl+Shift+C** (not Ctrl+C — that interrupts or quits the agent and is intercepted by the TUI regardless of selection state). On macOS use **Cmd+C**. If your terminal doesn't honor Shift+drag, disable mouse capture entirely with **`-mouse=false`** (single run) or **`/config mouse_enabled false`** (persistent — see [configuration](configuration.md#config-reference-codientconfigjson)); with mouse disabled, normal click-and-drag selection and Ctrl+C copy work as usual. Keyboard scrolling is unaffected either way.

**Slash command autocomplete:** type **`/`** at the start of a line to open a dropdown of available commands. Navigate with **Up/Down** arrows, press **Enter** to select (inserts the command name followed by a space), or **Escape** to dismiss. The list filters as you type.

The TUI uses the alternate screen buffer; when you exit, the terminal returns to its previous state. Pass **`-plain`** or pipe stdin to fall back to the classic inline REPL.

## Interrupting a running turn

While the agent is working (making LLM calls or executing tools), you can cancel the current turn and return to the prompt without exiting codient:

| Mode | Interrupt | Effect |
|------|-----------|--------|
| **TUI** (default) | **Ctrl+C** or **Escape** | Cancels the current turn; prints "interrupted" and returns to the input panel. Press **Ctrl+C** when idle to quit. |
| **Plain REPL** (`-plain`) | **Ctrl+C** | Cancels the current turn; prints "interrupted" and returns to the prompt. Press **Ctrl+C** at the prompt to exit. |

Partial results from an interrupted turn (e.g. files already written by tools) are kept on disk but the assistant reply is discarded from conversation history.

## Headless / CI mode (`-print`)

Use **`-print`** (alias **`-p`**) for a **single non-interactive turn**: no REPL, no welcome banner, suitable for scripts and CI. This forces the same path as piping a prompt on stdin, but makes automation explicit. Combine with **`-prompt`** or stdin.

**Session persistence (same files as the REPL):** after each single-shot or **`-print`** turn, codient writes **`<workspace>/.codient/sessions/<id>.json`**. On the next invocation from the **same workspace**, if you do **not** pass **`-new-session`**, codient **loads the latest** saved session and continues the conversation before applying your new prompt. Use **`-session-id <id>`** to attach to a specific session file (the id is the filename stem; optional **`.json`** suffix is accepted). IDs must be a single path segment (letters, digits, `_`, `-`, `.` only). **`-new-session`** starts fresh and ignores **`-session-id`** (a warning is printed if both were set). If **`-session-id`** is set and the file is missing, codient exits with code **2**.

| Flag | Meaning |
|------|---------|
| **`-output-format text`** (default) | Assistant reply on stdout; errors on stderr |
| **`-output-format json`** | One JSON object on stdout with `reply`, `session_id`, `workspace`, `tools_used`, `files_modified`, optional `tokens` / `cost_usd`, `exit_reason`, and `error` on failure |
| **`-output-format stream-json`** | JSONL on stdout: same event shapes as **`-log`** (`llm`, `tool_start`, `tool_end`), plus a final `{"type":"result",...}` line (includes `session_id` and `workspace` when set) |
| **`-auto-approve off`** (default) | Same as today: non-interactive sessions deny exec/fetch prompts unless allowlisted |
| **`-auto-approve exec`** | Allow **run_command** / **run_shell** when not on the allowlist (no prompt) |
| **`-auto-approve fetch`** | Allow **fetch_url** to hosts not on the allowlist (no prompt) |
| **`-auto-approve all`** | Both exec and fetch |
| **`-max-turns N`** | Cap LLM rounds for this user turn (`0` = unlimited) |
| **`-max-cost USD`** | Stop when **estimated** session cost exceeds the limit (requires usage metadata and known pricing via **`cost_per_mtok`** or the built-in model table) |

**`-log`** still appends JSONL to a file. With **`-output-format stream-json`**, events are written to **stdout** and optionally duplicated to the log file if **`-log`** is set.

Examples:

```bash
codient -print -prompt "List top-level files"

codient -print -auto-approve all -output-format json -prompt "Run fmt" -max-turns 25

echo "Fix the typo in README" | codient -print -output-format json

# Force the plan -> build hand-off in -print so COMPLEX_TASK routes through to writes.
codient -print -force -auto-approve all -output-format json -prompt "Refactor session.go"
```

Chaining two CI steps on the same checkout (second run resumes the first):

```bash
codient -print -workspace "$REPO" -output-format json -auto-approve exec -prompt "Say hello" > out1.json
SID=$(jq -r .session_id < out1.json)
codient -print -workspace "$REPO" -session-id "$SID" -output-format json -auto-approve exec -prompt "Say goodbye"
```

For long-running HTTP integration, see **`-a2a`** below.

## Bring-your-own remote and background runs

Codient does not provide hosted infrastructure. You can still run it **in the cloud or in the background** on machines you control:

- **Detach on a server:** run the normal REPL inside **tmux**, **screen**, or a **systemd** user service so the process survives SSH disconnects.
- **CI / automation:** use **`-print`** (or a piped single-shot prompt) with **`-workspace`**, **`-auto-approve`**, and **`-output-format json`**; persist API keys via environment or CI secrets. Use **`-session-id`** or default resume to split work across jobs on the same workspace checkout.
- **Container:** run the binary in Docker or Podman with the repo mounted as a volume; set **`sandbox_mode`** to **`container`** if you want tool subprocesses isolated inside the container runtime.
- **Editor over SSH:** spawn **`codient -acp`** on the remote host and forward stdio (same NDJSON protocol as local Codient Unity or other ACP clients).

To back up session files to object storage, use **[lifecycle hooks](context-and-integrations.md#lifecycle-hooks)** (e.g. **`SessionEnd`**) to run `rclone` or `aws s3 sync` on **`<workspace>/.codient/sessions/`** after a run.

## File references (`@path`)

Type **`@path/to/file.go`** in your message to inline that file's contents directly in the prompt the model sees — no tool call round-trip needed.

- **Paths are relative to the workspace** root (or absolute). Quoted paths work for names with spaces: `@"my file.go"` or `@'my file.go'`.
- **`@image:path`** is still handled as a vision image (not a text file reference).
- **Escape with `\@`** to use a literal `@` that shouldn't be treated as a reference.
- **Per-file limit:** 256 KiB (same as `read_file`). **Aggregate limit:** 1 MiB across all `@` references in a single message.
- Binary / non-UTF-8 files are skipped with a warning.
- Directories are not supported; use a file path.

### Drag-and-drop

When you **drag files onto the terminal** (or paste paths from your file manager), codient auto-detects that the submitted text consists entirely of file paths and rewrites them with `@` prefixes. This works in both the TUI and plain REPL.

Example: dragging `main.go` and `util.go` into the terminal inserts their absolute paths; on submit, codient rewrites them to `@/full/path/main.go @/full/path/util.go` and loads both files as context.

## Images and vision

Use a **vision-capable** model (e.g. GPT-4o, Claude 3.5+, many local multimodal servers). Codient sends images as base64 **data URIs** in the standard OpenAI chat format (`image_url` parts).

- **CLI:** `-image path` or repeat `-image` for multiple paths. Comma-separated lists work (`-image a.png,b.png`). Applies to the **first user message** in a REPL session, or to a **single-shot** `-prompt` / stdin run. Combines with `-stream` (no tools).
- **REPL:** `/image path/to.png` queues an image for your **next** message (repeat to attach several). You can also embed paths in text: `@image:screenshot.png` or `@image:"C:\path\with spaces.png"` (paths are relative to the workspace when not absolute).
- **Clipboard:** `/paste` grabs an image from the OS clipboard and attaches it to your next message. In the TUI, **Ctrl+V** does the same thing. Both **raw image data** (e.g. a browser "Copy Image" or a screenshot tool that writes to the clipboard) **and a copied image file** (file managers like Nautilus, Dolphin, Finder, or Explorer place a file reference on the clipboard rather than the bytes) are accepted; for file references the original on-disk path is loaded without being copied. Requires a clipboard tool on Linux: **`wl-paste`** (Wayland) or **`xclip`** (X11). macOS and Windows use built-in APIs (`osascript` / PowerShell).
- **Limits:** PNG, JPEG, GIF, WebP; max **20 MiB** per file (warning above **5 MiB**). Large images still count toward context—use `/compact` if needed.

Use `-help` for all flags. Notable options:

- **`-force`** (alias **`-yes`** / **`-y`**) — auto-approve the plan -> build hand-off for COMPLEX_TASK turns (used by `-print` and other non-interactive runs)
- **`-workspace`** — workspace root (overrides config and cwd)
- **`-sandbox`** — subprocess isolation mode (`off`, `native`, `container`, `auto`; overrides config); see [Subprocess sandboxing](configuration.md#subprocess-sandboxing)
- **`-new-session`** — start fresh instead of resuming the latest session (REPL or single-shot / **`-print`**)
- **`-session-id`** — resume a specific persisted session under the workspace (single-shot / **`-print`**; see [Headless / CI mode](#headless--ci-mode--print))
- **`-update`** — check for a newer release and install it (see [Auto-update](context-and-integrations.md#auto-update))
- **`-repl`** — explicit REPL (default when stdin is a TTY)
- **`-system`** — optional extra system prompt merged into the default tool prompt
- **`-stream` / `-stream-reply`** — streaming behavior
- **`-plain`** — raw assistant text
- **`-progress`** — agent progress on stderr
- **Default error log** — unless disabled, codient appends **plain-text** timestamped lines (failed agent turns, orchestrator supervisor errors during intent classification, ACP `session/prompt` failures, and **panics with stack traces**) under `<state dir>/logs/errors-<UTC>-<pid>.log` (state dir is `~/.codient` or **`CODIENT_STATE_DIR`**). Disable with **`CODIENT_ERROR_LOG=0`** (also `false`, `off`, or `no`). Separate from **`-log`** JSONL below.
- **`-log`** — append JSONL events (LLM rounds, tools; each `llm` event may include `prompt_tokens`, `completion_tokens`, `total_tokens` when the server reports usage)
- **`-goal` / `-task-file`** — merged into the first user turn as a task directive
- **`-image`** — attach one or more image files to the first user turn (REPL) or to a one-shot prompt (`-stream` supported); see [Images and vision](#images-and-vision)
- **`-design-save-dir`** — where to save completed plans
- **`-a2a` / `-a2a-addr`** — run an [A2A](https://github.com/a2aproject/A2A) protocol server instead of the interactive CLI (default listen `:8080`)
- **`-acp`** — run as an [ACP](https://agentclientprotocol.com/) agent over stdio (JSON-RPC, NDJSON); stdout must contain only protocol lines; use with **`-plain`** and **`-progress`** (and **`-workspace`** when launched by an editor). Incompatible with **`-print`**, **`-repl`**, **`-stream`**, **`-a2a`**, etc.; see [ACP stdio](#acp-stdio-agent)
- **`-print` / `-p`** — headless single-turn mode for CI/scripts; see [Headless / CI mode](#headless--ci-mode--print)
- **`-output-format`** — with `-print`: `text`, `json`, or `stream-json`
- **`-auto-approve`** — with `-print`: `off`, `exec`, `fetch`, or `all`
- **`-max-turns`** / **`-max-cost`** — guardrails for `-print` (see [Headless / CI mode](#headless--ci-mode--print))

## A2A server

To expose codient as an Agent-to-Agent HTTP server:

```bash
codient -a2a -a2a-addr :8080
```

Use the same config (model, base URL, API key, workspace) as the CLI. See [`internal/a2aserver/`](../internal/a2aserver/) for protocol details.

## ACP stdio agent

Editors such as **Codient Unity** spawn `codient` as a subprocess and speak the [Agent Client Protocol](https://agentclientprotocol.com/) on stdin/stdout (one UTF-8 JSON object per line). Typical launch:

```bash
codient -plain -progress -acp -workspace /path/to/project
```

- Every ACP session is auto-mode: there is no manual mode flag and no **`session/set_mode`** RPC. The orchestrator is invoked once per **`session/prompt`** to pick the internal mode (ask / plan / build / plan -> build) for that turn.
- **`initialize`** result includes **`defaultChatModel`**: the trimmed chat model id from the agent’s effective config (same default the OpenAI client uses when **`session/new`** omits **`model`**). Editors can align a model picker with this when the user has no saved preference or a stale id.
- **`initialize` params `clientInfo.codientUnityPackageVersion`** (optional strict semver): when present, codient rejects the handshake if the Codient Unity package is **older** than the minimum this binary supports (JSON-RPC **`-32602`**). Older clients that omit the field keep working.
- **`session/new`** requires **`cwd`** to match the configured workspace (same rule as **`-workspace`**). Optional **`model`** selects a chat model id for that session (OpenAI-compatible API); omit it to use the configured default (same as the CLI). Any legacy **`mode`** field is accepted for backward compatibility but ignored — the result always echoes back **`"mode": "auto"`**.
- **`session/set_model`** updates **`model`** for an existing **`sessionId`** without clearing server-side conversation history. Omit **`model`** or send an empty string to revert that session to the configured default. Returns **`{"model": "<trimmed id>"}`** (empty string means default). Fails with **`session_busy`** while a **`session/prompt`** turn is in progress on that session.
  - **Preload (default on):** unless disabled, the agent runs a **minimal non-streaming chat completion** (not added to conversation history) so local inference servers load the model **before** the RPC returns. On failure, the session’s previous model is restored and the RPC fails with **`preload:`** in the error message. Disable per request with **`"preload": false`**, or set **`acp_preload_model_on_set_model`** to **`false`** in **`config.json`** to skip preloads for all switches.
  - **Progress:** while switching models, the agent may emit JSON-RPC notifications **`session/model_status`** with **`params`**: **`sessionId`**, **`phase`** (`unloading` \| `loading` \| `ready` \| `error`), and optional **`message`**. On Ollama-compatible hosts (OpenAI base URL ending in **`/v1`**, not known cloud APIs), **`unloading`** precedes a best-effort **`POST …/api/generate`** unload (`keep_alive: 0`) for the previous model before the new one is warmed. Clients may show these in UI chrome without treating them as transcript content.
- **`agent/list_models`** returns ids from **`GET …/models`** on the effective connection for the agent (top-level **`base_url`** plus any **`low_reasoning_model`** / **`high_reasoning_model`** sibling overrides in **`config.json`**) — same endpoint as **`codient -list-models`**.
- **`agent/list_profiles`** returns `{"active": "<name|empty>", "profiles": [...]}` — same data as **`codient -list-profiles`**.
- **`session/new`** accepts an optional **`"profile"`** parameter to select a named profile for that session (does not change the global `active_profile`). Unknown names fail with **`-32602`** invalid params.
- **`session/set_profile`** switches the active profile mid-session: `{sessionId, profile}`. Emits a **`session/profile_changed`** notification with `{sessionId, profile, resolvedModel}`. Fails with **`session_busy`** while a turn is in progress. Send an empty string to revert to top-level defaults.
- **`session/intent_identified`** is emitted on every **`session/prompt`** when the orchestrator classifies the user message. **`params`**: **`sessionId`**, **`category`** (`QUERY` \| `DESIGN` \| `SIMPLE_FIX` \| `COMPLEX_TASK`), **`reasoning`** (short string), **`fallback`** (boolean — `true` when the supervisor reply could not be parsed and codient defaulted to QUERY).
- **`session/mode_status`** is emitted whenever the resolved internal mode changes during a turn (orchestrator routing decision, plan -> build hand-off). **`params`**: **`sessionId`**, **`mode`** (resolved mode for the rest of the turn), **`phase`** (`changed` \| `plan_ready`), and an optional **`handoff`** boolean (set when an automatic plan -> build hand-off occurred).
- **`session/set_mode`** is no longer routed: codient responds with JSON-RPC **`-32601` "method not found"** so older clients can fall back gracefully.
- **stderr** may carry human-readable progress; **stdout** is reserved for ACP messages only.
- **`-max-turns`** and **`-max-cost`** apply per user prompt turn, like headless mode.
- **Codient Unity / Editor-shaped workspaces:** When **`-workspace`** looks like a Unity project (**`Assets/`** plus **`ProjectSettings/ProjectVersion.txt`**), the agent system prompt includes **Unity ACP** guidance. In **`-acp`**, the agent registers **`unity_*`** tools that issue JSON-RPC **`unity/...`** requests on stdout; the editor client (Codient Unity) runs them on the **Unity main thread** and returns results on stdin—same transport as **`session/request_permission`**. Read tools (hierarchy, assets, prefabs, console snapshot, package/asmdef summary) work in **ask** / **plan** / **build**; **`unity_apply_actions`** is **build mode only** and requires a **user confirmation** dialog in Unity before applying structured edits (scene objects and **`create_prefab`** for new **`Assets/.../*.prefab`** assets). API keys and chat payloads still follow your **`config.json`** / provider rules; nothing is sent to Unity except what the model requests through those tools.

Implementation: [`internal/acpserver/`](../internal/acpserver/) (transport) and [`internal/codientcli/acp_serve.go`](../internal/codientcli/acp_serve.go) (handlers).

## Slash commands

Inside a session you can use slash commands to control the agent:

| Command | Description |
|---------|-------------|
| `/edit-plan` (or `/ep`) | Open the active plan in `$EDITOR`/`$VISUAL` (fallbacks: `vi`, `notepad`). On save, codient re-parses the markdown into the structured `plan.json` and bumps the revision. |
| `/config [key] [value]` | View or set any configuration key (no args = show all, key = show one, key value = set and save). The reasoning-tier overrides live under `low_reasoning_*` and `high_reasoning_*` keys (e.g. `low_reasoning_model`, `high_reasoning_base_url`). |
| `/profile [subcommand]` | View, switch, save, or delete named config profiles. See [Named profiles](configuration.md#named-profiles). |
| `/setup` | Guided setup wizard for API connection, chat model selection, optional **low / high reasoning tier** model overrides used by the orchestrator, optional embedding model for semantic search, and optional save-as-profile |
| `/create-skill` | Guided wizard to author a **skill** (`SKILL.md` under user or workspace skills dirs); refreshes the in-session skill catalog |
| `/create-rule` | Guided wizard to author a **Cursor-style rule** (`.mdc` under **`.cursor/rules/`** in the workspace; same frontmatter as Cursor). Codient does not load these into the CLI system prompt—they apply in Cursor and compatible editors |
| `/skills` | List discovered skills (name, scope, `read_file` path) |
| `/compact` | Summarize conversation history to save context space |
| `/model <name>` | Switch to a different model (shortcut for `/config model`) |
| `/workspace <path>` | Change the workspace directory |
| `/tools` | List tools available to the current internal mode (orchestrator-resolved per turn) |
| `/hooks` | List configured lifecycle hooks (requires `hooks_enabled`) |
| `/mcp [server]` | List connected MCP servers and tool counts; with a server name, list that server's tools |
| `/lsp [server]` | List LSP servers and capabilities |
| `/status` | Show session state (orchestrator's resolved mode for the last turn, model, turns, estimated context, API token totals, resolved **auto-check** build/lint/test commands, exec policy) |
| `/cost` (or `/tokens`) | Show session token counts (prompt/completion/total) and estimated cost |
| `/log [path]` | Show **JSONL** telemetry status or enable append-only **`-log`**-style logging to a file (LLM/tool events). The default **error log** (failures and panics under `<state dir>/logs/`) is separate; use **`CODIENT_ERROR_LOG=0`** to disable it |
| `/undo` | Undo the last build turn. With **`git_auto_commit`** (default): removes the last codient commit (`HEAD~1`). Otherwise: restores tracked files and deletes new files from that turn. `/undo all` resets the repo to the commit at session start (auto-commit) or reverts all working-tree changes (legacy). Requires a git repo. |
| `/checkpoint` (or `/cp`) | Save a **named snapshot** of the conversation, mode, model, plan state, and current git `HEAD` (default name `turn-N`). With **`git_auto_commit`** in build mode, uncommitted changes are committed first so the snapshot points at a real commit. |
| `/checkpoints` (or `/cps`) | List checkpoints for this session as a tree (`*` marks the current checkpoint id). |
| `/rollback` (or `/rb`) | Restore conversation and (with **`git_auto_commit`**) reset the working tree to a checkpoint: pass **name**, **`cp_` id prefix**, or **turn number**. Stashes uncommitted work first when needed. |
| `/fork` | Roll back to a checkpoint, then create and checkout **`codient/<slug>`** for a new git line of work; sets a new **conversation branch** label for later checkpoints. Optional second argument is the branch slug. |
| `/branches` (or `/cbranch`) | List logical conversation branches (checkpoint fork labels) and their tips. |
| `/diff [path]` | Print a colored `git diff` vs `HEAD` (optional workspace-relative file). |
| `/branch [name]` | Show current branch, or switch to an existing branch, or create and checkout `name`. |
| `/pr [draft]` | Push `HEAD` to `origin` and open a GitHub pull request with **`gh`** (base branch = protected branch left behind, or `origin` default). Pass `draft` for a draft PR. |
| `/memory` (or `/mem`) | View, edit, or clear cross-session memory files. Subcommands: `show` (default), `edit [global\|workspace]`, `clear [global\|workspace]`, `reload`. |
| `/image <path>` | Attach an image file to your **next** message (vision models). Repeat to queue multiple images. |
| `/paste` | Attach an image from the OS clipboard to your **next** message (see [Images and vision](#images-and-vision)). In the TUI, **Ctrl+V** does the same thing. |
| `/new` (or `/n`) | Start a brand new session (fresh ID, history, and design namespace) |
| `/clear` | Reset conversation history (same session) |
| `/help` (or `/h`, `/?`) | Show available commands |
| `/exit` (or `/quit`, `/q`) | Quit the session |

## Git workflow (build mode)

In a git workspace, **build** mode can **auto-commit** each turn that changes files (`git_auto_commit`, default `true`). Each commit uses subject **`codient: turn N`** and a body copied from your user message (truncated to 200 characters). Configure **`git_protected_branches`** (default `main`, `master`, `develop`): if the first commit would land on one of those branches, codient creates and checks out **`codient/<task-slug>`** (with numeric suffixes if the name already exists) so you do not commit directly to e.g. `main`.

After a build turn that leaves **uncommitted** working-tree changes, stderr prints a **`git diff --stat`** summary vs `HEAD` and lists **untracked** files (up to 20), instead of a full unified diff.

Set **`git_auto_commit`** to **`false`** to restore the older behavior: no commits; `/undo` restores files from the working tree snapshot instead of removing the last commit.

The **`create_pull_request`** tool (build mode only) and the **`/pr`** slash command push the current branch to **`origin`** and run **`gh pr create`**. The PR base branch is the protected branch you left when codient created `codient/...`, when applicable; otherwise it follows **`origin/HEAD`** (usually `main`).

## Session persistence

Session state (conversation history, model, plan artifacts) is saved under `<workspace>/.codient/sessions/` after each turn. Starting codient again in the same workspace resumes the latest session. Use `-new-session` to start fresh. Older session files may include a top-level `mode` field; codient still loads them but ignores that field — every resumed session runs through the orchestrator just like a fresh one.

**Checkpoints** (named snapshots for rollback and branching) are stored under `<workspace>/.codient/checkpoints/<sessionID>/` (one JSON file per checkpoint plus a `tree.json` index). The session file records **`current_checkpoint_id`** and **`current_branch`** so resume keeps your place in the checkpoint tree.

The semantic search index (when **`embedding_model`** is set) and the repo map cache (**`repomap.gob`**) live under `<workspace>/.codient/index/` and are separate from chat sessions.

## Cross-session memory

Codient supports persistent memory that carries project conventions, user preferences, and past decisions across sessions. Memory is loaded into the system prompt at startup so the agent "remembers" what it learned previously.

**Two layers:**

| Scope | File | Purpose |
|-------|------|---------|
| **Global** | `~/.codient/memory.md` | User-wide preferences and conventions (applies to all projects) |
| **Workspace** | `<workspace>/.codient/memory.md` | Project-specific conventions, architecture decisions, patterns |

Both files are Markdown. Global memory is loaded first, workspace memory second, so project-specific notes can override global ones. Each file is capped at 16 KiB to avoid bloating the system prompt.

**How it works:**

- **Automatic:** In build mode, the agent has a `memory_update` tool. It can proactively record conventions it discovers (build commands, naming patterns, architecture decisions) and user preferences it learns (style, verbosity, workflow).
- **Manual:** Use the `/memory` slash command to view (`/memory show`), edit in `$EDITOR` (`/memory edit workspace`), or clear (`/memory clear global`) memory files. `/memory reload` re-reads files after external edits.
- **Tool actions:** `memory_update` supports `append` (add to end) and `replace_section` (update a `## Heading` section in-place, or create it if missing).

**Repository instruction files** are also loaded into the system prompt alongside memory:

| File | Description |
|------|-------------|
| `AGENTS.md` | Workspace-root conventions file (compatible with common agent tooling) |
| `.codient/instructions.md` | Codient-specific project instructions |

These are read-only from the agent's perspective (capped at 32 KiB total) and complement the read-write memory files.

## Agent skills

**Skills** are optional folders containing **`skill.yaml`** or **`SKILL.md`**. They behave like Cursor-style agent skills: a short **Agent skills** section is injected into the system prompt listing each skill’s name, description, and the path to pass to **`read_file`**. When a task matches a skill, the model should read that file before following it.

Modern skills use **`skill.yaml`** to define metadata and optional **MCP servers** (Model Context Protocol). Legacy skills use **`SKILL.md`** with YAML frontmatter (`name`, `description`, and optionally `disable-model-invocation`) followed by markdown instructions.

| Scope | Location |
|-------|----------|
| **User (global)** | `<state-dir>/skills/<skill-id>/` |
| **Workspace** | `<workspace>/.codient/skills/<skill-id>/` |

If the same **`name`** appears in both places, the **workspace** skill wins. The catalog is capped in size; very large lists may show `[truncated]`.

### Managing skills

Use the **`codient skill`** CLI command to manage user-wide skills:

```bash
# List all discovered user and workspace skills
codient skill list

# Install a skill from a local path or git URL
codient skill install https://github.com/user/my-skill
codient skill install ./path/to/skill

# Remove an installed user skill
codient skill remove my-skill
```

**MCP Servers:** If a skill contains an `mcp` section in its `skill.yaml`, codient automatically connects to that MCP server when the skill is discovered. This allows skills to provide their own custom tools.

```yaml
# skill.yaml example
name: github-helper
description: Tools for interacting with GitHub
instructions: instructions.md
mcp:
  command: npx
  args: ["-y", "@modelcontextprotocol/server-github"]
  env:
    GITHUB_PERSONAL_ACCESS_TOKEN: "your-token"
```

**`read_file`:** Paths under the workspace work as usual. Paths for **user** skills (shown in the catalog) are resolved under `<state-dir>/skills/` when the file is not found under the workspace—so global skills remain readable without widening access to arbitrary files outside the workspace.

**REPL:** **`/create-skill`** walks you through scope, folder id, description, and optional `disable-model-invocation`, then writes **`SKILL.md`**. **`/skills`** prints what codient discovered on disk.

**Cursor rules:** **`/create-rule`** writes **`.cursor/rules/<stem>.mdc`** with YAML frontmatter (`description`, **`alwaysApply`**, optional **`globs`**) like Cursor’s rule editor. The codient CLI does not inject these files into its system prompt; use **`AGENTS.md`**, **`.codient/instructions.md`**, or **skills** for codient-native guidance.

**ACP / Unity:** Slash commands run only in the interactive CLI REPL; editor sessions still receive the **Agent skills** section in the system prompt when codient starts.

## Plan path and saved plans

When the orchestrator routes a turn to the plan path (`DESIGN` or `COMPLEX_TASK`), and the assistant's reply includes a **Ready to implement** section, codient saves the markdown under the workspace (by default `.codient/plans/<sessionID>/`). Plans are scoped to the session that created them. Filenames are `{task-slug}_{date-time}_{nanoseconds}.md` so runs never collide. The task slug comes from `-goal`, else `-task-file` basename, else the first line of your first message. For `COMPLEX_TASK`, codient automatically transitions into the build path immediately after the plan reply (subject to the confirmation rules below) so the next user message implements the plan.

### Plan -> build hand-off

Codient never shows an `[a/r/e/c]` approval menu — the orchestrator decides when to hand off based on the plan structure and the run mode.

- **REPL (interactive):** when a plan-path reply ends with **Ready to implement**, codient prompts you once before transitioning into the build path; press Enter or answer "yes" and the **next** message implements the plan. Pass **`-force`** / **`-yes`** at start-up to skip that prompt for every COMPLEX_TASK turn.
- **Headless `-print`:** by default the run stops at the plan so CI can review or persist it. Pass **`-force`** to auto-approve the hand-off and let the same invocation continue into build. Resuming with **`-session-id <sid>`** re-enters auto mode, and the next prompt is routed by the orchestrator just like a fresh turn (it will see the saved plan in history and continue implementation if appropriate).
- **ACP / Codient Unity:** the orchestrator runs on every **`session/prompt`**. When it hands off plan -> build mid-turn, codient emits a **`session/mode_status`** notification with **`mode: "build"`**, **`phase: "changed"`**, and **`handoff: true`** so the editor can reflect the transition in its UI. There is no **`session/set_mode`** RPC — the editor never has to drive the transition manually.

The injected hand-off directive includes:

- A clear "this session is already in build mode" line plus the available tool list for the build path.
- Explicit instructions: "do not ask", "the user already confirmed", "start implementing now using tools".
- A "verify each step's premise" reminder so the build agent reads the files the plan referenced before editing.
- The structured plan (Implementation steps / Files to modify / Verification) when codient parsed one — otherwise the original plan markdown with any "run codient" / "switch modes" lines stripped (these are obsolete in the auto-only world).

The build path prompt has a companion bullet that tells the model: when an approved plan or "Ready to implement" design is in context, treat the listed scope as user-approved and start using write tools (`write_file`, `str_replace`, `patch_file`, `insert_lines`) on the first turn rather than launching another research pass.

### Editing the active plan

Use **`/edit-plan`** (alias **`/ep`**) to open the active plan in **`$EDITOR`** (or **`$VISUAL`**, falling back to **`vi`** / **`notepad`**). On save, codient re-parses the markdown back into the structured plan and bumps the revision number. The structured artifact under **`.codient/plans/<sessionID>/plan.json`** is updated in place.

### Chained CI

```bash
codient -print -force -prompt "design and implement feature X" > out.json
SID=$(jq -r .session_id < out.json)
codient -print -force -session-id "$SID" -prompt "now add tests for feature X"
```

A `COMPLEX_TASK` turn under **`-force`** plans **and** implements in one invocation; the resume picks up the saved plan and continues in the same auto-orchestrated way.

### Parallel plan generation (Tree of Thoughts)

On selected plan-path turns, codient runs **three** full read-only agent passes in parallel, each with a small extra system emphasis (performance, readability/maintainability, idiomatic Go architecture), then a **Senior Principal Engineer**–style evaluator completion picks the best draft and returns that text (plus a two-sentence justification). The heuristic matches the **first** plan-path user message in a session and the first user message **after** a blocking plan **Question** (a reply that ends with **Waiting for your answer**). Further back-and-forth in the plan path uses the usual single agent path. If any branch or the evaluator fails, codient **falls back** to one normal plan-path turn.

- Set **`plan_tot`** to **`false`** in `~/.codient/config.json` (or run **`/config plan_tot false`** in the REPL) to disable this pipeline entirely.
- Your global **`max_concurrent`** still applies to normal turns; the ToT fan-out uses a dedicated client with at least **four** concurrent in-flight requests so three branches can overlap with the evaluator. Raising **`max_concurrent`** in `~/.codient/config.json` is still recommended if you run other parallel work against the same server.

## Sub-agents (task delegation)

The agent has a **`delegate_task`** tool that spawns an isolated sub-agent to handle a self-contained task. This is always available — the model decides when delegation is useful (e.g. parallelizing codebase exploration across multiple areas).

**How it works:**

- The parent agent calls `delegate_task` with a **mode** (`build`, `ask`, or `plan`), a **task** description, and optional **context** snippets.
- A fresh `agent.Runner` is created for the sub-agent with its own conversation history, tool registry matching the requested mode, and a model from the appropriate **reasoning tier** (low for `build` / `ask`, high for `plan`). Override **`low_reasoning_model`** / **`high_reasoning_model`** (and optional `_base_url` / `_api_key` siblings) in [config](configuration.md#config-file-reference-codientconfigjson) to route them to different backends.
- The sub-agent runs to completion and its reply is returned to the parent as the tool result.
- Sub-agents cannot spawn further sub-agents (recursion guard).

**Mode restrictions (privilege escalation prevention):**

`delegate_task` is the **only** place where the `build` / `ask` / `plan` names survive in user-visible code: they let the orchestrator-driven parent restrict what a sub-agent is allowed to do.

| Parent path (orchestrator-resolved) | Allowed sub-agent modes |
|-------------------------------------|------------------------|
| **build** (SIMPLE_FIX or post-handoff COMPLEX_TASK) | `build`, `ask`, `plan` |
| **ask** (QUERY) | `ask` only |
| **plan** (DESIGN or pre-handoff COMPLEX_TASK) | `ask` only |

Read-only parent paths (ask, plan) can only delegate to read-only sub-agents, preventing a plan/ask turn from gaining write access through delegation.

**Reasoning-tier routing:** sub-agents in `build` / `ask` modes use the **low** tier model; `plan` sub-agents use the **high** tier model. Configure them with **`low_reasoning_model`** / **`high_reasoning_model`** in config to route, for example, build-mode edits at a fast local model and design work at a remote frontier model.

**Git worktree isolation (optional):** Set **`delegate_git_worktrees`** to **`true`** in [config](configuration.md#config-file-reference-codientconfigjson) (or **`/config delegate_git_worktrees true`**) so each **`delegate_task`** runs with its tools rooted in a **separate detached worktree** at the same commit as **`HEAD`**, under **`~/.codient/delegate-worktrees/`**. Use this when parallel **`delegate_task`** calls should not share the working tree (e.g. concurrent **build**-mode edits). Changes in that worktree are **discarded** when the sub-agent finishes — they are **not** applied to your main checkout. **`HEAD`** only: uncommitted edits in the parent workspace are **not** included. While this is enabled, delegated runs skip the **`repo_map`** tool (isolation MVP).

## Streaming

With **styled output** (default when **`-plain`** is off), assistant replies are **not** streamed token-by-token to stdout on interactive terminals: the full reply is rendered once at end of turn with **GitHub-flavored markdown** (headings, lists, code blocks, tables) via [glamour](https://github.com/charmbracelet/glamour). That avoids raw `**bold**` and fences in the transcript while the model is still generating.

When stdout is **not** a TTY (pipe, redirect, or some IDE integrations), codient still renders markdown, using glamours **no-TTY** layout (structured text without relying on a character device).

Use **`-plain`** if you want **live** assistant tokens as they arrive as raw text with no markdown layout.

**`-stream-reply`** (default on for TTYs) still applies to **plain** sessions: when plain and streaming is enabled, assistant text is written to stdout as it streams.

Plan-path turns with a blocking **Question** already buffered the next turn so the full design could be markdown-rendered; non-blocking turns now use the same “render once” path whenever styled output is on.
