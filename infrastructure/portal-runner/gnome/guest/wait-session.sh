#!/usr/bin/env bash
set -euo pipefail

portal_ping() {
  busctl --address=unix:path=/run/user/1100/bus \
    --no-pager call \
    org.freedesktop.portal.Desktop \
    /org/freedesktop/portal/desktop \
    org.freedesktop.DBus.Peer \
    Ping \
    >/dev/null 2>&1
}

deadline=$((SECONDS + 120))
while ((SECONDS < deadline)); do
  if [[ -S /run/user/1100/wayland-0 &&
        -S /run/user/1100/bus ]] &&
    portal_ping; then
    exit 0
  fi
  sleep 1
done

stage=unknown
if ! systemctl is-active --quiet gdm3.service; then
  stage=display-manager
elif [[ ! -d /run/user/1100 ]]; then
  stage=runtime-directory
elif [[ ! -S /run/user/1100/bus ]]; then
  stage=user-bus
elif [[ ! -S /run/user/1100/wayland-0 ]]; then
  stage=wayland
elif ! portal_ping; then
  stage=portal
fi
printf 'ROBOTGO_SESSION_STAGE=%s\n' "$stage" >&2
exit 1
