#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# CI runs only in a disposable worker or explicitly isolated local harness.
export GOTOOLCHAIN=local GOWORK=off GOENV=off GOCACHEPROG= GOTELEMETRY=off
unset DISPLAY WAYLAND_DISPLAY XDG_SESSION_TYPE DBUS_SESSION_BUS_ADDRESS
[[ $# -eq 1 ]] || { echo "Expected one CI stage" >&2; exit 2; }
case "$1" in
prepare)
mkdir -p "$HOME" "$GOMODCACHE" "$GOCACHE" "$GOTMPDIR" \
  "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME"
test "$(go env GOVERSION)" = go1.26.10
test "$(xkbcli --version)" = 1.6.0
git rev-parse HEAD
test -z "$(git status --porcelain --untracked-files=all)"
  ;;
source_contracts)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go mod verify
actionlint
go run ./internal/cmd/supportmatrix
  ;;
linux_cgo)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go mod download
go build -v .
go test -v ./...
go vet ./...
  ;;
linux_nocgo)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
CGO_ENABLED=0 go build .
env CGO_ENABLED=0 bash -euo pipefail -c 'go test -v ./...'
env CGO_ENABLED=0 bash -euo pipefail -c 'go test -tags ocr ./...'
CGO_ENABLED=0 go vet ./...
  ;;
api_compat)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go run ./internal/cmd/apicompat \
  -variant linux-cgo \
  -variant linux-cgo-wayland \
  -variant linux-cgo-portal \
  -variant linux-cgo-pipewire \
  -variant linux-cgo-full \
  -variant linux-nocgo \
  -variant linux-nocgo-arm64 \
  -variant windows-nocgo \
  -variant windows-nocgo-arm64 \
  -variant darwin-nocgo \
  -variant darwin-nocgo-amd64
  ;;
race)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go test -race ./...
go test -race -count=20 -timeout=2m ./internal/x11input
  ;;
sanitizer)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
export ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1
go test -asan -count=1 ./...
scripts/run-wayland-sanitizer-tests.sh
  ;;
lint)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
CGO_ENABLED=1 golangci-lint run --timeout=5m ./...
CGO_ENABLED=0 golangci-lint run --timeout=5m ./...
  ;;
ocr)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go test -tags ocr ./...
go run ./internal/cmd/apicompat -variant linux-cgo-ocr
  ;;
wayland_keyboard_contracts)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
set -euo pipefail
expected="$(printf '%s\n' \
  TestWaylandVirtualKeyboardASCIIAndUnmappedUnicode \
  TestWaylandExactTextPreflightRejectsUnmappedWithoutMutation \
  TestWaylandKeyboardFlushFailureReconnects \
  TestWaylandKeyboardSelectsDeterministicCapableSeatAndCleansAll \
  TestWaylandKeyboardAcceptsSeatWithoutPhysicalKeyboardCapability \
  TestWaylandKeyboardRejectsMissingSeat \
  TestWaylandKeyboardProcessesRuntimeSeatChanges | LC_ALL=C sort)"
listed="$(go test -tags waylandint ./key -list '^TestWayland')"
printf '%s\n' "$listed"
actual="$(printf '%s\n' "$listed" | awk '/^TestWayland/ { print }' | LC_ALL=C sort)"
if [ "$actual" != "$expected" ]; then
  echo "Wayland keyboard test manifest mismatch" >&2
  printf 'expected:\n%s\nactual:\n%s\n' "$expected" "${actual:-<empty>}" >&2
  exit 1
fi
output="$(go test -race -tags waylandint ./key \
  -run '^TestWayland' -count=1 -timeout=60s -v)"
printf '%s\n' "$output"
while IFS= read -r test_name; do
  if ! printf '%s\n' "$output" | grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
    echo "Wayland keyboard test did not pass: ${test_name}" >&2
    exit 1
  fi
done <<EOF
$expected
EOF
root_expected="$(printf '%s\n' \
  TestWaylandPublicTextPreflightFallsBackToRemoteDesktop \
  TestWaylandPublicTypeStrEUsesExactRunes | LC_ALL=C sort)"
