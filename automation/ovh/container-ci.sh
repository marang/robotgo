#!/usr/bin/env bash

# Fixed, image-installed tasks, not a command runner. Host admission owns the
# exact source commit, image ID, resource limits and private report retention.
set -euo pipefail

stage() {
    printf 'stage=%s\n' "$1"
}

# Every expected test must be present and actually PASS. A successful go test
# with zero matching tests, or an integration skip, is not runtime evidence.
require_tests() {
    local pattern="$1" names="$2" listed actual expected log name
    shift 2
    local -a expected_names
    IFS=, read -r -a expected_names <<<"$names"
    expected="$(printf '%s\n' "${expected_names[@]}" | LC_ALL=C sort)"
    listed="$(go test "$@" -list "$pattern")"
    actual="$(printf '%s\n' "$listed" | awk '/^Test/ {print}' | LC_ALL=C sort)"
    if [[ -z "$expected" || "$actual" != "$expected" ]]; then
        printf 'required test manifest mismatch\n' >&2
        return 1
    fi
    log="$(mktemp "$ROBOTGO_CI_PRIVATE_TMPROOT/test-protocol.XXXXXX")"
    go test "$@" -run "$pattern" -count=1 -timeout=2m -v | tee "$log"
    for name in "${expected_names[@]}"; do
        if ! grep -Eq "^--- PASS: ${name} \\(.*\\)$" "$log"; then
            printf 'required test did not pass: %s\n' "$name" >&2
            return 1
        fi
    done
    rm -f -- "$log"
}

x11_default_checks() {
    xdpyinfo >/dev/null
    go test -count=1 -timeout=8m ./...
    require_tests '^Test(GetScreenSize|GetSysScale|GetTitle)$' \
        'TestGetScreenSize,TestGetSysScale,TestGetTitle' .
}

x11_no_xtest_checks() {
    export ROBOTGO_EXPECT_X11_IMPLEMENTATION=native-cgo
    export ROBOTGO_EXPECT_X11_NO_XTEST=1
    require_tests '^TestX11MissingXTestReadinessContract$' \
        TestX11MissingXTestReadinessContract -tags x11integration .
}

x11_wayland_build_checks() {
    require_tests '^TestWaylandBuildPreservesX11Capture$' \
        TestWaylandBuildPreservesX11Capture -tags 'wayland x11integration' .
}

