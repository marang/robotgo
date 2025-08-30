RobotGo is a Go library for desktop automation and control.
Our current goal is to extend RobotGo with Wayland support.

# Go Project Agent Operating Manual

> **Purpose:** This document tells human contributors _and_ code agents how to work on a generic, production-grade Go project consistently and safely. Treat it as the source of truth for conventions, commands, and quality gates.

---

## 0) TL;DR for Agents
- Always start with **`make help`** to discover commands.
- Use **Go `>= 1.24`** and **Go Modules**. Never use GOPATH mode.
- Before making changes, run: **`make setup tidy generate fmt vet lint test`**.
- Keep the build green: **`make verify`** runs the full quality gate.
- When adding code:
  1. Write/extend tests (table-driven, `t.Parallel` where safe).
  2. Keep packages small and cohesive; prefer composition over inheritance.
  3. Propagate `context.Context`; set timeouts at boundaries.
  4. Return errors you can act on; wrap with `%w`; never panic in libraries.
  5. Use structured logging (`log/slog`) and never log secrets.
- When exposing binaries/APIs, add **health**, **metrics**, and **pprof** endpoints.
- Version binaries with `-ldflags "-X <module>/internal/version.Version=$(VERSION)"`.
- **Conventional Commits** and **Semantic Versioning**.
- Open a PR only after **`make verify`** passes locally.

---


**Rules:**
- **`cmd/<app>`** contains only wiring (`main.go`), flags/env parsing, and server start; no business logic.
- **`internal/`** is where 90% of the code lives. Treat packages as layers; do not import upward (acyclic).
- **`pkg/`** is for reusable, stable libraries; breaking changes require semver major bump.
- Split packages by **domain**, not by technical pattern (avoid `utils`/`helpers`).

---

## Toolchain & Baselines
- **Go:** `>= 1.22` (set in `go.mod`).
- **Format & Vet:** `go fmt`, `go vet`.
- **Linter:** `golangci-lint` (pinned via `build` or `tools/tools.go`).
- **Static analysis:** `staticcheck`, `govulncheck`.
- **Security:** `gosec` (advisory), secrets scanning (e.g., `gitleaks`).
- **Testing:** `go test` with `gotestsum` (optional), coverage target >= **80%** where practical.
- **Mocks/Gen:** `go generate` for `mockery`, `stringer`, `protoc`/`buf` if used.
- **Migrations:** `goose` or similar.
- **Release:** `goreleaser` (optional) for multi-platform builds.

Pin auxiliary tools in `tools/tools.go` (example at the end of this file).

---

## Make Targets (Golden Path)

Run **`make help`** for live descriptions. Typical targets:

- **`setup`**: install pinned developer tools locally.
- **`tidy`**: `go mod tidy` (no extraneous deps).
- **`generate`**: run all `go generate ./...` hooks.
- **`fmt`**: format sources.
- **`vet`**: vet analysis.
- **`lint`**: `golangci-lint run` (see `.golangci.yml`).
- **`test`**: unit tests.
- **`it`**: integration tests (require local services).
- **`coverage`**: unit + coverage HTML report.
- **`bench`**: benchmarks (`go test -bench=. -benchmem ./...`).
- **`build`**: build binaries to `./bin`.
- **`run`**: run the primary app locally.
- **`verify`**: `fmt + vet + lint + test + govulncheck` (quality gate).
- **`docker-build`**: multi-stage Docker build.
- **`clean`**: remove build artifacts.

---

## Coding Standards

### Style & Structure
- **Naming:** packages `lowercase`, no underscores; functions and vars `mixedCaps`; keep exported surface minimal.
- **Comments:** exported identifiers must have **GoDoc** comments starting with the name (`Foo does ...`).
- **APIs:** prefer small, focused interfaces defined by the consumer. Avoid “god” interfaces.
- **Generics:** use when it improves safety/clarity; avoid over-generalization.
- **Dependencies:** inject via constructors; avoid global state; prefer `func(ctx context.Context, ...)`.
- **Context:** always the first parameter; do **not** store contexts in structs.
- **Errors:** return sentinel or typed errors; wrap with `%w`; check with `errors.Is/As`; include actionable context (not secrets).
- **Logging:** use `log/slog`:
  - Structured key-values; prefer `Info`, `Error`, `Debug`.
  - Attach request IDs, user IDs, and critical dimensions near the edge.
  - Redact secrets and PII; never log tokens, keys, passwords.
- **Concurrency:** prefer channels when coordinating goroutines; guard shared state with mutexes; avoid goroutine leaks via context cancellation; test with `-race`.
- **I/O & Time:** pass `io.Reader/Writer` and `time.Clock` abstractions for testability. Use `time.Duration` for timeouts.

