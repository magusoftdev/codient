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

## Optional dependencies

**Clipboard image paste** (`/paste`, Ctrl+V in TUI) shells out to platform-specific tools:

| Platform | Tool | Install |
|----------|------|---------|
| Linux (Wayland) | `wl-paste` | `apt install wl-clipboard` / `pacman -S wl-clipboard` |
| Linux (X11) | `xclip` | `apt install xclip` / `pacman -S xclip` |
| macOS | `osascript` | Built-in |
| Windows | `powershell` | Built-in |

The clipboard integration test (`internal/clipboard/integration_test.go`) requires `CODIENT_INTEGRATION=1` and a real clipboard with an image copied.
