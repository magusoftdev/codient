# Getting started

## Requirements

- [Go](https://go.dev/dl/) 1.26+ (see `go.mod`)
- A running server exposing OpenAI-style `/v1/chat/completions` (default base URL `http://127.0.0.1:1234/v1`; typical for local stacks)

**Optional:**

- [ast-grep](https://ast-grep.github.io/) — for the `find_references` structural code search tool. Codient auto-detects or offers to download it on first interactive session.
- [Git](https://git-scm.com/) — required for undo, auto-commit, and diff features in a workspace that is a git repository.
- [GitHub CLI](https://cli.github.com/) (`gh`) — optional; required for `/pr` and the `create_pull_request` tool (push + open a PR).

## Install

**macOS / Linux:**

```bash
curl -sSfL https://raw.githubusercontent.com/vaughanb/codient/main/scripts/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/vaughanb/codient/main/scripts/install.ps1 | iex
```

Both scripts detect your OS and architecture, download the latest release binary, and place it on your PATH. Set `CODIENT_INSTALL_DIR` to override the install location (defaults to `~/.local/bin` on Unix, `%LOCALAPPDATA%\codient` on Windows).

**From source** (requires [Go](https://go.dev/dl/) 1.26+):

```bash
go install github.com/vaughanb/codient/cmd/codient@latest
```

Or clone and build with Make:

```bash
git clone https://github.com/vaughanb/codient.git
cd codient
make install   # installs codient to $(go env GOPATH)/bin
```