root_listed="$(go test -tags 'wayland waylandint' \
  -list '^TestWaylandPublic' .)"
printf '%s\n' "$root_listed"
root_actual="$(printf '%s\n' "$root_listed" | \
  awk '/^TestWaylandPublic/ { print }' | LC_ALL=C sort)"
if [ "$root_actual" != "$root_expected" ]; then
  echo "public Wayland text test manifest mismatch" >&2
  printf 'expected:\n%s\nactual:\n%s\n' \
    "$root_expected" "${root_actual:-<empty>}" >&2
  exit 1
fi
root_output="$(go test -race -tags 'wayland waylandint' . \
  -run '^TestWaylandPublic' -count=1 -timeout=60s -v)"
printf '%s\n' "$root_output"
while IFS= read -r test_name; do
  if ! printf '%s\n' "$root_output" | \
    grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
    echo "public Wayland text test did not pass: ${test_name}" >&2
    exit 1
  fi
done <<EOF
$root_expected
EOF
  ;;
wayland_tags)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
go test -tags "wayland" ./...
go test -tags "portal" ./screen/portal -v
go test -race -tags "pipewire" ./screen/portal -v
# Compile the consent-driven ScreenCast/PipeWire harness; real runs
# require a graphical desktop and ROBOTGO_SCREENCAST_E2E=1.
go test -tags "pipewire integration" ./screen/portal -run '^$'
# Compile the consent-driven harness; real runs use remote-desktop-e2e.yml.
go test -tags "integration" ./input/portal -run '^$'
go test -tags "wayland test" ./screen -run 'Test(Screencopy|NativeWaylandScrollDirections)' -v
go test -tags "wayland test" . -run 'Test(DrmFindRenderNode|Wayland)' -v
go test -tags "wayland integration" . ./mouse ./window -v
  ;;
wayland_output_integration)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
set -euo pipefail
expected="$(printf '%s\n' \
  TestPureGoWaylandMultiOutputEnumerationWeston \
  TestPureGoWaylandOutputEnumerationWeston | LC_ALL=C sort)"
listed="$(CGO_ENABLED=0 go test -tags "waylandoutputintegration" . \
  -list '^TestPureGoWayland.*EnumerationWeston$')"
printf '%s\n' "$listed"
actual="$(printf '%s\n' "$listed" | \
  awk '/^TestPureGoWayland/ { print }' | LC_ALL=C sort)"
if [ "$actual" != "$expected" ]; then
  echo "Pure-Go Wayland output integration manifest mismatch" >&2
  printf 'expected:\n%s\nactual:\n%s\n' \
    "$expected" "${actual:-<empty>}" >&2
  exit 1
fi
if ! output="$(ROBOTGO_REQUIRE_WAYLAND_OUTPUT_INTEGRATION=1 \
    xvfb-run -a -s '-screen 0 1920x1080x24 -nolisten tcp -noreset' \
    env CGO_ENABLED=0 \
    go test -tags "waylandoutputintegration" . \
      -run '^TestPureGoWayland.*EnumerationWeston$' \
      -count=1 -timeout=30s -v 2>&1)"; then
  printf '%s\n' "$output"
  exit 1
fi
printf '%s\n' "$output"
while IFS= read -r test_name; do
  if ! printf '%s\n' "$output" | \
    grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
    echo "Pure-Go Wayland output integration did not pass: ${test_name}" >&2
    exit 1
  fi
done <<EOF
$expected
EOF
  ;;
