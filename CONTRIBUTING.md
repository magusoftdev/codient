# Contributing

## Development setup

- Install [Go](https://go.dev/dl/) 1.26+ (see `go.mod`).
- Clone the repository and run `go install ./cmd/codient` or `make install`.

## Checks before a pull request

```bash
make check        # go vet + unit tests (same as CI core)
make build
make test-race    # race detector (also run in CI)
```

Optional (install tools locally if not using CI):

```bash
make lint         # golangci-lint (requires golangci-lint on PATH)
make govulncheck  # uses `go run` for govulncheck
```

Integration tests that call a live API need a configured model and are not run in default CI:

```bash
make test-integration
```

## Style

- Run `make fmt` before committing.
- Match existing patterns in the package you touch; prefer small, focused changes.

## Issues and pull requests

Use GitHub issues for bugs and feature discussion. Pull requests should describe the change and how you verified it (commands run).
