# Development

```bash
make check       # vet + unit tests only (no live LLM; safe for CI)
make test-unit   # same tests as check, without vet
make test-race   # race detector (also run in GitHub Actions CI after check + build)
make test        # full suite: unit tests + live integration (needs ~/.codient/config.json model + API)
```

`make test` sets `CODIENT_INTEGRATION=1`, `CODIENT_INTEGRATION_STRICT_TOOLS=1`, and `CODIENT_INTEGRATION_RUN_COMMAND=1`, and runs `go test -tags=integration` with a 90-minute timeout so workspace tools, strict tool-calling, and `run_command` are all exercised.

Lighter integration runs (see `make help`):

```bash
make test-integration         # live API only (CODIENT_INTEGRATION=1)
make test-integration-strict  # + strict tool tests (no run_command test unless you set CODIENT_INTEGRATION_RUN_COMMAND=1 yourself)
make test-acp                 # live ACP subprocess tests (spawns codient -acp; same JSON-RPC as Codient Unity)
```

## Intent-Driven Orchestrator (auto-only)

Codient has **one** execution path: every CLI / `-print` / ACP turn runs through the orchestrator, and the internal `prompt.Mode` (`ModeBuild` / `ModeAsk` / `ModePlan`) is an implementation detail of that path. There is no `-mode` flag, no `/build` / `/ask` / `/plan` slash commands, and no `session/set_mode` RPC.

Key files:

- **`internal/intent.IdentifyIntent`** is the orchestrator's classifier entry point. The classification path has three tiers:

  1. **Heuristic fast path (`heuristicQuickClassify`)** — runs first on every turn (unless `disable_intent_heuristic` is set). A pure pattern matcher checks the prompt for high-confidence signals and returns immediately when one fires, skipping the supervisor LLM entirely. Patterns:
     - **DESIGN phrase prefixes**: `create a plan`, `draft a plan`, `make a plan`, `design a/the/an X`, `architect a/the/an X`, `how should I/we`, `how would you`, `what's the best way`, `what approach`, `plan for …`, `plan how to …`, `sketch a design`.
     - **Leading edit verb + multi-file scope hint** → COMPLEX_TASK. Edit verbs: `add`, `address`, `build`, `change`, `clean`, `create`, `delete`, `design`, `draft`, `extract`, `factor`, `fix`, `hook`, `implement`, `improve`, `inline`, `install`, `introduce`, `make`, `migrate`, `move`, `patch`, `port`, `refactor`, `remove`, `rename`, `replace`, `restructure`, `rewrite`, `sketch`, `split`, `tweak`, `update`, `wire`, `write`. Multi-file scope hints: `across`, `everywhere`, `throughout`, `all files`, `every file`, `entire`, `whole codebase`, `every module`.
     - **Leading edit verb + small-scope hint** → SIMPLE_FIX. Small-scope hints: `typo`, `comment`, `log statement`, `in line N`, `on line N`, `this function`, `this method`, `this file`, `this class`, `single line`, `import statement`, `trailing whitespace`.
     - **Polite imperative (`please …`, `could you …`, `can you …`, `would you …`, `kindly …`, `hey codient, …`) + edit verb** → SIMPLE_FIX. The politeness prefix is stripped before verb detection, so `please fix X` is recognised even though `please` isn't an edit verb.
     - **QUERY phrase prefixes**: `what`, `why`, `where`, `when`, `who`, `which`, `whose`, `explain`, `describe`, `summarize`, `tell me about`, `tell me`, `is this`, `is there`, `is the`, `are there`, `are the`, `does this`, `does the`, `do you`, `do these`, `can you tell/explain/describe`, `how does`, `how is`, `how are`, `how was`, `how were`.
     - **Trailing `?`** (when no leading edit verb) → QUERY.

     Anything ambiguous (no clear pattern match) falls through to tier 2 — the heuristic refuses to guess on borderline cases. Confidence-first: false negatives are fine (they just cost an LLM call) but false positives are bad (they skip a smarter classifier).

  2. **Supervisor LLM (`runSupervisor`)** — a tiny JSON-only chat completion (default `MaxCompletionTokens: 80`, `Temperature: 0`, `ResponseFormat: JSONObject`) against the **low** reasoning tier. Five layers of defense make this robust against thinking-capable local models:
     - **`/no_think` directive** — every supervisor user message ends with `/no_think` (see `intent.supervisorUserSuffix`). Qwen3-Thinking / DeepSeek-R1 recognise it as a per-turn switch that bypasses the hidden reasoning channel; non-thinking models ignore the trailing token.
     - **Anti-thinking system prompt** — `SupervisorSystemPrompt` includes a model-agnostic line that instructs the model not to emit chain-of-thought before JSON and to make `{` its very first output token.
     - **`<think>` tag stripping** — the parser strips closed `<think>` / `<thought>` / `<reasoning>` / `<reflection>` / `<scratchpad>` blocks before scanning for JSON, so models that emit JSON alongside their hidden reasoning still classify cleanly on the first attempt.
     - **Truncation retry** — when a truncated reply (`finish_reason == "length"`) yields no parseable JSON, the supervisor **automatically retries once** with a much larger budget (4× initial, clamped to `[1024, 2048]`). Power users can override the initial budget via `low_reasoning_max_completion_tokens` to skip the retry; the retry budget itself can be tuned per-call via `intent.Options.RetryMaxCompletionTokens`.
     - **Reasoning-channel salvage** — when `Message.Content` failed to parse, `tryReasoningChannelSalvage` decodes the raw assistant JSON itself and looks for `reasoning_content` / `reasoning` fields (string, or object with `text` / `content` / `summary`). LM Studio, vLLM, and patched Ollama servers leak the structured answer into that non-standard channel when the budget runs out — the supervisor recovers the classification *without* consuming the retry.

  3. **Heuristic fallback (`heuristicFallback`)** — invoked when the supervisor LLM cannot produce a parseable answer (parse error, truncation without recovery, chat error). It calls `heuristicQuickClassify` on the original prompt and routes to the matched category on a confident match, or defaults to `QUERY` on no match (safety net so a faulty supervisor can never deadlock the agent on a write path the user never explicitly requested). The `Reasoning` string surfaces **per-attempt** `budget`, `finish_reason`, `channel` (content / reasoning_content / reasoning), and a body summary — that string is what the user sees on the `codient: intent: … (fallback) — supervisor: …` status line, so triage-by-eyeball is possible.

  The returned `Identification.Source` is one of:
  - `SourceSupervisor` — tier 2 returned a parseable answer.
  - `SourceHeuristic` — tier 1 (pre-LLM fast path) matched.
  - `SourceHeuristicFallback` — tier 3 (post-LLM-failure safety net) returned the result; also implies `Fallback: true`.

  The on-screen status line (`formatIntentStatusLine` in `internal/codientcli`) renders `(heuristic)` for tier 1, `(fallback)` for tier 3, and no tag for tier 2 (expected case, keep terse). The ACP `session/intent_identified` notification gained a `source` string field so Codient Unity can display the same provenance in its transcript. The new config key **`disable_intent_heuristic`** (default `false`, override via top-level config or profile) disables tier 1 so every turn consults the model.