linux_ci() {
    stage xkb-oracle-contract
    if [[ "$(xkbcli --version)" != 1.6.0 ]]; then
        printf 'the admitted XKB oracle 1.6.0 is required\n' >&2
        return 1
    fi
    stage module-integrity
    go mod verify
    stage workflow-syntax
    actionlint
    stage linux-cgo-default
    go build .
    go test -count=1 -timeout=8m ./...
    go vet ./...
    stage linux-purego-default
    CGO_ENABLED=0 go build .
    CGO_ENABLED=0 go test -count=1 -timeout=8m ./...
    CGO_ENABLED=0 go vet ./...
    stage linux-lint
    golangci-lint run --timeout=10m ./...
    CGO_ENABLED=0 golangci-lint run --timeout=10m ./...
    stage runtime-support-and-api
    go run ./internal/cmd/supportmatrix
    go run ./internal/cmd/apicompat \
        -variant linux-cgo -variant linux-cgo-wayland \
        -variant linux-cgo-portal -variant linux-cgo-pipewire \
        -variant linux-cgo-full -variant linux-nocgo \
        -variant linux-nocgo-arm64 -variant windows-nocgo \
        -variant windows-nocgo-arm64 -variant darwin-nocgo \
        -variant darwin-nocgo-amd64
    stage linux-race
    go test -race -count=1 -timeout=8m ./...
    go test -race -count=20 -timeout=2m ./internal/x11input
    stage linux-sanitizers
    export ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1
    go test -asan -count=1 -timeout=8m ./...
    bash scripts/run-wayland-sanitizer-tests.sh
    unset ASAN_OPTIONS
    stage linux-ocr
    go test -tags ocr -count=1 -timeout=8m ./...
    go run ./internal/cmd/apicompat -variant linux-cgo-ocr
    CGO_ENABLED=0 go test -tags ocr -count=1 -timeout=8m ./...
    stage linux-wayland-unit-contracts
    go test -tags wayland -count=1 -timeout=8m ./...
    go test -tags portal -count=1 -timeout=2m ./screen/portal
    go test -race -tags pipewire -count=1 -timeout=2m ./screen/portal
    go test -tags 'wayland test' -count=1 -timeout=2m ./screen \
        -run 'Test(Screencopy|NativeWaylandScrollDirections)' -v
    go test -tags 'wayland test' -count=1 -timeout=2m . \
        -run 'Test(DrmFindRenderNode|Wayland)' -v
    require_tests '^TestWayland' \
        'TestWaylandVirtualKeyboardASCIIAndUnmappedUnicode,TestWaylandExactTextPreflightRejectsUnmappedWithoutMutation,TestWaylandKeyboardFlushFailureReconnects,TestWaylandKeyboardSelectsDeterministicCapableSeatAndCleansAll,TestWaylandKeyboardAcceptsSeatWithoutPhysicalKeyboardCapability,TestWaylandKeyboardRejectsMissingSeat,TestWaylandKeyboardProcessesRuntimeSeatChanges' \
        -race -tags waylandint ./key
    require_tests '^TestWaylandPublic' \
        'TestWaylandPublicTextPreflightFallsBackToRemoteDesktop,TestWaylandPublicTypeStrEUsesExactRunes' \
        -race -tags 'wayland waylandint' .
    stage compositor-and-consent-harness-compile-only
    go test -tags 'pipewire integration' ./screen/portal -run '^$'
    go test -tags integration ./input/portal -run '^$'
    go test -tags 'wayland integration' . ./mouse ./window -run '^$'
    CGO_ENABLED=0 go test -tags waylandoutputintegration . -run '^$'
    stage linux-private-xvfb-default
    xvfb-run -a -s '-screen 0 1280x720x24 -nolisten tcp -noreset' \
        /bin/bash -euo pipefail -c 'source "$1"; x11_default_checks' \
        robotgo-x11 "$pipeline_script"
    stage linux-x11-native-purego-contract-and-benchmark-smoke
    # The existing supervisor checks both complete manifests and requires every
    # test to PASS. This single-sample smoke is NOT decision-grade benchmarking.
    ROBOTGO_X11_EVIDENCE_BALANCED=0 ROBOTGO_X11_EVIDENCE_BENCHTIME=1x \
        ROBOTGO_X11_EVIDENCE_COUNT=1 ROBOTGO_X11_EVIDENCE_TIMEOUT=2m \
        ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU=2 \
        bash scripts/benchmark-x11-backends.sh "$runtime_root/x11-evidence"
    stage linux-x11-no-xtest
    xvfb-run -a -s '-screen 0 640x480x24 -nolisten tcp -noreset -extension XTEST' \
        /bin/bash -euo pipefail -c 'source "$1"; x11_no_xtest_checks' \
        robotgo-x11 "$pipeline_script"
    stage linux-wayland-build-x11-capture
    xvfb-run -a -s '-screen 0 640x480x24 -nolisten tcp -noreset' \
        /bin/bash -euo pipefail -c 'source "$1"; x11_wayland_build_checks' \
        robotgo-x11 "$pipeline_script"
    stage linux-ci-complete
    printf '%s\n' 'scope=Linux-only; no native macOS/Windows, real compositor, portal-consent or release qualification'
}

