#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
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
if [[ $2 != "$start_gate" ]]; then
  printf 'error invalid-cell\n'
  exit 1
fi

# The integration client creates its private marker only after CreateSession
# and every Select* request has completed, then waits on start_gate. First prove
# all earlier request objects have disappeared; only then release the client to
# issue the dialog-producing Start immediately after consuming the gate.
negotiation_clear=false
for _ in {1..80}; do
  objects=
  if objects="$(
    busctl --address=unix:path=/run/user/1100/bus --no-pager tree \
      org.freedesktop.impl.portal.desktop.gnome 2>/dev/null
  )" &&
    [[ ! $objects =~ /org/freedesktop/portal/desktop/request/[^/[:space:]]+/[^/[:space:]]+ ]]; then
    negotiation_clear=true
    break
  fi
  sleep 0.25
done
if [[ $negotiation_clear != true ]]; then
  printf 'error negotiation-busy\n'
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
    busctl --address=unix:path=/run/user/1100/bus --no-pager tree \
      org.freedesktop.impl.portal.desktop.gnome 2>/dev/null
  )" &&
    [[ $objects =~ /org/freedesktop/portal/desktop/request/[^/[:space:]]+/[^/[:space:]]+ ]]; then
    printf 'ok\n'
    exit 0
  fi
  sleep 0.25
done

printf 'error dialog-unavailable\n'
exit 1