### HTTP Services (if applicable)
- **Server:** set timeouts: `ReadHeader`, `Read`, `Write`, and `Idle`.
- **Handlers:** accept `context.Context` (via `*http.Request`), validate inputs, and return precise status codes.
- **Middleware:** logging, tracing, request ID, recovery (do not expose internals).
- **Health:** `/healthz` (liveness), `/readyz` (readiness).
- **Observability:** `/metrics` (Prometheus), `/debug/pprof`, tracing (OpenTelemetry).

### gRPC (if applicable)
- Use interceptors for logging, tracing, validation, and auth.
- Enable health and reflection in non-prod only.
- Bound timeouts and message sizes; use context deadlines.

### Data Access
- Use `database/sql` (or a thin wrapper). If using ORM, isolate it in `internal/repo`.
- Use context for all queries; set connection pool limits.
- Migrations live in `/migrations`; run via `make migrate-up/down`.
- Keep SQL in `sqlc` or prepared statements; prefer `sqlc` types for safety.

---

## Testing Policy
- Unit tests are table-driven; use `t.Run` and `t.Parallel` when safe.
- Aim for **80%** package coverage; test public behavior, not private details.
- Use **mocks** and **fakes** behind interfaces; generate with `mockery` where helpful.
- Integration tests live in `/test`; can use Docker services; guard with build tag `integration`.
- Run `-race` in CI for tests; keep flakes out of main.
- Benchmarks belong with the code; compare with previous runs on PRs if possible.
- GUI tests are skipped when no display server is available (`DISPLAY` and `WAYLAND_DISPLAY` unset).

Example test skeleton:

```go
func TestThing_Do(t *testing.T) {
    t.Parallel()
    cases := []struct{
        name string
        in   input
        want output
        err  error
    }{
        {"happy path", input{...}, output{...}, nil},
    }
    for _, tc := range cases {
        tc := tc
        t.Run(tc.name, func(t *testing.T) {
            got, err := Do(tc.in)
            if !errors.Is(err, tc.err) { t.Fatalf("err: %v", err) }
            if diff := cmp.Diff(tc.want, got); diff != "" { t.Fatalf("-want +got\n%s", diff) }
        })
    }
}
```

---

## Configuration & Secrets
- Config via **env vars** and/or **flags**. Example: `APP_HTTP_ADDR`, `APP_DB_DSN`, `APP_LOG_LEVEL`.
- Centralize in `internal/config` with a `Load(ctx)` function; validate values.
- Secrets are read from environment or secret manager; **never commit** secrets.
- Support a `.env` for local only (ignored in VCS).

---

## Security Guidelines
- Run `govulncheck` and `gosec` regularly (part of `make verify`).
- Validate and sanitize all inputs; use whitelists where possible.
- Use constant-time comparisons for secrets (`subtle.ConstantTimeCompare`).
- Prefer `crypto/rand` for key material; avoid weak algorithms.
- Enforce TLS, CSRF protections where relevant (web).
- Avoid reflection-heavy code paths unless measured and justified.

---

## Observability
- **Logging:** consistent fields (`trace_id`, `span_id`, `req_id`, `user_id`).
- **Metrics:** expose Prometheus counters, histograms, and gauges for critical paths.
- **Tracing:** OpenTelemetry SDK + OTLP exporter; instrument inbound/outbound calls.
- **Profiling:** enable `pprof` in non-prod; gate behind auth in prod.

---

## Versioning & Releases
- Semantic Versioning for public modules and binaries.
- Version is injected at build time:
  - `-ldflags "-X your/module/internal/version.Version=$(VERSION)"`.
- Expose `/version` endpoint or `--version` flag.
- Tag releases `vX.Y.Z`; maintain `CHANGELOG.md`.
- Optionally use `goreleaser` for artifacts and checksums.

---

## Git Hygiene & PRs
- **Conventional Commits** (`feat:`, `fix:`, `chore:`, `refactor:`, `docs:`, `test:`).
- Small, focused PRs; include rationale and before/after if applicable.
- Keep vendor out of PRs unless necessary.
- Do not merge with failing checks; require code review.
- PR Checklist:
  - [ ] `make verify` passes locally
  - [ ] Tests added/updated
  - [ ] Docs updated (README/AGENTS/CHANGELOG)
  - [ ] No secrets, no debug prints
  - [ ] Backwards compatibility assessed

---

## Agent Change Protocol
1. **Understand the ask**: restate goal, constraints, and acceptance criteria at the top of the PR description.
2. **Plan**: outline file/package changes; prefer minimal-scope diffs.
3. **Implement**: keep commits logically grouped; update tests alongside code.
4. **Validate**: run `make verify` and relevant integration tests.
5. **Document**: update comments, README sections, and user-facing flags.
6. **Propose**: open PR with risk assessment and rollout plan.

---

## Common Commands

