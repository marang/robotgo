#!/usr/bin/env bash
set -euo pipefail

readonly bus_address=unix:path=/run/user/1100/bus
readonly bus_name=io.github.marang.robotgo.KDEPortalGeometry
readonly plugin_name=robotgo-kde-portal-geometry
readonly report_script=/usr/local/libexec/robotgo-runner-report-screencast-geometry
readonly kwin_script=/usr/local/share/robotgo/report-screencast-geometry.js

output=
receiver_pid=
script_id=

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ -n $script_id ]]; then
    busctl --address="$bus_address" --no-pager call \
      org.kde.KWin /Scripting org.kde.kwin.Scripting \
      unloadScript s "$plugin_name" >/dev/null 2>&1 || true
  fi
  if [[ -n $receiver_pid ]]; then
    kill "$receiver_pid" >/dev/null 2>&1 || true
    wait "$receiver_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n $output ]]; then
    rm -f -- "$output"
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'error %s\n' "$1"
  exit 1
}

output=$(mktemp /run/user/1100/robotgo-kde-portal-geometry.XXXXXX)
chmod 0600 "$output"
"$report_script" >"$output" 2>/dev/null &
receiver_pid=$!

receiver_ready=false
for _ in {1..50}; do
  if busctl --address="$bus_address" --no-pager call \
    org.freedesktop.DBus /org/freedesktop/DBus \
    org.freedesktop.DBus NameHasOwner s "$bus_name" 2>/dev/null |
    grep -qx 'b true'; then
    receiver_ready=true
    break
  fi
  if ! kill -0 "$receiver_pid" >/dev/null 2>&1; then
    fail bridge-unavailable
  fi
  sleep 0.1
done
if [[ $receiver_ready != true ]]; then
  fail bridge-unavailable
fi

load_reply=$(
  busctl --address="$bus_address" --no-pager call \
    org.kde.KWin /Scripting org.kde.kwin.Scripting \
    loadScript ss "$kwin_script" "$plugin_name" 2>/dev/null
) || fail compositor-unavailable
if [[ ! $load_reply =~ ^i\ ([0-9]+)$ ]]; then
  fail compositor-unavailable
fi
script_id=${BASH_REMATCH[1]}

busctl --address="$bus_address" --no-pager call \
  org.kde.KWin "/$script_id" org.kde.kwin.Script run \
  >/dev/null 2>&1 || fail compositor-unavailable

for _ in {1..50}; do
  if ! kill -0 "$receiver_pid" >/dev/null 2>&1; then
    wait "$receiver_pid" >/dev/null 2>&1 || true
    receiver_pid=
    test -s "$output" || fail bridge-unavailable
    cat "$output"
    exit 0
  fi
  sleep 0.1
done
fail bridge-unavailable