- **`internal/codientcli/orchestrator.go::orchestratedTurn`** maps the category to a concrete internal mode and swaps in the right registry / system prompt / OpenAI client for the turn via [`transitionToInternalMode`](../internal/codientcli/modeswitch.go) (a pure runtime-artifact swap with no user-facing chatter and no history mutation). For **COMPLEX_TASK**, [`runOrchestratedBuildPhase`](../internal/codientcli/orchestrator.go) performs the plan -> build transition *first* and then builds the synthetic implementation directive with [`buildPlanHandoffMessage`](../internal/codientcli/designhandoff.go) against the now-active build-mode tool registry, so the directive never cites stale plan-mode tools. [`planHandoffApplies`](../internal/codientcli/planhandoff.go) is the single source of truth for "is there anything to implement?".
- After every orchestrated turn the session resets to **`prompt.ModeAuto`**; the resolved mode of the last turn is remembered as `session.lastTurnMode` so plan parsing, design saving, and the REPL "Answer:" prompt can still distinguish the path.
- **`acp_serve.go::orchestrateACPTurn`** drives every ACP `session/prompt`. It emits **`session/intent_identified`** and **`session/mode_status`** notifications (with **`handoff: true`** when an automatic plan -> build transition happens). **`session/set_mode`** is no longer routed and returns JSON-RPC **`-32601` "method not found"**.
- Two reasoning tiers (`config.TierLow`, `config.TierHigh`) drive client selection through **`openaiclient.NewForTier`** — there is no `NewForMode`. `delegate_task` sub-agents pick a tier from the requested sub-agent mode (`build` / `ask` -> low, `plan` -> high).
- Legacy config keys (`mode`, `models.<mode>`) are accepted by the JSON parser, logged once via `maybeWarnDeprecatedMode` / `maybeWarnDeprecatedModeModels`, and ignored at runtime.

