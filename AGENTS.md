# AGENTS.md

This file defines project-wide engineering rules for humans and coding agents.
If another document conflicts with this file, follow this file for day-to-day
implementation behavior in this repository.

Normative scope:

1. Sections 1-12 are the repository's active, normative policy.
2. Section 13 is migrated reference content preserved for context/history.
3. If Section 13 conflicts with Sections 1-12 or actual repository layout,
   Sections 1-12 and repository reality take precedence.

## 1) Project Scope and Priorities

RobotGo is a cross-platform desktop automation library with Go + CGO backends
for:

- Mouse
- Keyboard
- Screen capture and pixel operations
- Window/process helpers
- Image/bitmap conversion utilities

Current strategic priority on Linux is robust Wayland support while preserving
cross-platform compatibility (macOS, Windows, Linux/X11).

## 2) Core Principles

1. Keep behavior correct before optimizing internals.
2. Prefer explicit capability/unsupported behavior over silent degradation.
3. Keep public API behavior stable unless a change is intentional and documented.
4. Keep platform branches isolated and easy to reason about.
5. Treat tests as required evidence, not optional cleanup.
6. Avoid magic strings in runtime logic; use named constants/enums for env keys,
   backend identifiers, protocol tokens, and resolver markers.

## 3) Linux Display-Server Policy (Wayland-First)

Wayland is the primary Linux target. X11 support remains important, but it is
not the default design anchor for Wayland sessions.

X11 support is required when Linux is running an X11 session.

1. In Wayland sessions (`WAYLAND_DISPLAY` set), features should run directly on
   native Wayland paths whenever possible.
2. Do not route Wayland-primary logic through X11-only helpers.
3. X11 fallback in Wayland sessions is allowed only as a constrained,
   explicit, well-justified fallback.
4. Any fallback order must be intentional, documented, and testable.
5. A code path that appears Wayland-capable but still depends on X11 internals
   is a bug.
6. Wayland-only environments (`WAYLAND_DISPLAY` set, `DISPLAY` unset) are a
   first-class runtime target and must not regress.
7. In X11 sessions (`DISPLAY` set, no Wayland session), X11 behavior must stay
   fully supported and tested.

## 4) Fallback Strategy Rules

When a feature supports multiple Linux backends, keep the decision flow clear:

1. Prefer native backend for the detected session.
2. If native backend fails, use the smallest safe fallback.
3. Log fallback decisions behind existing debug knobs (for example
   `ROBOTGO_CAPTURE_DEBUG=1`).
4. Return explicit errors when no supported backend can satisfy the operation.
5. Never hide backend failures by returning zero-values that look valid.

For screen capture on Linux, preserve and extend the current model:

1. Wayland screencopy first (`dmabuf`/`wl_shm`).
2. Portal fallback when required (`ROBOTGO_FORCE_PORTAL` and
   `ROBOTGO_WAYLAND_BACKEND` overrides remain honored).
3. X11 path for X11 sessions.

## 5) Build Tags and Platform Boundaries

Respect current tag split and do not collapse platform boundaries:

- `linux && wayland`: native Wayland-enabled Linux path
- `linux && !wayland`: X11-focused Linux path
- `linux && portal`: explicit portal package path (`screen/portal`)
- `linux && wayland && test`: tagged Wayland capture/DRM test paths
- `linux && wayland && integration`: integration suites requiring compositor setup
- `cgo && linux && waylandint`: keyboard integration harness
- `linux && !cgo && x11integration`: Pure-Go X11/XTEST input integration suite

Rules:

1. New Linux backend code must land in the correct tag/file split.
2. Non-Linux and non-CGO builds must continue to compile (provide stubs where
   required).
3. Avoid introducing hidden runtime dependencies that only work in one tagged
   build variant unless intentionally scoped and documented.

## 6) C / CGO Integration Rules

This repository includes C protocol and backend code. Changes must be careful
and minimal.

1. Keep C resource ownership explicit (connect/disconnect, alloc/free, destroy).
2. Match existing ownership patterns (`FreeBitmap`, C alloc/free boundaries).
3. Do not leak Wayland/X11 handles, file descriptors, or buffers.
4. Prefer deterministic cleanup over relying on process teardown.
5. Preserve generated protocol file conventions and build guards.
6. If changing protocol-generation flow, keep `wayland_generate.go` and checked
   in generated artifacts consistent.

## 7) API Behavior Contract

1. Maintain existing exported function signatures unless change is intentional.
2. For operations unsupported on Wayland/compositor policy, return explicit
   errors (`ErrNotSupported` and related helpers) rather than pretending success.
3. Keep semantics consistent across equivalent APIs (`GetBounds`/`GetClient`,
   `GetScreenSize`/`GetScreenRect`, etc.).
