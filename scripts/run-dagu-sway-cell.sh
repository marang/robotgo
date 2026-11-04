#!/usr/bin/env bash
set -euo pipefail
umask 077

[[ $# -eq 1 ]] || { printf 'Expected one Sway cell\n' >&2; exit 2; }
readonly cell="$1"
case "$cell" in
	native-input|native-capture|native-window|native-output|native-output-multi|portal-availability) ;;
	*) printf 'Unsupported Sway cell\n' >&2; exit 2 ;;
esac

# All session sockets, raw output and private preflight state live in the
# container's tmpfs. Only finalized, verified reports leave it.
RUNNER_TEMP="$(mktemp -d /tmp/robotgo-dagu-sway.XXXXXX)"
export RUNNER_TEMP ROBOTGO_CI_PROVIDER=dagu
cleanup() {
	local status=$?
	trap - EXIT INT TERM HUP
	if [[ "$RUNNER_TEMP" == /tmp/robotgo-dagu-sway.* ]]; then
		rm -rf -- "$RUNNER_TEMP"
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

if [[ "$cell" == native-output-multi ]]; then
	command -v pgrep >/dev/null
	# Exercise the existing failure seam after the real compositor is ready.
	set +e
	ROBOTGO_SWAY_E2E_FAIL_AFTER_START=1 dbus-run-session -- \
		bash scripts/run-sway-e2e.sh "$cell" >"$RUNNER_TEMP/failure.log" 2>&1
	status=$?
	set -e
	[[ "$status" -eq 86 ]] || { printf 'Sway cleanup control did not fail as expected\n' >&2; exit 1; }
	reason_file="$RUNNER_TEMP/sway-$cell-failure-reason"
	reason="$(bash scripts/read-sway-failure-reason.sh "$RUNNER_TEMP" "$reason_file")"
	[[ "$reason" == 'isolated Sway evidence failed at sanitized stage: induced-failure' ]]
	rm -f -- "$reason_file" "$RUNNER_TEMP/failure.log"
	if compgen -G "$RUNNER_TEMP/robotgo-sway-runtime.*" >/dev/null || pgrep -x sway >/dev/null; then
		printf 'Sway cleanup control retained runtime state\n' >&2
		exit 1
	fi
	printf 'Sway induced-failure cleanup passed\n'
fi

dbus-run-session -- bash scripts/run-sway-e2e.sh "$cell"
readonly report="$RUNNER_TEMP/sway-e2e-$cell"
readonly destination="/w/sway-evidence/$cell"
[[ ! -e "$destination" && ! -L "$destination" ]]
mkdir -m 700 -p /w/sway-evidence
cp -aT -- "$report" "$destination"
printf 'Verified Sway report retained: %s\n' "$cell"
