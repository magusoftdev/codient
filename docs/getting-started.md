# Getting started

Install the **codient** binary using the **[instructions in the main README](../README.md#install)** (release download scripts, `go install`, or `make install` from a clone).

## Requirements

- [Go](https://go.dev/dl/) 1.26+ (see `go.mod`)
- A running server exposing OpenAI-style `/v1/chat/completions` (default base URL `http://127.0.0.1:1234/v1`; typical for local stacks)

**Optional:**

- [ast-grep](https://ast-grep.github.io/) — for the `find_references` structural code search tool. Codient auto-detects or offers to download it on first interactive session.
- [Git](https://git-scm.com/) — required for undo, auto-commit, and diff features in a workspace that is a git repository.
- [GitHub CLI](https://cli.github.com/) (`gh`) — optional; required for `/pr` and the `create_pull_request` tool (push + open a PR).