run_task() (
    set -euo pipefail
    readonly task="$1"
    pipeline_script="$(readlink -f -- "${BASH_SOURCE[0]}")"
    readonly pipeline_script
    readonly pipeline_dir="${pipeline_script%/*}"
    case "$task" in ci|security|dependencies) ;; *) exit 2 ;; esac
    if [[ ! -f go.mod ]] || ! grep -Fxq 'module github.com/marang/robotgo' go.mod; then
        printf 'RobotGo module checkout is required\n' >&2
        exit 2
    fi
    umask 077
    runtime_root="$(mktemp -d "${TMPDIR:-/tmp}/robotgo-ovh.XXXXXX")"
    readonly runtime_root
    cleanup() {
        local status=$? cleanup_failed=0
        trap - EXIT INT TERM HUP
        # Go deliberately makes downloaded module directories read-only. Its
        # scoped cache cleaner makes those removable without chmodding or
        # deleting unrelated paths. Preserve the task verdict if cleanup works.
        if [[ -d "$runtime_root/go/pkg/mod" ]] && \
            ! GOMODCACHE="$runtime_root/go/pkg/mod" go clean -modcache; then
            cleanup_failed=1
        fi
        if ! rm -rf -- "$runtime_root"; then
            cleanup_failed=1
        fi
        if ((cleanup_failed)); then
            printf 'private runtime cleanup failed\n' >&2
            exit 1
        fi
        exit "$status"
    }
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    trap 'exit 129' HUP
    export HOME="$runtime_root/home"
    # This /tmp belongs to the disposable OCI container, never a host mount.
    # Go test names plus QMP/Wayland socket suffixes must fit UNIX's 108 bytes;
    # nesting t.TempDir beneath our private root makes otherwise valid tests
    # exceed that limit. Go temporary directories themselves remain mode 0700.
    export TMPDIR=/tmp ROBOTGO_CI_PRIVATE_TMPROOT="$runtime_root/tmp"
    export XDG_RUNTIME_DIR="$runtime_root/r" XDG_CACHE_HOME="$runtime_root/cache"
    export XDG_CONFIG_HOME="$runtime_root/config" XDG_DATA_HOME="$runtime_root/data"
    export GOPATH="$runtime_root/go" GOMODCACHE="$runtime_root/go/pkg/mod"
    export GOCACHE="$runtime_root/build-cache" GOTOOLCHAIN=local GOTELEMETRY=off
    export GOMAXPROCS=2 GOFLAGS='-p=2 -mod=readonly' CGO_ENABLED=1
    unset DISPLAY WAYLAND_DISPLAY XDG_SESSION_TYPE DBUS_SESSION_BUS_ADDRESS
    mkdir -p -- "$HOME" "$ROBOTGO_CI_PRIVATE_TMPROOT" "$XDG_RUNTIME_DIR" \
        "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$GOPATH" "$GOCACHE"
    # Keep runtime ancestors private, but do not hide intentionally weak 0755
    # permission fixtures from repository tests with an inherited umask 077.
    umask 022
    if [[ "$(go env GOVERSION)" != go1.26.10 ]]; then
        printf 'the admitted Go 1.26.10 toolchain is required\n' >&2
        exit 1
    fi
    # Disposable source may be owned by the host worker rather than image UID.
    git config --global --add safe.directory "$PWD"
    case "$task" in
        ci) linux_ci ;;
        security|dependencies) bash "$pipeline_dir/container-dependencies.sh" "$task" ;;
    esac
)

main() {
    if (($# != 1)); then
        printf 'expected exactly one fixed task: ci, security or dependencies\n' >&2
        return 2
    fi
    case "$1" in ci|security|dependencies) ;; *) return 2 ;; esac
    local script
    script="$(readlink -f -- "${BASH_SOURCE[0]}")"
    # A fixed shell program sources the image-installed implementation. Caller
    # strings are arguments, never shell source; auth, host HOME, display,
    # proxies, Go overrides and exported functions cannot cross this boundary.
    exec /usr/bin/env -i PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin \
        LANG=C.UTF-8 TMPDIR=/tmp \
        /bin/bash -euo pipefail -c 'source "$1"; run_task "$2"' \
        robotgo-task "$script" "$1"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
