# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **LSP (Language Server Protocol) client:** configure `lsp_servers` in config to enable type-aware code intelligence tools (`lsp_definition`, `lsp_references`, `lsp_hover`, `lsp_type_definition`, `lsp_implementation`, `lsp_document_symbols`, `lsp_workspace_symbols`, `lsp_rename`). Servers connect eagerly at session start. Use `/lsp` to inspect status.
- **Named config profiles:** define multiple workflow bundles (`local`, `frontier`, `ci-strict`, ...) under `profiles` in `~/.codient/config.json` and switch with `-profile <name>`, `CODIENT_PROFILE` env var, or `active_profile` in config. New `/profile` slash command supports `list`, `show`, `diff`, `save`, `delete`, `default`, and mid-session swap with full subsystem rebuild. ACP integration: `agent/list_profiles`, `session/new` accepts optional `profile` param, `session/set_profile` + `session/profile_changed` notification. The `/setup` wizard offers an optional save-as-profile step. Config schema bumped to v2 (additive; v1 configs load unchanged). See [Named profiles](docs/configuration.md#named-profiles).
- **Intent-Driven Orchestrator (auto-only mode):** every CLI / `-print` / ACP turn now runs through an internal supervisor LLM call that classifies the prompt as **QUERY**, **DESIGN**, **SIMPLE_FIX**, or **COMPLEX_TASK** and routes it to the matching internal path. **COMPLEX_TASK** automatically hands off from plan to build (gated by **`-force`** / **`-yes`** in non-interactive runs). New `internal/intent.IdentifyIntent`, `internal/codientcli/orchestrator.go::runOrchestratedTurn`, and `acp_serve.go::orchestrateACPTurn`. See [docs/usage.md](docs/usage.md#intent-driven-orchestrator) and [docs/development.md](docs/development.md#intent-driven-orchestrator-auto-only).
- **Supervisor hardening for thinking models:** `IdentifyIntent` now strips `<think>` / `<thought>` / `<reasoning>` / `<reflection>` / `<scratchpad>` blocks before scanning for JSON and auto-retries once with a larger budget (4× initial, clamped to `[1024, 2048]`) when the first attempt truncates with `finish_reason == "length"` and no JSON could be recovered. Fixes the `supervisor: parse error` fallback that Qwen3-Thinking / DeepSeek-R1 / GLM-Z1 users were hitting on every prompt. New config key **`low_reasoning_max_completion_tokens`** lets power users raise the initial budget (default `80`, clamped to `2048`) to skip the retry on known-thinking models. New `intent.Options{MaxCompletionTokens, RetryMaxCompletionTokens}` for callers that want per-call control. See [docs/configuration.md](docs/configuration.md#config-file-reference-codientconfigjson).
- **Heuristic intent classifier (pre-LLM fast path):** every CLI / `-print` / ACP turn now runs through a deterministic pattern-based classifier *before* the supervisor LLM is consulted. When the prompt's structure unambiguously implies one of the four orchestrator categories, the LLM call is skipped entirely — the user sees `codient: intent: SIMPLE_FIX (heuristic) — polite imperative: fix` instead of waiting 200-1500ms for a supervisor round-trip. High-confidence patterns: **DESIGN phrase prefixes** (`create a plan`, `draft a plan`, `design a/the/an X`, `how should I/we`, `what's the best way`, `plan for …`); **leading edit verb + multi-file scope hint** (`refactor … across every file`, `migrate the entire codebase`, `update all files`) → COMPLEX_TASK; **polite imperative or leading edit verb + small-scope hint** (`please fix typo`, `can you rename …`, `fix on line 42`, `add a comment to this function`) → SIMPLE_FIX; **query phrase prefixes or trailing `?`** (`what is …`, `how does …`, `explain …`, `tell me about …`, `is this safe?`) → QUERY. Anything ambiguous falls through to the supervisor LLM, which can still read context the heuristic cannot. The same `heuristicQuickClassify` now drives the post-LLM-failure fallback (replacing the previously conservative "QUERY or DESIGN only" rule) so a clearly-typed `please fix X` no longer downgrades to QUERY when the supervisor times out. New `intent.Source` field (`supervisor` / `heuristic` / `heuristic-fallback`) is surfaced on the on-screen status line as `(heuristic)` / `(fallback)` / `(no tag)` and on the ACP `session/intent_identified` notification as a `source` string. New `disable_intent_heuristic` config key (default `false`) opts out of the fast path for users who want every turn to consult their model. See [docs/configuration.md](docs/configuration.md#supervisor-token-budget-thinking-models) and [docs/development.md](docs/development.md#intent-driven-orchestrator-auto-only).
- **Supervisor hardening — second pass:** four additional layers of defense for thinking-model setups where the supervisor was still falling back to `QUERY` with an empty body even after the retry path (observed on local Qwen3-style 35B variants where the entire 1024-token retry budget gets burned inside the hidden reasoning channel without ever emitting JSON). (1) The supervisor user message now ends with the **`/no_think`** directive, which Qwen3-Thinking / DeepSeek-R1 recognise as a per-turn switch that bypasses the reasoning channel; other models treat it as harmless trailing text. (2) `SupervisorSystemPrompt` gained an explicit "Do NOT think out loud … Your VERY FIRST token must be `{`" line for model-agnostic guidance. (3) When `Message.Content` failed to parse, `IdentifyIntent` now decodes the raw assistant JSON itself and looks for `reasoning_content` / `reasoning` (strings, or `{text|content|summary}` objects) — LM Studio, vLLM, and patched Ollama servers leak the structured answer into that non-standard field when the budget runs out, and we recover the classification without consuming the retry. (4) When both LLM attempts fail, a new **`heuristicCategory`** keyword classifier picks between `QUERY` and `DESIGN` based on the prompt's leading imperative verb (`refactor`, `fix`, `implement`, …) or design phrasing (`how should I structure`, `what's the best way`) — write-path categories (`SIMPLE_FIX`, `COMPLEX_TASK`) are **never** returned by the heuristic so a failed supervisor cannot silently grant writes. The on-screen `codient: intent: … (fallback) — supervisor: …` line now also surfaces the **per-attempt** `budget`, `finish_reason`, `channel`, and a body summary, so users can immediately see why classification failed (and which channel the salvage tried). See [docs/development.md](docs/development.md#intent-driven-orchestrator-auto-only).
- **Two reasoning tiers:** `low_reasoning_model` (supervisor + QUERY + SIMPLE_FIX) and `high_reasoning_model` (DESIGN + COMPLEX_TASK planning), each with optional `_base_url` / `_api_key` siblings. New `config.ConnectionForTier` and `openaiclient.NewForTier`. The `/setup` wizard now configures both tiers. See [docs/configuration.md](docs/configuration.md#config-file-reference-codientconfigjson).
- **ACP notifications for orchestration:** `session/intent_identified` (per `session/prompt`, with `category`, `reasoning`, `fallback`) and `session/mode_status` with `phase: "changed"` / `"plan_ready"` and a `handoff: true` flag on automatic plan -> build transitions.
- **`/edit-plan`** (alias `/ep`) REPL slash command: opens the active plan in `$EDITOR` / `$VISUAL` (fallbacks: `vi`, `notepad`); on save the markdown is re-parsed back into `plan.json` and the revision is bumped.
- **Clipboard image paste:** `/paste` slash command and **Ctrl+V** in the TUI grab an image from the OS clipboard and attach it to the next message. Platform-specific: `wl-paste` (Wayland) or `xclip` (X11) on Linux; `osascript` on macOS; PowerShell on Windows. Both **raw image bytes** (`image/*`, `ContainsImage`, `«class PNGf»`) and **a copied image file** (`text/uri-list`, `ContainsFileDropList`, `«class furl»`) are accepted — the latter is what file managers (Nautilus, Dolphin, Finder, Explorer) actually place on the clipboard when you "Copy" an image file. Stale temp files are cleaned up automatically. See [docs/usage.md](docs/usage.md#images-and-vision).
- **`/create-rule`** REPL slash command: interactive wizard that writes a Cursor-compatible **`.mdc`** rule under **`<workspace>/.cursor/rules/`** (`description`, optional **`globs`**, **`alwaysApply`**). See [docs/usage.md](docs/usage.md#slash-commands).
- **Single-shot / `-print` session resume:** non-REPL runs save to **`<workspace>/.codient/sessions/`** after each turn and, unless **`-new-session`**, load the latest session (or **`-session-id <id>`**). JSON / `stream-json` output includes **`session_id`** and **`workspace`**. See [docs/usage.md](docs/usage.md#headless--ci-mode--print) and [Bring-your-own remote runs](docs/usage.md#bring-your-own-remote-and-background-runs).
- **Multi-line input editor** in the TUI: the input panel is now a true multi-line text area that word-wraps and grows up to 8 visible rows. **Enter** submits; **Ctrl+J** / **Alt+Enter** / **Shift+Enter** inserts a newline. Standard editing keys (Home/End, Ctrl+W, Ctrl+U, Backspace/Delete) work within the box.
- **Turn interruption:** **Ctrl+C** (or **Escape** in the TUI) while the agent is working cancels the current turn and returns to the prompt without exiting codient. The second Ctrl+C while idle quits. See [docs/usage.md](docs/usage.md#interrupting-a-running-turn).
- **Intent nudge for local models:** when tools are enabled and the model returns intent-only prose without tool calls or XML markup, codient injects a single follow-up message asking the model to emit real tool calls (stderr progress: `requesting tool calls…`). This helps local OpenAI-compatible servers that occasionally narrate intent instead of invoking tools.
- **Color-coded REPL voices:** user-authored content (boxed user messages, input panel accent strip, `> ` prompt) renders in **codient-blue** while codient agent framing (welcome banner border, `● ` intent / progress bullets) renders in **codient-purple** — the two endpoints of the welcome logo gradient. The split is consistent across every turn since the orchestrator picks the internal mode silently; slash commands stay as echoed lines without a box, and their effect (new session, model change, …) is the only feedback shown.
- **Vendor directory exclusion:** code indexing and repo map walkers now skip `vendor/` directories so Go vendored dependencies are not indexed.
- **Delegate sandbox profiles:** admin-defined named profiles (`delegate_sandbox_profiles`) for `delegate_task` sub-agents. Each profile can specify a container image, network policy (`none`/`bridge`/`host`), resource limits (memory, CPU, pids), and a `long_lived` flag that keeps a single container running per delegate (preserving build caches between `run_command` calls). The model selects a profile via the `sandbox_profile` parameter on `delegate_task`. See [Configuration](docs/configuration.md#delegate-sandbox-profiles).
- **Long-lived container sessions:** new `ContainerSession` in `internal/sandbox` provides `Start` / `Exec` / `Close` lifecycle for a single container. Delegates with `long_lived: true` profiles use `docker run -d` + `docker exec` instead of a fresh container per shell command.
- **Symlink-escape hardening:** `absUnderRoot` (used by all file tools: `read_file`, `write_file`, `str_replace`, `insert_lines`, `list_dir`, `search_files`) now evaluates symlinks and rejects paths whose real target escapes the workspace root. Symlinks that resolve inside the root are still allowed.
- **Default process error log:** append-only plain-text log under `<state dir>/logs/errors-<UTC>-<pid>.log` for failed agent turns, supervisor/intent errors, ACP `session/prompt` failures, and panics (with stacks). Disable with **`CODIENT_ERROR_LOG=0`**. Separate from optional **`-log`** JSONL telemetry. New package **`internal/errorsink`**; **`internal/agent.Runner`** gains optional **`ErrorLog`** and panic recovery in **`RunConversation`**; REPL, **`-print`**, and **`-acp`** wire the sink and session-level recovery. See [docs/usage.md](docs/usage.md) and [docs/development.md](docs/development.md#process-error-log).

### Changed

- **Removed the `[a/r/e/c]` plan-approval menu** that previously fired when a plan-mode reply ended with `Ready to implement`. The plan -> build hand-off is now driven purely by the orchestrator (interactive prompt or `-force` / `-yes`). Existing plan-resume behavior (`r/d/n` selector) is preserved; explicit `/run-plan` still drives the structured-phase executor.
- **TUI input prompt** simplified to a plain `> ` (no mode label) since the orchestrator decides the path per turn.
- **TUI viewport scrolling:** **Page Up/Down** and **Alt+Up/Down** scroll the transcript; plain **Up/Down** and **Home/End** now belong to the multi-line input editor.
- **Intent / reasoning display:** model pre-tool prose and streaming reasoning deltas appear as mode-accent **● intent lines** (replaces the old boxed "Thinking:" blocks). **Ctrl+T** toggles compact vs full display.
- **Styled markdown output:** assistant replies are rendered with GitHub-flavored markdown (via glamour) at end of turn even when stdout is not a TTY, unless **`-plain`** is set. **`-stream-reply`** applies to plain sessions only.
- **`stream_with_tools`** defaults to **`false`**: non-streaming completions for tool-enabled turns reduce dropped `tool_calls` on local servers.

### Fixed

- **Clipboard image paste with file-manager copies:** `/paste` and **Ctrl+V** previously checked only for raw image MIME types on the clipboard (`image/*`, `ContainsImage()`, `«class PNGf»`), so any image copied from a file manager — which puts a file *reference* on the clipboard (`text/uri-list` on Linux, `FileDropList` on Windows, `«class furl»` on macOS) rather than the bytes — was reported as "no image found in clipboard" even though one was clearly there. `internal/clipboard.HasImage` and `internal/clipboard.SaveImage` now fall back to scanning the file-reference form on every platform and accept the first entry whose extension is a supported image type (PNG, JPEG, GIF, WebP). For URI-list / file-drop matches the original on-disk path is returned without copying. Covered by new `internal/clipboard/clipboard_test.go` cases (`TestHasImage_WaylandURIListImage`, `TestHasImage_X11URIListImage`, `TestHasImage_URIListNonImage`, `TestHasImage_URIListMissingFile`, `TestSaveImage_WaylandURIList`, `TestSaveImage_X11URIList`, `TestSaveImage_NoImageNoFiles`, `TestParseURIList`, `TestFileURIToPath`) and verified live against `wl-paste` with `CODIENT_INTEGRATION=1`.
- **`/setup` mid-session model change:** the TUI footer below the input box now refreshes immediately when `/setup` selects a different chat model. Previously the post-wizard reload rebuilt the client and registry but never published a fresh chrome update, so the footer kept displaying the old model until the next turn finished (or some other path happened to push a chrome refresh). Fixed by routing the slash-command path through `applyPostSetupReload`, which always ends with `sendTUIChrome`.
- **Orchestrator plan -> build double-handoff with stale tool names:** in both the CLI orchestrator and the ACP server, the plan-to-build hand-off message used to be constructed *before* the mode transition, so it listed plan-mode (read-only) tools instead of build-mode tools. At the same time the mode-switch helper *also* appended its own copy of the hand-off message, so the build agent saw two consecutive directives with the wrong tool set in one of them. The mode-switch helper (`transitionToInternalMode`) is now a pure runtime-artifact swap — it changes the mode, reasoning tier, registry, and system prompt but never mutates conversation history. The orchestrator (`runOrchestratedBuildPhase`) and the ACP server (`orchestrateACPTurn`) now perform the build-mode swap *first* and then build the hand-off message against `registry.Names()` of the freshly installed build registry, so the synthetic user prompt always cites the correct tool list.
- **Missing "working" indicator during supervisor LLM call:** the TUI/REPL "Agent is working…" spinner used to be started inside `executeTurn`, but on every turn the orchestrator runs the supervisor LLM call (and now also potentially a retry) *before* `executeTurn`. With local thinking models that can take several seconds, leaving the user staring at a blank prompt with no feedback. `internal/codientcli/orchestrator.go::orchestratedTurn` now begins the working indicator (`tuiWorkingMsg(true)` for the TUI, `startWorkingSpinner(os.Stderr)` for plain REPL) before `identifyIntent` and clears the stderr spinner just before printing the `codient: intent: …` line. `executeTurn` re-asserts the same state so the on-screen indicator stays continuously visible from prompt submission until the turn completes.
- **Auto-mode resume:** the persistent REPL used to read the `Mode` field from `<workspace>/.codient/sessions/*.json` on resume and apply it directly to the live session, which could leave a resumed session pinned to `build` / `plan` / `ask`. Combined with the per-turn `defer` in the orchestrator (which only restored `ModeAuto` if the *current* mode wasn't already auto), this meant the orchestrator could keep mis-routing turns after a resume. The persisted `Mode` is now ignored on load (kept in the file for back-compat) and every session re-enters `ModeAuto`, matching the documented "auto-only" runtime.
- **Stale `-mode build` / `/build` references:** the `Plan mode` system prompt and the post-plan REPL pause hint still told the model / user to re-run codient with `-mode build` or to type `/build`. Both have been removed from the codebase, so the references were misleading. The system prompt now describes the orchestrator-driven hand-off, and the pause hint mentions `-force` / `-yes` (for `-print`) or a plain follow-up message (for the REPL).
- **Redundant auto-compact threshold check** in `internal/config/config.go` (`autoCompactPct == 0 && pc.AutoCompactPct == 0`) simplified to a single comparison.

### Tests

- New unit tests in `internal/codientcli`:
  - `TestTransitionToInternalMode_PlanToBuild_NeverMutatesHistory`, `TestTransitionToInternalMode_PlanToBuild_NoHandoffWhenNoPlan`, `TestTransitionToInternalMode_BuildToPlan_DoesNotMutateHistory`, `TestTransitionToInternalMode_SameMode_NoOp`, `TestTransitionToInternalMode_RebuildsRegistryForMode` cover the new contract for `transitionToInternalMode` (pure swap, registry rebuilt per mode, same-mode no-op).
  - `TestApplyStoredSessionState_LoadsHistoryIgnoresPersistedMode` and `TestApplyStoredSessionState_WorkspaceMismatch` lock in the "auto-only resume" behavior.
  - `TestRunOrchestratedBuildPhase_HandoffUsesBuildRegistry` and `TestRunOrchestratedBuildPhase_NoLastReply_StillTransitions` exercise the orchestrator's plan -> build chain end-to-end (mode flip, plan approval mark, handoff message refers to build-mode tools).
  - `TestACPApplyACPMode_SwapsRegistryAndPrompt` and `TestACPOrchestrateTurn_BuildHandoffMessageUsesBuildRegistry` cover the ACP path with the same invariants.
  - Existing `internal/intent` live integration test (`TestIntegration_IdentifyIntent_Categories`, gated by `CODIENT_INTEGRATION=1`) continues to exercise the supervisor against a real LLM, satisfying the workspace LLM-integration testing rule.
- New unit tests in `internal/intent`:
  - `TestExtractJSONObject` adds cases for closed `<think>` tags, multiple reasoning-tag variants, attributes on the opening tag, fenced JSON after a stripped tag, dangling unclosed tags, and JSON-before-dangling-tag preservation.
  - `TestStripReasoningTags` covers per-tag stripping (`think` / `thought` / `reasoning` / `reflection` / `scratchpad`) including case-insensitivity and dangling-open behavior.
  - `TestSummariseBody` validates the single-line diagnostic helper baked into fallback reasons.
  - New `identify_test.go` exercises `IdentifyIntent` end-to-end against an `httptest` mock server: happy path with default budget, `<think>`-prefixed reply without retry, retry-on-length recovery, configurable initial/retry budgets, no-retry when `finish_reason == "stop"`, retry-disabled when `RetryMaxCompletionTokens <= MaxCompletionTokens`, two-truncation fallback, token-tracker summation across both attempts, and the nil-client / empty-prompt guards.
  - New live integration test `TestIntegration_IdentifyIntent_RetryOnTinyBudget` (gated by `CODIENT_INTEGRATION=1` + `CODIENT_INTEGRATION_THINKING=1`) forces a 16-token initial budget so a real thinking model exercises the retry path.
  - **Second-pass hardening tests:** `TestIdentifyIntent_AppendsNoThinkDirective` (asserts every supervisor user message ends with `/no_think`), `TestIdentifyIntent_SalvagesFromReasoningContent` (stub server stuffs the JSON answer into a `reasoning_content` field — supervisor recovers it without retrying), `TestIdentifyIntent_HeuristicFallback_FallbackKeepsAmbiguousAsQuery` (ambiguous prompts stay on the read-only `QUERY` path), and `TestIdentifyIntent_FallbackReasoningIncludesAttemptDiagnostics` (per-attempt `budget=`, `finish="length"`, body summary surfaced in the fallback reason). New pure-function tests `TestSupervisorUserSuffixIsNoThinkDirective`, `TestHeuristicFallback_RoutesViaQuickClassifier`, `TestHeuristicFallback_NoMatchDefaultsToQuery`, `TestTryReasoningChannelSalvage` (six cases covering string / object-with-text / object-with-content / object-with-summary / missing field / invalid JSON), and `TestFormatAttempt`.
  - **Heuristic-classifier tests:** `TestHeuristicQuickClassify` is a 30-case table-driven test covering positives + negatives for all four categories — DESIGN phrases (`create a plan`, `draft a plan`, `design a X`, `how should I/we`, `what's the best way`, `plan for …`), COMPLEX_TASK scope hints (`across every file`, `entire codebase`, `everywhere`, `all files`, `throughout`), SIMPLE_FIX patterns (polite imperatives `please/can you/could you/would you`, small-scope hints `typo / comment / line N / this function`), QUERY starters (`what / why / how does / explain / describe / tell me about / is this`), trailing-`?` detection, plus deliberate "falls through to LLM" cases (ambiguous noun, edit verb without scope, verb-as-noun). `TestIdentifyIntent_HeuristicFastPath_NoLLMCall` and `TestIdentifyIntent_HeuristicFastPath_PerCategory` confirm the supervisor is **not** called when the heuristic matches (HTTP request count = 0). `TestIdentifyIntent_DisableHeuristic_AlwaysCallsLLM` confirms the `Options.DisableHeuristic` flag forces the LLM path. `TestFormatIntentStatusLine` (in `internal/codientcli`) covers the on-screen `(heuristic)` / `(fallback)` / no-tag formatting per source. `TestDisableIntentHeuristic_PersistRoundTrip`, `TestDisableIntentHeuristic_DefaultsToFalse`, and `TestProfileOverride_DisableIntentHeuristic` (in `internal/config`) verify the new config knob.
- New unit tests in `internal/config`: `TestReasoningTier_PersistRoundTrip` round-trips `low_reasoning_max_completion_tokens`; `TestLowReasoningMaxCompletionTokens_Clamp` and `TestLowReasoningMaxCompletionTokens_NegativeFallsToDefault` verify the persisted-value sanitisation (≤0 → unset / default, >2048 → clamped).
- New package **`internal/errorsink`**: `TestDisabled`, `TestSink_OpenAndLogPanic`, `TestSink_ConcurrentLogf`, `TestSink_NilNoOp`. **`internal/agent`:** `TestRunConversation_RecoversPanic` asserts panics become a returned error and invoke **`ErrorLog.LogPanic`**.

### Removed

- **Manual mode flags and slash commands:** the `-mode` and `-no-orchestrator` CLI flags and the `/auto`, `/build` / `/b`, `/ask` / `/a`, `/plan` / `/p`, `/design` / `/d` REPL slash commands have all been removed. Codient is auto-only — `-force` / `-yes` is the only knob (auto-approves the COMPLEX_TASK plan -> build hand-off in non-interactive runs).
- **ACP `session/set_mode` RPC:** removed. Older clients that still send it receive JSON-RPC `-32601` "method not found" so they can fall back gracefully. `session/new` accepts but ignores any legacy `mode` field and always returns `{"mode": "auto"}`.
- **Persisted session mode:** the `last_mode` file and the `mode` field in saved session JSONs are no longer written or honored on resume; every resumed session re-enters auto mode.
- **`config.json` `mode` and per-mode `models` map:** parsed for back-compat (one-time deprecation warning on load) but ignored at runtime. Use `low_reasoning_*` / `high_reasoning_*` instead.
- **`openaiclient.NewForMode`:** replaced by **`openaiclient.NewForTier`** plus tier mapping helpers in `internal/codientcli/modeswitch.go::tierForResolvedMode` and `internal/subagent/subagent.go::tierForMode`.
- **`session.switchMode`:** replaced by the internal-only **`transitionToInternalMode`** (no user-facing chatter; orchestrator drives it directly per turn).

## [0.10.0] - 2026-04-29

### Added

- **ACP stdio** (`-acp`): [Agent Client Protocol](https://agentclientprotocol.com/) over NDJSON on stdin/stdout for editor hosts (e.g. **Codient Unity**). Includes `initialize`, `session/new`, `session/prompt`, **`session/set_model`** (with optional model preload and **`session/model_status`** notifications), **`agent/list_models`**, and Unity-shaped workspaces register **`unity_*`** tools that round-trip **`unity/*`** JSON-RPC to the editor. See [docs/usage.md](docs/usage.md#acp-stdio-agent).
- **Subprocess sandboxing:** configurable `sandbox_mode` (`off`, `native`, `container`, `auto`) with env scrubbing for tools, hooks, MCP, and verification commands. See [docs/configuration.md](docs/configuration.md#subprocess-sandboxing).
- **Repository map:** structural workspace overview (paths + top-level symbols) in the system prompt and **`repo_map`** tool; cache under `<workspace>/.codient/index/repomap.gob`. Config **`repo_map_tokens`** (`0` = auto, `-1` = off).

### Changed

- **Documentation:** user guides live under [`docs/`](docs/README.md); root README is a short entry point.
- **Build-turn git summary:** after a build turn with working-tree changes, stderr shows `git diff --stat` vs `HEAD` and lists untracked files (instead of a large unified diff).

### Fixed

- **ACP:** emit assistant text when stream chunks are absent so clients still receive the full reply.

## [0.9.0] - 2026-04-17

### Added

- **Split-screen Bubble Tea TUI** when stdin is a TTY (scrollable output + fixed input; use `-plain` for classic REPL).
- **Build / lint / test auto-check** sequence after mutating file tools in build mode (`autocheck_cmd`, `lint_cmd`, `test_cmd`; empty = auto-detect, `off` = skip).
- **Checkpoints:** named snapshots of conversation + git state under `<workspace>/.codient/checkpoints/<sessionID>/`; slash commands **`/checkpoint`**, **`/checkpoints`**, **`/rollback`**, **`/fork`**, **`/branches`**. Config **`checkpoint_auto`**: `plan` (default), `all`, or `off`.
- **Lifecycle hooks** and welcome context display (`hooks_enabled`, `hooks.json`); see [docs/context-and-integrations.md](docs/context-and-integrations.md#lifecycle-hooks).

### Changed

- Default auto-commit subject uses **`codient: turn N`** (was `codient: turn 1` for every turn).

## [0.8.0] - 2026-04-16

### Added

- **Headless / CI** mode: **`-print`** / **`-p`** with `-output-format` (`text`, `json`, `stream-json`), `-auto-approve`, `-max-turns`, `-max-cost`.

## [0.7.0] - 2026-04-16

### Added

- **Multimodal images** for vision-capable models (`-image`, `/image`, `@image:` paths).
- **Session token usage and cost estimates** when the API returns usage metadata.

## [0.6.0] - 2026-04-16

### Added

- **Git workflow** in build mode: optional per-turn auto-commits (`git_auto_commit`), protected-branch handling (`git_protected_branches`), **`/diff`**, **`/branch`**, **`/pr`**, and **`create_pull_request`** tool.

## [0.5.0] - 2026-04-15

### Added

- **Sub-agent delegation** (`delegate_task`) with per-mode model configuration.

## [0.4.0] - 2026-04-15

### Added

- **MCP (Model Context Protocol)** client and namespaced tools (`mcp__<server>__<tool>`).

## [0.3.0] - 2026-04-15

### Added

- **Self-update:** interactive prompt on REPL startup and **`-update`** flag.
- **Integration tests** (live API) behind `CODIENT_INTEGRATION`.

## [0.2.0] - 2026-04-14

### Changed

- Install scripts and README refresh; removed outdated env references.

## [0.1.3] - 2026-04-14

### Fixed

- Lint fixes.

## [0.1.2] - 2026-04-14

### Fixed

- Lint fixes.

## [0.1.1] - 2026-04-14

### Changed

- CI workflow updates.

## [0.1.0] - 2026-04-14

### Added

- Initial public release: OpenAI-compatible CLI agent with REPL, ask/plan/build modes, workspace tools (read, grep, patch, shell with allowlist), optional HTTPS fetch, web search, semantic search via embeddings, A2A server mode, session persistence, and project instructions (`AGENTS.md`, `.codient/instructions.md`).

[Unreleased]: https://github.com/magusoftdev/codient/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/magusoftdev/codient/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/magusoftdev/codient/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/magusoftdev/codient/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/magusoftdev/codient/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/magusoftdev/codient/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/magusoftdev/codient/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/magusoftdev/codient/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/magusoftdev/codient/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/magusoftdev/codient/compare/v0.1.0...v0.2.0
[0.1.3]: https://github.com/magusoftdev/codient/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/magusoftdev/codient/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/magusoftdev/codient/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/magusoftdev/codient/releases/tag/v0.1.0