x11_purego)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
env CGO_ENABLED=0 ROBOTGO_REQUIRE_X11_INTEGRATION=1 bash -euo pipefail -c 'xvfb-run -a -s "-screen 0 1280x720x24 -nolisten tcp -noreset" \
  sh -eu -c '"'"'
    setxkbmap -layout us,de
    listed="$(env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE \
      go test -tags x11integration -list "^TestPureGoX11" .)"
    printf "%s\n" "$listed"
    expected="$(printf "%s\n" \
      TestPureGoX11RejectsImplicitDependencyPortalFallback \
      TestPureGoX11WindowCapabilitySelection \
      TestPureGoX11WindowCapabilityRejectsWaylandConflict \
      TestPureGoX11MultiLayoutConfiguration \
      TestPureGoX11Capabilities \
      TestPureGoX11PointerInput \
      TestPureGoX11KeyboardInput \
      TestPureGoX11TextReachesDelayedXKBClient \
      TestPureGoX11CrashRestoresScratchAndOwnedInput \
      TestPureGoX11ExplicitShiftReachesXKBClient \
      TestPureGoX11ScratchReservationSkipsPressedEmptyKeycode \
      TestPureGoX11ScratchCleanupCanRetryAfterForeignRelease \
      TestPureGoX11RejectsNonModifierBeforeScratchMutation \
      TestPureGoX11ScratchCleanupCanRetryAfterModifierRestore \
      TestPureGoX11TextCapacityFailsBeforeInput \
      TestPureGoX11RejectsScratchReplacementBeforeTextTap \
      TestPureGoX11ClosePreservesForeignScratchReplacement \
      TestPureGoX11ClosePreservesForeignModifierScratchReplacement \
      TestPureGoX11PreservesForeignInputState \
      TestPureGoX11EventDrainDoesNotStall \
      TestPureGoX11CloseMainDisplayReconnects \
      TestPureGoX11SessionSwitchReleasesOwnedInput \
      TestPureGoX11WindowIntrospectionAndControl | LC_ALL=C sort)"
    actual="$(printf "%s\n" "$listed" | awk "/^TestPureGoX11/ { print }" | LC_ALL=C sort)"
    if [ "$actual" != "$expected" ]; then
      echo "Pure-Go X11 test manifest mismatch" >&2
      printf "expected:\n%s\nactual:\n%s\n" "$expected" "${actual:-<empty>}" >&2
      exit 1
    fi
    output="$(env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE \
      go test -tags x11integration \
      -run "^TestPureGoX11" -count=1 -timeout=30s -v .)"
    printf "%s\n" "$output"
    printf "%s\n" "$expected" | while IFS= read -r test_name; do
      if ! printf "%s\n" "$output" | grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
        echo "Pure-Go X11 test did not pass: ${test_name}" >&2
        exit 1
      fi
    done
  '"'"''
  ;;
x11_backend_evidence)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
env ROBOTGO_X11_EVIDENCE_BALANCED=0 ROBOTGO_X11_EVIDENCE_BENCHTIME=1x ROBOTGO_X11_EVIDENCE_COUNT=1 ROBOTGO_X11_EVIDENCE_TIMEOUT=2m bash -euo pipefail -c 'output="$RUNNER_TEMP/x11-backend-evidence"
scripts/benchmark-x11-backends.sh "$output"
grep -Fxq '"'"'status=complete'"'"' "$output/run-status.txt"
grep -Fxq '"'"'allow_dirty=0'"'"' "$output/run-status.txt"
grep -Fxq '"'"'snapshot_verified=1'"'"' "$output/run-status.txt"
grep -Fxq '"'"'decision_grade=0'"'"' "$output/run-status.txt"
grep -Fq '"'"'**Non-decision configuration:**'"'"' "$output/summary.md"'
env CGO_ENABLED=1 ROBOTGO_EXPECT_X11_IMPLEMENTATION=native-cgo ROBOTGO_EXPECT_X11_NO_XTEST=1 bash -euo pipefail -c 'xvfb-run -a \
  -s '"'"'-screen 0 640x480x24 -nolisten tcp -noreset -extension XTEST'"'"' \
  sh -eu -c '"'"'
    unset WAYLAND_DISPLAY XDG_SESSION_TYPE
    test_name=TestX11MissingXTestReadinessContract
    listed="$(go test -tags x11integration -list "^${test_name}$" .)"
    printf "%s\n" "$listed"
    if ! printf "%s\n" "$listed" | grep -Fxq "$test_name"; then
      echo "missing-XTEST test is absent from the native manifest" >&2
      exit 1
    fi
    output="$(go test -tags x11integration \
      -run "^TestX11MissingXTestReadinessContract$" \
      -count=1 -timeout=30s -v .)"
    printf "%s\n" "$output"
    if ! printf "%s\n" "$output" | grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
      echo "missing-XTEST test did not pass" >&2
      exit 1
    fi
  '"'"''
