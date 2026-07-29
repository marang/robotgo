#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  printf 'error invalid-cell\n'
  exit 1
fi

case "$1" in
  remote-desktop | screencast) ;;
  *)
    printf 'error invalid-cell\n'
    exit 1
    ;;
esac

start_gate="/run/user/1100/robotgo-portal-consent-$1.start"
start_target="/run/user/1100/robotgo-portal-consent-$1.target"
if [[ $2 != "$start_gate" || $3 != "$start_target" ]]; then
  printf 'error invalid-cell\n'
  exit 1
fi

# The integration client creates the exact random Start request path after
# CreateSession and every Select* request has completed, stores it in the
# private target file, then waits on start_gate. Older exported request objects
# are deliberately irrelevant: only this exact path can satisfy readiness.
if [[ ! -f $start_target ]] ||
  [[ $(stat -c '%U:%a:%F' "$start_target" 2>/dev/null) != \
  'robotgo:600:regular file' ]]; then
  printf 'error target-invalid\n'
  exit 1
fi
expected_request=$(<"$start_target")
if [[ ! $expected_request =~ ^/org/freedesktop/portal/desktop/request/[A-Za-z0-9_]+/[A-Za-z0-9_]+$ ]]; then
  printf 'error target-invalid\n'
  exit 1
fi

umask 077
if ! (set -o noclobber; printf 'start\n' > "$start_gate") 2>/dev/null; then
  printf 'error start-gate\n'
  exit 1
fi

for _ in {1..160}; do
  objects=
  if objects="$(
    busctl --address=unix:path=/run/user/1100/bus --no-pager --list tree \
      org.freedesktop.impl.portal.desktop.gnome 2>/dev/null
  )" &&
    grep -Fxq -- "$expected_request" <<<"$objects"; then
    printf 'ok\n'
    exit 0
  fi
  sleep 0.25
done

printf 'error dialog-unavailable\n'
exit 1