4. Avoid adding behavior that is ambiguous under multi-output or scale/transform
   scenarios without tests.

## 8) Testing and Validation Requirements

Minimum local validation for meaningful changes:

1. `go test ./...`
2. Relevant tagged suites for changed area (see `TEST.md`)

Repository-default command baseline in this repo currently uses direct `go`
commands (no required root `Makefile` workflow).

Sensitive-data cleanup is mandatory:

1. Default unit tests must not persist the developer's real desktop, clipboard
   contents, input events, OCR inputs, or similar private data.
2. Integration tests and diagnostics that intentionally exercise real private
   data must remove every sensitive artifact on success, failure, timeout, and
   cancellation paths.
3. Prefer in-memory fixtures and `t.TempDir()`/`t.Cleanup()` for unavoidable
   files. Files returned by external backends remain RobotGo's cleanup
   responsibility.
4. Regression tests must verify that sensitive artifacts no longer exist after
   processing, including error paths.

Common targeted suites:

1. `go test -tags "portal" ./screen/portal -v`
2. `go test -tags "wayland test" ./screen -run TestScreencopy -v`
3. `go test -tags "wayland test" . -run TestDrmFindRenderNode -v`
4. `go test -tags "wayland integration" ./mouse ./window -v`
5. `go test -race -tags "waylandint" ./key -v`
6. `go test -race -tags "wayland waylandint" . -run '^TestWaylandPublic' -v`
7. `CGO_ENABLED=0 ROBOTGO_REQUIRE_X11_INTEGRATION=1 xvfb-run -a -s "-screen 0 1280x720x24 -nolisten tcp -noreset" sh -eu -c 'setxkbmap -layout us,de; env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE go test -tags x11integration -run "^TestPureGoX11" -count=1 -timeout=30s -v .'`

Wayland-related code changes should include at least one of:

1. A test in a Wayland-only setting.
2. A regression test proving fallback behavior.
3. A test that verifies explicit unsupported/error contract.

When implementing new or previously missing Wayland functionality, provide all
of the following unless technically impossible:

1. At least one runnable example (or update an existing example) showing usage.
2. Unit tests that validate normal and failure/fallback behavior.
3. Integration tests that exercise real compositor/runtime interaction where
   applicable.

If one layer (example/unit/integration) is intentionally omitted, document why
in the PR and add a follow-up task.

## 9) Screen/Bounds-Specific Guardrails

For functions that expose dimensions or rectangles:

1. Do not depend on X11 helpers inside Wayland-primary branches.
2. Validate non-zero dimensions before accepting backend results.
3. Handle multi-output aggregation correctly when backend provides per-output
   geometry.
4. Keep behavior stable when `displayId` is absent or negative.
5. Add regression coverage when touching bounds logic, especially for
   Wayland-only sessions.

## 10) Documentation and Change Hygiene

The operational branch, pull-request, CI, reviewer, merge, and cleanup loop is
defined in `docs/workflow_conventions.md`. Follow it for normal repository work;
this file remains authoritative for engineering and safety rules.

Any backend behavior change should update affected docs:

1. `README.md` for user-visible backend behavior/env vars.
2. `TEST.md` for new/changed test commands or prerequisites.
3. `docs/wayland-tasks.md` if roadmap status changes.

Keep diffs focused:

1. Do not mix unrelated refactors with backend fixes.
2. Prefer small, auditable commits and clear PR notes.
3. Call out risks, fallbacks, and tested environments in PR description.

Documentation granularity policy:

1. Document only decisions with lasting impact:
   - architecture choices
   - backend selection policy/contracts
   - non-obvious trade-offs/risks
   - API/behavior/test-strategy changes
2. Do not document low-signal details:
   - tiny refactors without behavior change
   - local implementation minutiae
   - temporary intermediate steps
3. Keep docs concise, scannable, and decision-oriented to avoid noise.

## 11) Review Checklist (Required Before Merge)

1. Correct tag/file placement for platform-specific changes.
2. No unintended X11 dependency inside Wayland-primary runtime path.
3. Resource lifecycle validated (no obvious leaks).
4. New/updated tests cover changed behavior and regression risk.
5. User-facing docs/env vars remain accurate.
6. Default and relevant tagged tests pass.

## 12) Quick Decision Guide for Agents

When implementing Linux functionality:

1. Detect session (`Wayland` vs `X11`) early.
2. Implement native Wayland path first.
3. Add explicit fallback only if necessary.
4. Make fallback observable and testable.
5. Preserve cross-platform compile behavior.


## 13) Content Migrated From CRUSH.md (Reference Appendix, Non-Normative)

The following content was migrated from CRUSH.md to keep historical operating
guidance in one canonical file.

This appendix is retained for historical and generic engineering reference. It
is not the authoritative policy for this repository when it conflicts with
Sections 1-12.

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
