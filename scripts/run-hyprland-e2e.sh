#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly script_dir
# shellcheck source=hyprland-failure-stages.sh
source "$script_dir/hyprland-failure-stages.sh"

readonly runtime_prefix='robotgo-hyprland-runtime'
readonly required_width=1280
readonly required_height=720
readonly cell='hyprland-window'
readonly test_name='TestHyprlandWindowRuntime'

if (($# != 0)); then
	printf 'usage: %s\n' "$0" >&2
	exit 2
fi

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_WORKFLOW:?GITHUB_WORKFLOW is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}"
: "${GITHUB_REF:?GITHUB_REF is required}"
: "${ROBOTGO_APPROVED_COMMIT:?ROBOTGO_APPROVED_COMMIT is required}"

if [[ "$GITHUB_WORKFLOW" != 'Hyprland E2E' ]]; then
	printf 'unexpected workflow identity\n' >&2
	exit 2
fi
if [[ ! "$RUNNER_TEMP" = /* || "$RUNNER_TEMP" == / ]]; then
	printf 'RUNNER_TEMP must be a non-root absolute path\n' >&2
	exit 2
fi
readonly failure_reason_file="$RUNNER_TEMP/hyprland-$cell-failure-reason"
if [[ -e "$failure_reason_file" || -L "$failure_reason_file" ]]; then
	printf 'Hyprland failure-reason path already exists\n' >&2
	exit 2
fi

repo_root="$(git -c safe.directory=/workspace rev-parse --show-toplevel)"
readonly repo_root
cd "$repo_root"
if [[ "$(git -c safe.directory="$repo_root" rev-parse HEAD)" != "$ROBOTGO_APPROVED_COMMIT" ]]; then
	printf 'checked-out commit does not match the approved commit\n' >&2
	exit 1
fi
if [[ -n "$(git -c safe.directory="$repo_root" status --porcelain=v1 --untracked-files=all)" ]]; then
	printf 'Hyprland evidence requires a clean exact-commit checkout\n' >&2
	exit 1
fi

umask 077
ulimit -c 0
runtime_dir=''
session_bus_socket=''
dbus_pid=''
hyprland_pid=''
seatd_pid=''
test_pid=''
failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_DEVICE_CONTRACT"

terminate_group() {
	local pid="$1"
	[[ -n "$pid" ]] || return 0
	if kill -0 -- "-$pid" 2>/dev/null; then
		kill -TERM -- "-$pid" 2>/dev/null || true
		for _ in {1..50}; do
			kill -0 -- "-$pid" 2>/dev/null || break
			sleep 0.1
		done
		kill -KILL -- "-$pid" 2>/dev/null || true
	fi
	wait "$pid" 2>/dev/null || true
}

cleanup() {
	local status=$?
	trap - EXIT INT TERM HUP
	terminate_group "$test_pid"
	terminate_group "$hyprland_pid"
	terminate_group "$seatd_pid"
	terminate_group "$dbus_pid"
	if [[ "$session_bus_socket" == /tmp/dbus-* ]]; then
		rm -f -- "$session_bus_socket"
	fi
	if [[ -n "$runtime_dir" && "$runtime_dir" == "$RUNNER_TEMP/$runtime_prefix".* ]]; then
		rm -rf -- "$runtime_dir"
	fi
	if ((status != 0)); then
		local temporary_reason=''
		if ! robotgo_hyprland_failure_stage_is_allowed "$failure_stage"; then
			exit "$status"
		fi
		temporary_reason="$(mktemp "$RUNNER_TEMP/.hyprland-failure-reason.XXXXXX")" || true
		if [[ -n "$temporary_reason" ]]; then
			printf '%s\n' "$failure_stage" >"$temporary_reason" || true
			chmod 600 "$temporary_reason" 2>/dev/null || true
			if ! mv -fT -- "$temporary_reason" "$failure_reason_file" 2>/dev/null; then
				rm -f -- "$temporary_reason"
			fi
		fi
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

runtime_dir="$(mktemp -d "$RUNNER_TEMP/$runtime_prefix.XXXXXX")"
chmod 700 "$runtime_dir"
mkdir -m 700 "$runtime_dir/home" "$runtime_dir/cache" "$runtime_dir/go-cache" "$runtime_dir/go-tmp"

export XDG_RUNTIME_DIR="$runtime_dir"
export XDG_CACHE_HOME="$runtime_dir/cache"
export HOME="$runtime_dir/home"
export GOCACHE="$runtime_dir/go-cache"
export GOTMPDIR="$runtime_dir/go-tmp"
export XDG_SESSION_TYPE='wayland'
export ROBOTGO_REQUIRE_HYPRLAND_E2E='1'
export ROBOTGO_HYPRLAND_ISOLATED='1'
export ROBOTGO_DISABLE_PORTAL='1'
export HYPRLAND_NO_SD_NOTIFY='1'
export HYPRLAND_NO_SD_VARS='1'
export WLR_LIBINPUT_NO_DEVICES='1'
: "${ROBOTGO_HYPRLAND_DRM_DEVICE:?ROBOTGO_HYPRLAND_DRM_DEVICE is required}"
: "${ROBOTGO_HYPRLAND_DRM_DRIVER:?ROBOTGO_HYPRLAND_DRM_DRIVER is required}"
if [[ ! "$ROBOTGO_HYPRLAND_DRM_DEVICE" =~ ^/dev/dri/card[0-9]+$ ||
	"$ROBOTGO_HYPRLAND_DRM_DRIVER" != 'vkms' ||
	-L "$ROBOTGO_HYPRLAND_DRM_DEVICE" ||
	! -c "$ROBOTGO_HYPRLAND_DRM_DEVICE" ]]; then
	printf 'isolated Hyprland requires one verified vkms DRM card\n' >&2
	exit 1
fi
shopt -s nullglob
drm_entries=(/dev/dri/*)
shopt -u nullglob
if ((${#drm_entries[@]} != 1)) ||
	[[ "${drm_entries[0]}" != "$ROBOTGO_HYPRLAND_DRM_DEVICE" ||
	-e /dev/input ]]; then
	printf 'isolated Hyprland exposes an unexpected device path\n' >&2
	exit 1
fi
drm_sysfs_path="$(
	readlink -f "/sys/class/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
)"
readonly drm_sysfs_path
if [[ ! -d /sys/module/vkms ]]; then
	printf 'isolated Hyprland DRM device is not backed by vkms\n' >&2
	exit 1
fi
readonly exact_drm_sysfs_path="/sys/devices/platform/vkms/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
readonly faux_drm_sysfs_path="/sys/devices/faux/vkms/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
if [[ "$drm_sysfs_path" != "$faux_drm_sysfs_path" &&
	"$drm_sysfs_path" != "$exact_drm_sysfs_path" &&
	"$drm_sysfs_path" != /sys/devices/platform/vkms.*/drm/"${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}" ]]; then
	printf 'isolated Hyprland DRM device is not backed by vkms\n' >&2
	exit 1
fi

export AQ_DRM_DEVICES="$ROBOTGO_HYPRLAND_DRM_DEVICE"
export AQ_NO_MODIFIERS='1'
export LIBSEAT_BACKEND='seatd'
export SEATD_SOCK="$runtime_dir/seatd.sock"
export SEATD_VTBOUND='0'
unset DISPLAY WAYLAND_DISPLAY HYPRLAND_INSTANCE_SIGNATURE SWAYSOCK

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_MACHINE_IDENTITY"
if [[ -L /etc/machine-id || ! -f /etc/machine-id || ! -r /etc/machine-id ]]; then
	printf 'isolated machine identity is unavailable\n' >&2
	exit 1
fi
machine_identity="$(</etc/machine-id)"
readonly machine_identity
if [[ ! "$machine_identity" =~ ^[0-9a-f]{32}$ ]]; then
	printf 'isolated machine identity is invalid\n' >&2
	exit 1
fi

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_SESSION_BUS"
setsid dbus-daemon \
	--session \
	--nofork \
	--nopidfile \
	--nosyslog \
	--print-address=3 \
	3>"$runtime_dir/dbus-address" \
	>"$runtime_dir/dbus.log" 2>&1 &
dbus_pid=$!
for _ in {1..100}; do
	[[ -s "$runtime_dir/dbus-address" ]] && break
	kill -0 "$dbus_pid" 2>/dev/null || break
	sleep 0.05
done
if [[ ! -s "$runtime_dir/dbus-address" ]]; then
	printf 'isolated session bus did not become ready\n' >&2
	exit 1
fi
IFS= read -r session_bus_address <"$runtime_dir/dbus-address"
if [[ ! "$session_bus_address" =~ ^unix:path=(/tmp/dbus-[A-Za-z0-9_-]+),guid=[0-9a-f]{32}$ ]]; then
	printf 'isolated session bus returned an invalid address\n' >&2
	exit 1
fi
session_bus_socket="${BASH_REMATCH[1]}"
if [[ ! -S "$session_bus_socket" ]]; then
	printf 'isolated session bus socket is unavailable\n' >&2
	exit 1
fi
export DBUS_SESSION_BUS_ADDRESS="$session_bus_address"

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_SEAT_MANAGER"
setsid seatd -l error >"$runtime_dir/seatd.log" 2>&1 &
seatd_pid=$!
for _ in {1..100}; do
	[[ -S "$SEATD_SOCK" ]] && break
	kill -0 "$seatd_pid" 2>/dev/null || break
	sleep 0.05
done
if [[ ! -S "$SEATD_SOCK" ]]; then
	printf 'isolated seat manager did not become ready\n' >&2
	exit 1
fi

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_COMPOSITOR_START"
export XDG_CURRENT_DESKTOP='Hyprland'
setsid Hyprland \
	--config "$repo_root/infrastructure/hyprland-runner/hyprland.lua" \
	>"$runtime_dir/hyprland.log" 2>&1 &
hyprland_pid=$!

for _ in {1..200}; do
	wayland_sockets=()
	instance_directories=()
	for candidate in "$runtime_dir"/wayland-*; do
		[[ -S "$candidate" ]] && wayland_sockets+=("$candidate")
	done
	for candidate in "$runtime_dir"/hypr/*; do
		[[ -d "$candidate" ]] && instance_directories+=("$candidate")
	done
	if ((${#wayland_sockets[@]} == 1 && ${#instance_directories[@]} == 1)); then
		export WAYLAND_DISPLAY="${wayland_sockets[0]##*/}"
		export HYPRLAND_INSTANCE_SIGNATURE="${instance_directories[0]##*/}"
		if hyprctl monitors -j >/dev/null 2>&1; then
			break
		fi
	fi
	sleep 0.05