env CGO_ENABLED=1 bash -euo pipefail -c 'xvfb-run -a \
  -s '"'"'-screen 0 640x480x24 -nolisten tcp -noreset'"'"' \
  sh -eu -c '"'"'
    unset WAYLAND_DISPLAY XDG_SESSION_TYPE
    test_name=TestWaylandBuildPreservesX11Capture
    listed="$(go test -tags "wayland x11integration" \
      -list "^${test_name}$" .)"
    printf "%s\n" "$listed"
    if ! printf "%s\n" "$listed" | grep -Fxq "$test_name"; then
      echo "Wayland-enabled X11 capture test is absent" >&2
      exit 1
    fi
    if ! output="$(go test -tags "wayland x11integration" \
      -run "^${test_name}$" -count=1 -timeout=30s -v .)"; then
      printf "%s\n" "$output"
      exit 1
    fi
    printf "%s\n" "$output"
    if ! printf "%s\n" "$output" | \
      grep -Eq "^--- PASS: ${test_name} \\(.*\\)$"; then
      echo "Wayland-enabled X11 capture test did not pass" >&2
      exit 1
    fi
  '"'"''
  ;;
x11_default_suite)
mkdir -m 700 -p "$XDG_RUNTIME_DIR"
set -euo pipefail
suite_root="$(mktemp -d /tmp/r.XXXXXX)"
cleanup() {
  rm -rf -- "$suite_root"
}
trap cleanup EXIT INT TERM
case "$suite_root" in
  /tmp/r.*) ;;
  *) echo "unsafe X11 suite temporary root" >&2; exit 1 ;;
esac
test "$(stat -c '%a' "$suite_root")" = "700"
export TMPDIR="$suite_root"
xvfb-run -a \
  -e "$suite_root/xvfb.err" \
  -s "-screen 0 1280x720x24 -nolisten tcp -noreset" \
  bash -euo pipefail -c '
    test -n "${DISPLAY:-}"
    test -z "${WAYLAND_DISPLAY:-}"
    test -z "${XDG_SESSION_TYPE:-}"
    xdpyinfo >/dev/null
    go test -v ./...
    output="$(
      go test -count=1 -v \
        -run "^Test(GetScreenSize|GetSysScale|GetTitle)$" .
    )"
    printf "%s\n" "$output"
    for test_name in \
      TestGetScreenSize \
      TestGetSysScale \
      TestGetTitle
    do
      if ! printf "%s\n" "$output" |
        grep -Eq "^--- PASS: ${test_name} \(.*\)$"
      then
        echo "display-aware default test did not pass: ${test_name}" >&2
        exit 1
      fi
    done
    if printf "%s\n" "$output" | grep -Eq "^--- SKIP:"
    then
      echo "display-aware default probe unexpectedly skipped" >&2
      exit 1
    fi
  '
test -z "$(git status --porcelain --untracked-files=all)"
cleanup
trap - EXIT INT TERM
test ! -e "$suite_root"
  ;;
security|dependencies)
mkdir -p "$HOME" "$GOMODCACHE" "$GOCACHE" "$GOTMPDIR" \
  "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME"
git rev-parse HEAD
go version
govulncheck -version
if [[ "$1" == dependencies ]]; then
  echo "stage=go-module-update-inventory (not a vulnerability verdict)" >&2
  go list -mod=readonly -m -u -json all
fi
echo "stage=go-reachable-vulnerabilities (Pure-Go Linux scope)" >&2
govulncheck ./...
  ;;
*) echo "Unknown CI stage: $1" >&2; exit 2 ;;
esac
