#!/usr/bin/env bash

set -euo pipefail

readonly runtime_prefix='robotgo-sway-runtime'
readonly required_width=1280
readonly required_height=720
readonly secondary_mode_width=1200
readonly secondary_mode_height=800
readonly secondary_x=-600
readonly secondary_y=0
readonly secondary_scale=2
readonly secondary_transform=90

if (($# != 1)); then
	printf 'usage: %s <evidence-cell>\n' "$0" >&2
	exit 2
fi

readonly cell="$1"
output_count=1
case "$cell" in
	native-input) test_name='TestSwayNativeInputRuntime' ;;
	native-capture) test_name='TestSwayNativeCaptureRuntime' ;;
	native-window) test_name='TestSwayNativeWindowRuntime' ;;
	native-output) test_name='TestSwayNativeOutputRuntime' ;;
	native-output-multi)
		test_name='TestSwayNativeOutputMultiRuntime'
		output_count=2
		;;
	portal-availability) test_name='TestSwayPortalAvailabilityRuntime' ;;
	*)
		printf 'unsupported Sway evidence cell: %s\n' "$cell" >&2
		exit 2
		;;
esac
readonly test_name
readonly output_count

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_WORKFLOW:?GITHUB_WORKFLOW is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}"
: "${GITHUB_REF:?GITHUB_REF is required}"
: "${ROBOTGO_APPROVED_COMMIT:?ROBOTGO_APPROVED_COMMIT is required}"

if [[ "$GITHUB_WORKFLOW" != 'Sway E2E' ]]; then
	printf 'unexpected workflow identity\n' >&2
	exit 2
