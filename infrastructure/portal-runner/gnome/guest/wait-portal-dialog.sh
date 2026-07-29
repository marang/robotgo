#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'error invalid-cell\n'
  exit 1
fi

case "$1" in
  remote-desktop) readonly expected_title="Remote Desktop" ;;
  screencast) readonly expected_title="Share Screen" ;;
  *)
    printf 'error invalid-cell\n'
    exit 1
    ;;
esac

for _ in {1..160}; do
  windows=
  if windows="$(
    busctl --address=unix:path=/run/user/1100/bus --no-pager call \
      org.gnome.Shell.Introspect \
      /org/gnome/Shell/Introspect \
      org.gnome.Shell.Introspect \
      GetWindows 2>/dev/null
  )" &&
    [[ $windows == *"\"title\" s \"$expected_title\""* ]]; then
    printf 'ok\n'
    exit 0
  fi
  sleep 0.25
done

printf 'error dialog-unavailable\n'
exit 1
