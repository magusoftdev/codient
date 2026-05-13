# Codient — Go CLI (module: codient, main: ./cmd/codient)
GO      ?= go
BIN_DIR := bin
EXE     := $(shell $(GO) env GOEXE)
BIN     := $(BIN_DIR)/codient$(EXE)

# Same defaults as scripts/install.sh, scripts/install.ps1, and Codient Unity (override with CODIENT_INSTALL_DIR).
ifeq ($(OS),Windows_NT)
  DEFAULT_INSTALL_DIR := $(LOCALAPPDATA)/codient
else
  DEFAULT_INSTALL_DIR := $(HOME)/.local/bin
endif
ifneq ($(strip $(CODIENT_INSTALL_DIR)),)
  INSTALL_DIR := $(strip $(CODIENT_INSTALL_DIR))
else
  INSTALL_DIR := $(DEFAULT_INSTALL_DIR)
endif

.PHONY: all help build install clean test test-unit test-short test-race test-integration test-integration-strict test-agent-bench test-acp vet fmt mod-tidy check lint govulncheck run release major minor patch terminal-bench-codient

all: build

help:
	@echo "Targets:"
	@echo "  make / make all     Build $(BIN)"
	@echo "  make build          Same as all"
	@echo "  make install        copy $(BIN) to $(INSTALL_DIR) (same as install scripts; CODIENT_INSTALL_DIR overrides)"
	@echo "  make run ARGS='…'   go run ./cmd/codient -- …"
	@echo "  make terminal-bench-codient ARGS='…'  Terminal-Bench full suite with codient (Unix; needs tb + Docker; see docs/terminal-bench.md)"
	@echo "  make test           full suite: unit + live integration (needs model + API; see test-unit for CI)"
	@echo "  make test-unit      unit tests only (go test ./...; no live LLM)"
	@echo "  make test-short     go test -short ./..."
	@echo "  make test-race      go test -race ./..."
	@echo "  make test-integration        live API tests (CODIENT_INTEGRATION=1 only)"
	@echo "  make test-integration-strict live + strict tool tests (+ CODIENT_INTEGRATION_STRICT_TOOLS=1)"
	@echo "  make test-agent-bench        benchmark-style real-LLM CLI scenarios (local OpenAI-compatible model)"
	@echo "  make test-acp                live ACP subprocess tests (Unity-identical JSON-RPC; needs model)"
	@echo "  make vet            go vet ./..."
	@echo "  make fmt            go fmt ./..."
	@echo "  make mod-tidy       go mod tidy"
	@echo "  make lint           golangci-lint run (requires golangci-lint on PATH)"
	@echo "  make govulncheck    vulnerability scan on dependencies (go run)"
	@echo "  make check          vet + test-unit (no live integration; safe for CI)"
	@echo "  make clean          remove $(BIN_DIR)/"
	@echo "  make release [patch]      bump version, commit, tag, push (default: patch)"
	@echo "  make release minor        bump minor version"
	@echo "  make release major        bump major version"

build:
	$(GO) build -o $(BIN) ./cmd/codient

ifeq ($(OS),Windows_NT)
install: build
	powershell -NoProfile -Command "New-Item -ItemType Directory -Force -Path '$(INSTALL_DIR)' | Out-Null; Copy-Item -Force '$(BIN)' -Destination (Join-Path '$(INSTALL_DIR)' 'codient$(EXE)')"
else
install: build
	mkdir -p "$(INSTALL_DIR)"
	cp "$(BIN)" "$(INSTALL_DIR)/codient$(EXE)"
	chmod +x "$(INSTALL_DIR)/codient$(EXE)"
endif

clean:
	$(RM) -r $(BIN_DIR)

# Full test run: integration-tagged tests + env for strict tools and run_command (requires configured model and server).
test: export CODIENT_INTEGRATION = 1
test: export CODIENT_INTEGRATION_STRICT_TOOLS = 1
test: export CODIENT_INTEGRATION_RUN_COMMAND = 1
test:
	$(GO) test -tags=integration -count=1 -timeout 90m ./...

test-unit:
	$(GO) test ./...

test-short:
	$(GO) test -short ./...

test-race:
	$(GO) test -race ./...

test-integration: export CODIENT_INTEGRATION = 1
test-integration:
	$(GO) test -tags=integration -count=1 ./...

test-integration-strict: export CODIENT_INTEGRATION = 1
test-integration-strict: export CODIENT_INTEGRATION_STRICT_TOOLS = 1
test-integration-strict:
	$(GO) test -tags=integration -count=1 ./...

test-agent-bench: export CODIENT_INTEGRATION = 1
test-agent-bench: export CODIENT_INTEGRATION_STRICT_TOOLS = 1
test-agent-bench: export CODIENT_AGENT_BENCH = 1
test-agent-bench:
	$(GO) test -tags=integration -count=1 -timeout 45m ./internal/codientcli/ -run TestAgentBench

# Black-box ACP: spawns codient -acp and drives session/* over stdio (same path as Codient Unity).
test-acp: export CODIENT_INTEGRATION = 1
test-acp: export CODIENT_INTEGRATION_STRICT_TOOLS = 1
test-acp:
	$(GO) test -tags=integration -count=1 -timeout 30m ./internal/codientcli/ -run TestACPIntegration

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

mod-tidy:
	$(GO) mod tidy

lint:
	golangci-lint run ./...

govulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: vet test-unit

run:
	$(GO) run ./cmd/codient -- $(ARGS)

# Terminal-Bench (terminal-bench-core 0.1.1) with the codient adapter. Unix shell only.
# Uses scripts/run-terminal-bench-codient.sh (timeouts, concurrency). Pass extra tb flags: make terminal-bench-codient ARGS='--task-id hello-world'
terminal-bench-codient:
	./scripts/run-terminal-bench-codient.sh $(ARGS)

VERSION_FILE := internal/codientcli/version.go
CUR_VERSION   = $(shell sed -n 's/.*Version = "\(.*\)"/\1/p' $(VERSION_FILE))

ifneq ($(filter major,$(MAKECMDGOALS)),)
  BUMP = major
else ifneq ($(filter minor,$(MAKECMDGOALS)),)
  BUMP = minor
else
  BUMP = patch
endif

major minor patch:
	@:

release:
	@CUR="$(CUR_VERSION)"; \
	MAJOR=$$(echo "$$CUR" | cut -d. -f1); \
	MINOR=$$(echo "$$CUR" | cut -d. -f2); \
	PATCH=$$(echo "$$CUR" | cut -d. -f3); \
	case "$(BUMP)" in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
		*) echo "error: BUMP must be major, minor, or patch"; exit 1 ;; \
	esac; \
	NEXT="$$MAJOR.$$MINOR.$$PATCH"; \
	TAG="v$$NEXT"; \
	echo "Bumping version: $$CUR -> $$NEXT ($$TAG)"; \
	sed -i.bak 's/const Version = ".*"/const Version = "'$$NEXT'"/' $(VERSION_FILE) && rm -f $(VERSION_FILE).bak; \
	git add $(VERSION_FILE); \
	git commit -m "release $$TAG"; \
	git tag "$$TAG"; \
	git push origin HEAD "$$TAG"; \
	echo "Released $$TAG"