fi
if [[ ! "$RUNNER_TEMP" = /* || "$RUNNER_TEMP" == / ]]; then
	printf 'RUNNER_TEMP must be a non-root absolute path\n' >&2
	exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
readonly repo_root
cd "$repo_root"
if [[ "$(git rev-parse HEAD)" != "$ROBOTGO_APPROVED_COMMIT" ]]; then
	printf 'checked-out commit does not match the approved commit\n' >&2
	exit 1
fi
if [[ -n "$(git status --porcelain=v1 --untracked-files=all)" ]]; then
	printf 'Sway evidence requires a clean exact-commit checkout\n' >&2
	exit 1
fi

umask 077
runtime_dir=''
sway_pid=''
test_pid=''

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
	terminate_group "$sway_pid"
	if [[ -n "$runtime_dir" && "$runtime_dir" == "$RUNNER_TEMP/$runtime_prefix".* ]]; then
		rm -rf -- "$runtime_dir"
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

runtime_dir="$(mktemp -d "$RUNNER_TEMP/$runtime_prefix.XXXXXX")"
chmod 700 "$runtime_dir"

export XDG_RUNTIME_DIR="$runtime_dir"
export WLR_BACKENDS='headless'
export WLR_RENDERER='pixman'
export WLR_RENDERER_ALLOW_SOFTWARE='1'
export WLR_LIBINPUT_NO_DEVICES='1'
export XDG_SESSION_TYPE='wayland'
export XDG_CURRENT_DESKTOP='sway'
export ROBOTGO_REQUIRE_SWAY_E2E='1'
export ROBOTGO_SWAY_ISOLATED='1'
export ROBOTGO_DISABLE_PORTAL='1'
unset DISPLAY WAYLAND_DISPLAY SWAYSOCK

setsid sway -c /dev/null >/dev/null 2>&1 &
sway_pid=$!

for _ in {1..100}; do
	wayland_sockets=()
	sway_sockets=()
	for candidate in "$runtime_dir"/wayland-*; do
		[[ -S "$candidate" ]] && wayland_sockets+=("$candidate")
	done
	for candidate in "$runtime_dir"/sway-ipc.*.sock; do
		[[ -S "$candidate" ]] && sway_sockets+=("$candidate")
	done
	if ((${#wayland_sockets[@]} == 1 && ${#sway_sockets[@]} == 1)); then
		export WAYLAND_DISPLAY="${wayland_sockets[0]##*/}"
		export SWAYSOCK="${sway_sockets[0]}"
		if swaymsg -t get_outputs -r >/dev/null 2>&1; then
			break
		fi
	fi
	sleep 0.05
done
if [[ -z "${WAYLAND_DISPLAY:-}" || -z "${SWAYSOCK:-}" ]]; then
	printf 'isolated Sway did not become ready\n' >&2
	exit 1
fi

if ((output_count == 2)); then
	swaymsg create_output >/dev/null
fi
output_names=()
for _ in {1..50}; do
	mapfile -t output_names < <(
		swaymsg -t get_outputs -r |
			jq -er --argjson count "$output_count" \
				'[.[] | select(.active)] | select(length == $count) | sort_by(.name) | .[].name'
	)
	((${#output_names[@]} == output_count)) && break
	sleep 0.05
done
if ((${#output_names[@]} != output_count)); then
	printf 'isolated Sway output count did not become ready\n' >&2
	exit 1
fi
for output_name in "${output_names[@]}"; do
	if [[ ! "$output_name" =~ ^HEADLESS-[0-9]+$ ]]; then
		printf 'isolated Sway exposed an unexpected output\n' >&2
		exit 1
	fi
done
readonly output_names
readonly primary_output="${output_names[0]}"
swaymsg \
	"output $primary_output mode ${required_width}x${required_height} pos 0 0 scale 1 transform normal" \
	>/dev/null
if ((output_count == 2)); then
	readonly secondary_output="${output_names[1]}"
	swaymsg \
		"output $secondary_output mode ${secondary_mode_width}x${secondary_mode_height} pos $secondary_x $secondary_y scale $secondary_scale transform $secondary_transform" \
		>/dev/null
fi
topology_ready=0
for _ in {1..50}; do
	topology="$(
		swaymsg -t get_outputs -r |
			jq -ec '[.[] | select(.active) | {name, rect, scale, transform}] | sort_by(.name)'
	)"
	if ((output_count == 1)); then
		jq -e --arg primary "$primary_output" \
			--argjson width "$required_width" --argjson height "$required_height" \
			'length == 1 and .[0].name == $primary and
			 .[0].rect == {x: 0, y: 0, width: $width, height: $height} and
			 .[0].scale == 1 and .[0].transform == "normal"' \
			<<<"$topology" >/dev/null && topology_ready=1
	else
		jq -e --arg primary "$primary_output" --arg secondary "$secondary_output" \
			--argjson width "$required_width" --argjson height "$required_height" \
			--argjson secondary_x "$secondary_x" --argjson secondary_y "$secondary_y" \
			--argjson secondary_width "$((secondary_mode_height / secondary_scale))" \
			--argjson secondary_height "$((secondary_mode_width / secondary_scale))" \
			--argjson secondary_scale "$secondary_scale" --arg secondary_transform "$secondary_transform" \
			'length == 2 and .[0].name == $primary and .[1].name == $secondary and
			 .[0].rect == {x: 0, y: 0, width: $width, height: $height} and
			 .[0].scale == 1 and .[0].transform == "normal" and
			 .[1].rect == {x: $secondary_x, y: $secondary_y, width: $secondary_width, height: $secondary_height} and
			 .[1].scale == $secondary_scale and .[1].transform == $secondary_transform' \
			<<<"$topology" >/dev/null && topology_ready=1
	fi
	((topology_ready == 1)) && break
	sleep 0.05
done
if ((topology_ready != 1)); then
	printf 'isolated Sway output topology did not become ready\n' >&2
	exit 1
fi

# The workflow invokes this only as a negative cleanup assertion. It fails
# after the compositor is live, so the EXIT trap must terminate Sway and remove
# its complete private runtime directory.
if [[ "${ROBOTGO_SWAY_E2E_FAIL_AFTER_START:-}" == '1' ]]; then
	exit 86
fi

if [[ "${GITHUB_EVENT_NAME:-}" == 'pull_request' ]]; then
	: "${GITHUB_HEAD_REF:?GITHUB_HEAD_REF is required for pull requests}"
	evidence_ref="refs/heads/$GITHUB_HEAD_REF"
else
	evidence_ref="$GITHUB_REF"
fi
readonly evidence_ref
readonly output_dir="$RUNNER_TEMP/sway-e2e-$cell"

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
	-output-count "$output_count" \
	-minimum-outputs "$output_count" \
	-require-headless-sway

setsid go test -count=1 -timeout=2m -tags=wayland,swayintegration . \
	-run "^${test_name}$" -v >"$output_dir/raw-test.log" 2>&1 &
test_pid=$!
if wait "$test_pid"; then
	test_status=0
else
	test_status=$?
fi
terminate_group "$test_pid"
test_pid=''

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
go run ./internal/cmd/compositorevidence verify \
	-lane wlroots \
	-cell "$cell" \
	-runner-temp "$RUNNER_TEMP" \
	-output-dir "$output_dir" \
	-expected-commit "$ROBOTGO_APPROVED_COMMIT" \
	-workflow "$GITHUB_WORKFLOW" \
	-run-id "$GITHUB_RUN_ID" \
	-run-attempt "$GITHUB_RUN_ATTEMPT"
cat "$output_dir/summary.md" >>"$GITHUB_STEP_SUMMARY"
