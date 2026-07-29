#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
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
