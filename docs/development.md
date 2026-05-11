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

- **`internal/intent.IdentifyIntent`** issues a tiny JSON-only chat completion (`MaxCompletionTokens: 80`, `Temperature: 0`, `ResponseFormat: JSONObject`) against the **low** reasoning tier and parses the `{ "category": ..., "reasoning": ... }` payload. Malformed / unknown replies fall back to **`CategoryQuery`** (read-only) so a faulty supervisor never escalates privileges.
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

- **`internal/intent`** has a gated `integration_test.go` (`CODIENT_INTEGRATION=1`) that asks the configured low-tier model to classify canned prompts and asserts the category falls in the expected set.
- **`internal/codientcli/acp_integration_test.go`** covers auto-mode round-trips, the `session/intent_identified` and `session/mode_status` notifications (mock LLM), the live COMPLEX_TASK plan -> build hand-off, and verifies that `session/set_mode` is rejected with method-not-found.
- **`internal/agent/integration_test.go`** continues to cover the **`OnIntent`** callback and the agent-level plan -> build behaviour the orchestrator depends on.

Set **`CODIENT_INTEGRATION_STRICT_TOOLS=1`** when running ACP integration tests that assert tool calls (the COMPLEX_TASK hand-off test in particular).

## Optional dependencies

**Clipboard image paste** (`/paste`, Ctrl+V in TUI) shells out to platform-specific tools:

| Platform | Tool | Install |
|----------|------|---------|
| Linux (Wayland) | `wl-paste` | `apt install wl-clipboard` / `pacman -S wl-clipboard` |
| Linux (X11) | `xclip` | `apt install xclip` / `pacman -S xclip` |
| macOS | `osascript` | Built-in |
| Windows | `powershell` | Built-in |

The clipboard integration test (`internal/clipboard/integration_test.go`) requires `CODIENT_INTEGRATION=1` and a real clipboard with an image copied.