done
if [[ -z "${WAYLAND_DISPLAY:-}" || -z "${HYPRLAND_INSTANCE_SIGNATURE:-}" ]]; then
	printf 'isolated Hyprland did not become ready\n' >&2
	exit 1
fi

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_OUTPUT_TOPOLOGY"
topology_ready=0
for _ in {1..100}; do
	topology="$(hyprctl monitors -j)"
	if jq -e \
		--argjson width "$required_width" \
		--argjson height "$required_height" \
		'length == 1 and
		 (.[0].name | type == "string" and length > 0) and
		 .[0].x == 0 and .[0].y == 0 and
		 .[0].width == $width and .[0].height == $height and
		 .[0].scale == 1' \
		<<<"$topology" >/dev/null; then
		topology_ready=1
		break
	fi
	sleep 0.05
done
if ((topology_ready != 1)); then
	printf 'isolated Hyprland output topology did not become ready\n' >&2
	exit 1
fi
if [[ -e /dev/input || ! -c "$ROBOTGO_HYPRLAND_DRM_DEVICE" ]]; then
	printf 'isolated Hyprland device contract changed during startup\n' >&2
	exit 1
fi

if [[ "${ROBOTGO_HYPRLAND_E2E_FAIL_AFTER_START:-}" == '1' ]]; then
	failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_INDUCED_FAILURE"
	exit 86
