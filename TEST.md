# Testing Guide

This repository has both default tests and special test suites behind build tags.

## Default Test Suite

Run this first on any platform:

```bash
go test ./...
```

This is the baseline suite used for regular development and should stay green.

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
- Portal-related build dependencies (`libportal`, `libpipewire-0.3`)

### `wayland,integration`

Purpose:
- Integration tests in `mouse/wayland_test.go` and `window/wayland_test.go`

Command:

```bash
go test -tags "wayland integration" ./mouse ./window -v
```

Prerequisites:
- Linux
- Wayland runtime available

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
- `ROBOTGO_WAYLAND_BACKEND`
  - Overrides Linux capture backend selection (`auto|dmabuf|wl_shm|portal`).
- `ROBOTGO_CAPTURE_DEBUG=1`
  - Enables backend/fallback diagnostic logs for capture flow.

## Recommended Local Sequence

```bash
go test ./...
go test -tags "portal" ./screen/portal -v
go test -tags "wayland integration" ./mouse ./window -v
```

Run tag-gated suites as needed for the area you changed.
