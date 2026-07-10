# Testing Guide

This repository has both default tests and special test suites behind build tags.

## Default Test Suite

Run this first on any platform:

```bash
go test ./...
```

This is the baseline suite used for regular development and should stay green.

The explicit unsupported non-CGO variant is also part of CI:

```bash
CGO_ENABLED=0 go test ./...
```

## Special Test Suites (Build Tags)

Some tests are intentionally gated because they require OS-specific runtime dependencies or mock compositor/server setup.

### `wayland,test`

Purpose:
- Linux Wayland screencopy/mock-server coverage
- DRM helper tests

Typical command:

```bash
go test -tags "wayland test" ./screen -run TestScreencopy -v
go test -tags "wayland test" . -run TestDrmFindRenderNode -v
```

Prerequisites:
- Linux
- CGO enabled
- Wayland dev/runtime deps
- For DRM tests: `/dev/dri` render node access

### `portal`

Purpose:
- Linux portal package tests (`screen/portal`)

Command:

```bash
go test -tags "portal" ./screen/portal -v
```

Prerequisites:
- Linux
- CGO enabled for the optional `CBitmap` adapter
- A live portal is not required; D-Bus behavior is tested hermetically

### `wayland,integration`

Purpose:
- Integration tests in `mouse/wayland_test.go` and `window/wayland_test.go`
- Runtime backend integration tests in root package for Wayland window resolver paths

Command:

```bash
go test -tags "wayland integration" . ./mouse ./window -v
```

Prerequisites:
- Linux
- Wayland runtime available

### RemoteDesktop portal input

Purpose:

- Hermetic RemoteDesktop request/session lifecycle coverage
- Consent response, denial, timeout, portal closure, device grants, and cleanup
- Direct pointer and keyboard notification dispatch
- High-level CGO and non-CGO fallback dispatch after explicit consent

Command:

```bash
go test -race ./input/portal
```

Prerequisites:

- No live portal is required for the hermetic suite.
- The runnable `examples/remote_desktop_input` probe requires Linux plus
  `xdg-desktop-portal` and a backend that implements RemoteDesktop.
- `-connect` and `-demo` may show a consent dialog; `-demo` injects input only
  after approval.

Opt-in real portal lifecycle test:

```bash
ROBOTGO_REMOTE_DESKTOP_E2E=1 go test -tags "integration" ./input/portal -run TestRemoteDesktopPortalRuntime -v
```

The test requests pointer consent and moves the pointer one logical unit out and
back. Default hosted CI only compile-checks this harness because it has no real
desktop consent session. `.github/workflows/remote-desktop-e2e.yml` runs the
test without skipping on explicitly provisioned self-hosted GNOME, KDE, and
wlroots Wayland runners, for both CGO and non-CGO builds. It can be triggered
manually at any time. Set the repository variable
`ROBOTGO_REMOTE_DESKTOP_E2E=1` after those runners are provisioned to run the
same matrix on pull requests and pushes to `main`, where it can be configured as
a required check for branches in this repository. Fork pull requests are
intentionally excluded because untrusted code must never execute on persistent
self-hosted desktop runners. Configure the `remote-desktop-e2e` GitHub
Environment with required reviewers and use ephemeral, network-isolated runners.
The workflow uses read-only permissions, does not persist checkout credentials,
and verifies that each runner's `XDG_CURRENT_DESKTOP` matches its matrix label
before injecting input.

### `waylandint` (Keyboard integration harness)

Purpose:
- Mock Wayland keyboard-server integration tests for Unicode input path
- Files:
  - `key/wayland_integration_test.go`
  - `key/mock_keyboard_server.go`
  - `key/testdata/mock_keyboard_server.c`

Command:

```bash
go test -tags "waylandint" ./key -run TestWaylandUnicodeTypingIntegration -v
```

Prerequisites:
- Linux
- CGO enabled
- Wayland server/client dev libs

Status:
- Experimental tag. Keep this isolated from default CI until the broader Wayland-tagged compile path is fully stabilized in all environments.

## Useful Environment Variables

- `WAYLAND_DISPLAY`
  - Selects Wayland socket in tests that launch/use a mock compositor server.
- `XDG_RUNTIME_DIR`
  - Must be set for Wayland socket creation.
- `ROBOTGO_FORCE_PORTAL=1`
  - Forces portal capture path for Linux capture tests.
- `ROBOTGO_DISABLE_PORTAL=1`
  - Disables portal capture and consent prompts; useful for deterministic native-backend tests.
- `ROBOTGO_WAYLAND_BACKEND`
  - Overrides Linux capture backend selection (`auto|dmabuf|wl_shm|portal`).
- `ROBOTGO_CAPTURE_DEBUG=1`
  - Enables backend/fallback diagnostic logs for capture flow.
- `ROBOTGO_WLROOTS_MINMAX_E2E=1`
  - Opt-in for wlroots active-window minimize/maximize E2E integration (`MinWindowE(0)`, `MaxWindowE(0)`).
- `ROBOTGO_SWAY_TITLE_E2E=1`
  - Opt-in for sway active-window title E2E integration (`GetTitleE`).
- `ROBOTGO_HYPRLAND_TITLE_E2E=1`
  - Opt-in for hyprland active-window title E2E integration (`GetTitleE`).

## Recommended Local Sequence

```bash
go test ./...
CGO_ENABLED=0 go test ./...
go test -tags "wayland" ./...
go test -tags "portal" ./screen/portal -v
go test -tags "wayland integration" . ./mouse ./window -v
```

Run tag-gated suites as needed for the area you changed.

## Sequential Crash-Tracking Run (No Parallelism)

When debugging intermittent crashes/aborts, run tests sequentially and persist
the currently running package:

```bash
set -euo pipefail
export GOCACHE=/tmp/robotgo-gocache
export GOMODCACHE=/tmp/robotgo-gomodcache
mkdir -p "$GOCACHE" "$GOMODCACHE" docs/plan
STATE_FILE="docs/plan/last-running-test.txt"
HIST_FILE="docs/plan/test-run-history.log"
: > "$HIST_FILE"

for pkg in $(go list ./...); do
  ts=$(date -Iseconds)
  printf "%s RUNNING %s\n" "$ts" "$pkg" | tee "$STATE_FILE" | tee -a "$HIST_FILE"
  go test -count=1 -p 1 -parallel 1 "$pkg" 2>&1 | tee -a "$HIST_FILE"
  ts=$(date -Iseconds)
  printf "%s PASS %s\n" "$ts" "$pkg" | tee -a "$HIST_FILE"
done

printf "%s COMPLETE all-packages\n" "$(date -Iseconds)" | tee "$STATE_FILE" | tee -a "$HIST_FILE"
```

After a crash, inspect:
- `docs/plan/last-running-test.txt` for the package that was active.
- `docs/plan/test-run-history.log` for the last emitted test output.