```bash
# Install tools and prep repo
make setup tidy generate fmt vet lint test

# Full gate before PR
make verify

# Run with live reload (if using air or similar)
# air

# Build and run
make build && ./bin/app --help

# Unit + Coverage HTML
make coverage && open ./coverage/index.html

# Docker image
make docker-build
```

---

## Appendix — Example Files

### 13.1 Makefile (reference)
```makefile
SHELL := /usr/bin/env bash
APP    ?= app
BIN    ?= ./bin
PKG    := $(shell go list -m)
VERSION ?= $(shell git describe --tags --always --dirty || echo "dev")
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION)

.PHONY: help
help: ## Show help
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: setup
setup: ## Install dev tools
	@echo "Installing tools..."
	@grep _ tools/tools.go >/dev/null 2>&1 || true
	@go generate ./tools

.PHONY: tidy
tidy: ## Tidy modules
	go mod tidy

.PHONY: generate
generate: ## Run go generate
	go generate ./...

.PHONY: fmt
fmt: ## Format code
	go fmt ./...

.PHONY: vet
vet: ## Vet analysis
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: test
test: ## Run unit tests
	go test -race -count=1 ./...

.PHONY: coverage
coverage: ## Unit tests with coverage HTML
	mkdir -p coverage
	go test -race -covermode=atomic -coverprofile=coverage/cover.out ./...
	go tool cover -html=coverage/cover.out -o coverage/index.html

.PHONY: bench
bench: ## Run benchmarks
	go test -bench=. -benchmem ./...

.PHONY: build
build: ## Build binary
	mkdir -p $(BIN)
	go build -ldflags '$(LDFLAGS)' -o $(BIN)/$(APP) ./cmd/$(APP)

.PHONY: run
run: build ## Build & run the app
	$(BIN)/$(APP)

.PHONY: verify
verify: fmt vet lint test ## Full quality gate
	govulncheck ./...
	gosec -quiet ./... || true

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -f build/Dockerfile -t $(APP):$(VERSION) .

.PHONY: clean
clean: ## Clean artifacts
	rm -rf $(BIN) coverage
```

### `.golangci.yml` (reference)
```yaml
run:
  timeout: 5m
  tests: true

linters:
  enable:
    - govet
    - staticcheck
    - errcheck
    - gofumpt
    - gocyclo
    - revive
    - ineffassign
    - typecheck
    - unparam
    - gosimple
    - prealloc
    - misspell
    - goconst
    - gocritic
    - exportloopref

linters-settings:
  gocyclo:
    min-complexity: 15
  revive:
    rules:
      - name: exported
      - name: var-naming
      - name: if-return
      - name: early-return
      - name: unnecessary-stmt

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - gocyclo
        - goconst
```

### `tools/tools.go` (pin dev tools)
```go
//go:build tools
// +build tools

package tools

import (
    _ "github.com/golangci/golangci-lint/cmd/golangci-lint"
    _ "golang.org/x/vuln/cmd/govulncheck"
    _ "github.com/securego/gosec/v2/cmd/gosec"
    _ "github.com/vektra/mockery/v2"
    _ "golang.org/x/tools/cmd/stringer"
)

// Run `go generate ./tools` from Makefile:setup to install pinned versions.
//go:generate go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
//go:generate go install golang.org/x/vuln/cmd/govulncheck@latest
//go:generate go install github.com/securego/gosec/v2/cmd/gosec@latest
//go:generate go install github.com/vektra/mockery/v2@latest
//go:generate go install golang.org/x/tools/cmd/stringer@latest
```

### `internal/version/version.go`
```go
package version

// Version is injected at build time via -ldflags.
var Version = "dev"
```

### Minimal HTTP server skeleton
```go
package main

import (
    "context"
    "errors"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

    srv := &http.Server{
        Addr:              ":8080",
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       15 * time.Second,
        WriteTimeout:      15 * time.Second,
        IdleTimeout:       60 * time.Second,
        Handler:           routes(logger),
    }

    go func() {
        logger.Info("http: starting", "addr", srv.Addr)
        if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
            logger.Error("http: server error", "err", err)
            os.Exit(1)
        }
    }()

    // Graceful shutdown
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
    <-stop

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Error("http: graceful shutdown failed", "err", err)
    }
    logger.Info("http: stopped")
}

func routes(log *slog.Logger) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })
    return mux
}
```

---

## Non-Goals / Anti-Patterns
- No global singletons (except carefully scoped logger).
- No business logic in HTTP handlers; keep them thin.
- No “utility” dumping-ground packages.
- No panics in libraries; use errors.
- Avoid reflection unless justified and tested.

---

## How to Adapt
This file is a default baseline. Modify sections (tools, CI, layout) to match your team’s stack. Keep the **principles** (context propagation, error handling, tests, observability) intact.

---

_This file is intended to be committed at the repo root and kept current alongside changes to tooling or policies._