fi

if [[ "${GITHUB_EVENT_NAME:-}" == 'pull_request' ]]; then
	: "${GITHUB_HEAD_REF:?GITHUB_HEAD_REF is required for pull requests}"
	evidence_ref="refs/heads/$GITHUB_HEAD_REF"
else
	evidence_ref="$GITHUB_REF"
fi
readonly evidence_ref
readonly output_dir="$RUNNER_TEMP/hyprland-e2e-$cell"

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_PREFLIGHT"
go run ./internal/cmd/compositorevidence preflight \
	-lane wlroots \
	-cell "$cell" \
	-runner-temp "$RUNNER_TEMP" \
	-output-dir "$output_dir" \
	-commit "$ROBOTGO_APPROVED_COMMIT" \
	-expected-commit "$ROBOTGO_APPROVED_COMMIT" \
	-ref "$evidence_ref" \
	-workflow "$GITHUB_WORKFLOW" \
	-run-id "$GITHUB_RUN_ID" \
	-run-attempt "$GITHUB_RUN_ATTEMPT" \
	-output-count 1 \
	-minimum-outputs 1 \
	-require-headless-hyprland

failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_INTEGRATION_TEST"
setsid go test -asan -count=1 -timeout=2m -tags=wayland,hyprlandintegration . \
	-run "^${test_name}$" -v >"$output_dir/raw-test.log" 2>&1 &
test_pid=$!
if wait "$test_pid"; then
	test_status=0
else
	test_status=$?
fi
terminate_group "$test_pid"
test_pid=''

if ((test_status == 0)); then
	failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_FINALIZE"
fi
go run ./internal/cmd/compositorevidence finalize \
	-lane wlroots \
	-cell "$cell" \
	-runner-temp "$RUNNER_TEMP" \
	-output-dir "$output_dir" \
	-expected-commit "$ROBOTGO_APPROVED_COMMIT" \
	-workflow "$GITHUB_WORKFLOW" \
	-run-id "$GITHUB_RUN_ID" \
	-run-attempt "$GITHUB_RUN_ATTEMPT" \
	-test-exit-code "$test_status"
failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_VERIFY"
go run ./internal/cmd/compositorevidence verify \
	-lane wlroots \
	-cell "$cell" \
	-runner-temp "$RUNNER_TEMP" \
	-output-dir "$output_dir" \
	-expected-commit "$ROBOTGO_APPROVED_COMMIT" \
	-workflow "$GITHUB_WORKFLOW" \
	-run-id "$GITHUB_RUN_ID" \
	-run-attempt "$GITHUB_RUN_ATTEMPT"
failure_stage="$ROBOTGO_HYPRLAND_FAILURE_STAGE_SUMMARY"
cat "$output_dir/summary.md" >>"$GITHUB_STEP_SUMMARY"
