#!/usr/bin/env bash
set -euo pipefail

portal_ping() {
  local service=$1
  busctl --address=unix:path=/run/user/1100/bus \
    --no-pager call \
    "$service" \
    /org/freedesktop/portal/desktop \
    org.freedesktop.DBus.Peer \
    Ping \
    >/dev/null 2>&1
}

deadline=$((SECONDS + 120))
while ((SECONDS < deadline)); do
  if [[ -S /run/user/1100/wayland-0 &&
        -S /run/user/1100/bus ]] &&
    pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1 &&
    pgrep -u robotgo -x plasmashell >/dev/null 2>&1 &&
    portal_ping org.freedesktop.portal.Desktop &&
    portal_ping org.freedesktop.impl.portal.desktop.kde; then
    exit 0
  fi
  sleep 1
done

stage=unknown
if ! systemctl is-active --quiet sddm.service; then
  stage=display-manager
elif [[ ! -d /run/user/1100 ]]; then
  stage=runtime-directory
elif [[ ! -S /run/user/1100/bus ]]; then
  stage=user-bus
elif [[ ! -S /run/user/1100/wayland-0 ]]; then
  stage=wayland
elif ! pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1; then
  stage=compositor
elif ! pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
  stage=desktop-shell
elif ! portal_ping org.freedesktop.portal.Desktop; then
  stage=portal
elif ! portal_ping org.freedesktop.impl.portal.desktop.kde; then
  stage=portal-backend
fi
printf 'ROBOTGO_SESSION_STAGE=%s\n' "$stage" >&2
exit 1