To debug supervisor classifications quickly:

```bash
make build
CODIENT_INTEGRATION=1 ./bin/codient -print -prompt "Refactor session.go into smaller files"   # observe the `intent: ...` line on stderr
```

Live tests:

- **`internal/intent`** has a gated `integration_test.go` (`CODIENT_INTEGRATION=1`) that asks the configured low-tier model to classify canned prompts and asserts the category falls in the expected set. An additional opt-in test **`TestIntegration_IdentifyIntent_RetryOnTinyBudget`** (gate: `CODIENT_INTEGRATION_THINKING=1`) forces a 16-token initial budget so a real thinking model exercises the retry-on-length path end-to-end.
- **`internal/codientcli/acp_integration_test.go`** covers auto-mode round-trips, the `session/intent_identified` and `session/mode_status` notifications (mock LLM), the live COMPLEX_TASK plan -> build hand-off, and verifies that `session/set_mode` is rejected with method-not-found.
- **`internal/agent/integration_test.go`** continues to cover the **`OnIntent`** callback and the agent-level plan -> build behaviour the orchestrator depends on.

Set **`CODIENT_INTEGRATION_STRICT_TOOLS=1`** when running ACP integration tests that assert tool calls (the COMPLEX_TASK hand-off test in particular).

## Process error log

For crashes and failed turns without enabling full **`-log`** JSONL telemetry, codient opens an append-only **process error log** when the state directory is known (`~/.codient` or **`CODIENT_STATE_DIR`**): **`<state dir>/logs/errors-<UTC>-<pid>.log`**. Lines are RFC3339-timestamped plain text; they include failed **`RunConversation`** / REPL turns, **`IdentifyIntent`** errors (orchestrator supervisor), ACP **`session/prompt`** errors, and **panics** (value plus `runtime/debug.Stack`). Disable the file with **`CODIENT_ERROR_LOG=0`** (also `false`, `off`, `no`). User-facing summary: [usage.md](usage.md) (notable flags and slash **`/log`**). Implementation: **`internal/errorsink`**, **`internal/agent.Runner.ErrorLog`**, **`internal/codientcli/run.go`**, **`session.go`**, **`acp_serve.go`**, **`orchestrator.go`**.

## Optional dependencies

**Clipboard image paste** (`/paste`, Ctrl+V in TUI) shells out to platform-specific tools:

| Platform | Tool | Install |
|----------|------|---------|
| Linux (Wayland) | `wl-paste` | `apt install wl-clipboard` / `pacman -S wl-clipboard` |
| Linux (X11) | `xclip` | `apt install xclip` / `pacman -S xclip` |
| macOS | `osascript` | Built-in |
| Windows | `powershell` | Built-in |

Two clipboard shapes are accepted on every platform:

1. **Raw image bytes** — `image/*` on Linux, `ContainsImage()` on Windows, `«class PNGf» / «class JPEG»` on macOS. Used by browsers (right-click → Copy Image) and screenshot tools (e.g. `gnome-screenshot --clipboard`).
2. **A reference to an image file on disk** — `text/uri-list` on Linux, `FileDropList` on Windows, `«class furl»` on macOS. Used by file managers (Nautilus, Dolphin, Finder, Explorer) when the user copies an image file. The first entry whose extension is a supported image format (PNG, JPEG, GIF, WebP) is selected and its original path is returned without copying.

The clipboard integration test (`internal/clipboard/integration_test.go`) requires `CODIENT_INTEGRATION=1` and a real clipboard with an image (or an image file URI) copied. To exercise the URI-list path on Linux:

```bash
printf 'file://%s' /absolute/path/to/screenshot.png | wl-copy --type text/uri-list
CODIENT_INTEGRATION=1 go test -tags integration -run TestIntegration_SaveImage -v ./internal/clipboard/
```

**LSP integration test** (`internal/lspclient/integration_test.go`) requires `CODIENT_INTEGRATION=1` and the **`gopls`** binary on `PATH` (`exec.LookPath("gopls")`). The test starts a real `gopls` server, exercises the LSP handshake and tool operations against a temporary Go workspace, and is skipped when either gate is unmet.
