# Context and integrations

## Web search

The `web_search` tool is always enabled. It uses an embedded metasearch engine ([searchmux](https://github.com/vaughanb/searchmux)) that fans out queries to multiple backends (Google, DuckDuckGo, StackOverflow, GitHub, pkg.go.dev, npm, PyPI, Hacker News, Wikipedia) in parallel, merges and deduplicates results, and returns a ranked list. No external server or Docker container is required.

## Semantic code search

When **`embedding_model`** is set in config, codient indexes text files in the workspace and registers the **`semantic_search`** tool (all modes). The agent can find files by meaning (e.g. “authentication”, “migrations”) instead of relying only on exact-string `grep`.

- **API:** Embeddings default to the same **`base_url`** and **`api_key`** as chat completions (`POST /v1/embeddings`). When chat targets a server that does not implement that endpoint (Anthropic / Claude is the common case — you'll see `stream error: stream ID 1; INTERNAL_ERROR; received from peer` instead of vectors), set **`embedding_base_url`** (and optionally **`embedding_api_key`**) to route embeddings to a different server such as a local LM Studio or Ollama instance.
- **When indexing runs:** After you start an interactive session, indexing begins automatically in the background—no separate command. stderr shows progress and completion (or an error if embeddings fail).
- **Persistence:** The index is stored under **`<workspace>/.codient/index/embeddings.gob`**. On later sessions, unchanged files reuse cached vectors; only new or modified files are re-embedded. If you change **`embedding_model`**, the stored index is invalidated and rebuilt.
- **Configure:** `/config embedding_model <model-id>` (and optionally `/config embedding_base_url <url>` / `/config embedding_api_key <key>`), set the same fields in `~/.codient/config.json`, or use **`/setup`** — after picking the embedding model the wizard asks whether to point embeddings at a different server.

## Repository map (structural overview)

Codient extracts **top-level symbols** from supported source files (Go, Python, TypeScript/JavaScript, Rust, Java, C/C++ headers) and builds a **concise map** of the workspace: file paths and symbol names (functions, types, classes, etc.). This gives the model a bird’s-eye view without reading every file—similar in spirit to other agents’ “repo map” context.

- **System prompt:** When **`repo_map_tokens`** is not **`-1`**, a **Repository map** section is added after **Project** (auto-detected stack hints). The budget is **`0`** = automatic by file count (roughly 2k–8k estimated tokens), or a positive integer to cap size.
- **Tool:** The **`repo_map`** tool is registered in all modes when the map is enabled. Optional **`path_prefix`** scopes to a subdirectory; optional **`max_tokens`** overrides the default size for that call (useful if the prompt map was truncated).
- **No embeddings required:** Unlike **`semantic_search`**, the structural map does not call the embeddings API.
- **When it runs:** Interactive REPL builds the map in the background after startup (stderr: `building repository map…` / `repo map ready`). Single-turn (`-prompt` / piped) runs build it **synchronously** so the first turn can use it.
- **Persistence:** Tag extraction is cached under **`<workspace>/.codient/index/repomap.gob`** (per-file mtimes; unchanged files are skipped).
- **Configure:** `/config repo_map_tokens <n>`, or set **`repo_map_tokens`** in `~/.codient/config.json`. Use **`-1`** to disable entirely.

## MCP (Model Context Protocol) servers

Codient can connect to external **MCP servers** and expose their tools to the agent alongside built-in tools. This lets you extend the agent with any MCP-compatible tool provider (databases, APIs, custom workflows, etc.).

Configure MCP servers in `~/.codient/config.json` under the `mcp_servers` key. Each entry is a server ID mapped to its connection config:

```json
{
  "mcp_servers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user"],
      "env": {}
    },
    "remote-api": {
      "url": "https://api.example.com/mcp",
      "headers": {"Authorization": "Bearer sk-xxx"}
    }
  }
}
```

**Transport types:**

- **stdio** (`command` + `args`): Spawns the server as a subprocess and communicates over stdin/stdout. Use for local MCP servers (e.g. `npx @modelcontextprotocol/server-filesystem`). Optional `env` passes environment variables to the process.
- **Streamable HTTP** (`url`): Connects to a remote MCP server endpoint. Optional `headers` are sent with each request (useful for auth tokens).

**How it works:**

- On session start, codient connects to all configured servers and discovers their tools via `tools/list`.
- MCP tools are registered in the tool registry with namespaced names: `mcp__<serverID>__<toolName>` (e.g. `mcp__filesystem__read_dir`). This prevents collisions with built-in tools.
- The agent calls MCP tools the same way as built-in tools — no special handling needed.
- If a server fails to connect, a warning is printed and the session continues without that server's tools.
- Use the `/mcp` slash command to inspect connected servers and their tools.

## LSP servers

Codient can connect to **LSP (Language Server Protocol)** servers and expose type-aware code intelligence tools to the agent. This gives the model precise go-to-definition, find-references, hover docs, and rename capabilities backed by the same language servers your editor uses.

Configure LSP servers in `~/.codient/config.json` under the `lsp_servers` key. Each entry is a **language ID** mapped to its server config:

```json
{
  "lsp_servers": {
    "go": {
      "command": "gopls",
      "args": ["serve"],
      "env": {},
      "file_extensions": [".go"]
    }
  }
}
```

- **`command`** + **`args`**: the server binary and arguments (stdio transport).
- **`env`**: optional environment variables passed to the server process.
- **`file_extensions`**: file suffixes this server handles (used for routing).

**Tools registered:**

| Tool | Mutating | Modes |
|------|----------|-------|
| `lsp_definition` | no | all |
| `lsp_references` | no | all |
| `lsp_hover` | no | all |
| `lsp_type_definition` | no | all |
| `lsp_implementation` | no | all |
| `lsp_document_symbols` | no | all |
| `lsp_workspace_symbols` | no | all |
| `lsp_rename` | **yes** | build only |

**Positions** use **1-based** line and character numbers (the client translates to the 0-based protocol offsets internally).

**How it works:**

- On session start, codient connects to all configured servers eagerly and performs the LSP `initialize` / `initialized` handshake.
- Tools are registered in the tool registry with names as listed above. The agent calls them the same way as built-in tools.
- If a server fails to start or crashes, a warning is printed and the session continues without that server's tools — server failures are **warnings, not fatal errors**.
- Use the **`/lsp`** slash command to inspect connected servers and their capabilities.

## Lifecycle hooks

Codient can run **shell commands** at specific points in the agent lifecycle (similar to hooks in Claude Code, Cursor, and OpenAI Codex CLI). Hooks are **opt-in**: set **`hooks_enabled`** to **`true`** in `~/.codient/config.json` or via **`/config hooks_enabled true`**.

**Discovery:** Both of these files are loaded and merged (all matching hook groups run):

- `~/.codient/hooks.json` (or `$CODIENT_STATE_DIR/hooks.json`)
- `<workspace>/.codient/hooks.json`

**Schema** (nested event → matcher → handlers):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "run_command|run_shell",
        "hooks": [
          {
            "type": "command",
            "command": "python3 .codient/hooks/check.py",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

- **`matcher`** is a regular expression (Go RE2). Empty or omitted matches all tools for tool events; **`SessionStart`** matches against `source` (`startup` or `resume`).
- **`type`** is **`command`** only (phase 1). The command receives **JSON on stdin** (session id, cwd, hook name, model, `turn_id`, plus event-specific fields).
- **`timeout`** is in seconds (default 600 if omitted). **`failClosed`**: when `true`, a crash, timeout, or invalid JSON from that handler is treated as a failure (blocking for `PreToolUse` / `UserPromptSubmit` when applicable).
- **Exit code `2`** without JSON means “block” (stderr can carry a reason). Other non-zero exits **fail open** unless **`failClosed`** is set.

**Events:**

| Event | When | Matcher |
|-------|------|-----------|
| `SessionStart` | REPL or `-print` session begins | `startup` / `resume` |
| `PreToolUse` | Before each tool runs | Tool name (e.g. `write_file`, `mcp__…`) |
| `PostToolUse` | After each tool runs | Tool name |
| `UserPromptSubmit` | Before your message is sent to the model | *(all)* |
| `Stop` | Model returns a **text** reply (no tool calls) in a turn | *(all)* — `decision: "block"` with **`reason`** requests another model round with that text as the user message (Codex-style continuation) |
| `SessionEnd` | Session exits | *(all)* |

**Stdout JSON** (subset): `decision` (`block` to deny a prompt/tool or, for `Stop`, to continue with `reason`), `reason`, `additional_context`, `system_message`, `continue` (`false` stops `Stop` continuation).

Use **`/hooks`** in the REPL to list configured hooks. Sub-agents (`delegate_task`) do not run parent hooks.

## Auto-update

Codient checks for new releases on GitHub each time an interactive REPL session starts. If a newer version is available (and was not previously skipped), you are prompted:

```
codient: update available 0.2.0 -> 0.3.0
Install now? [Y/n]
```

- **Y** (or Enter) — downloads the release, replaces the binary in-place, and exits. Restart codient to use the new version.
- **n** — skips this version. Codient remembers the choice (in `~/.codient/update_skip`) and will not ask again until an even newer release is published.

For non-interactive or scripted updates, use the `-update` flag:

```bash
codient -update
```

To disable the startup prompt entirely, set `update_notify` to `false` in config:

```
/config update_notify false
```

The `-update` flag always works regardless of this setting.
