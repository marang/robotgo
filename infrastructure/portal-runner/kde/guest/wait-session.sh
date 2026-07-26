#!/usr/bin/env bash
set -euo pipefail

deadline=$((SECONDS + 120))
while ((SECONDS < deadline)); do
  if [[ -S /run/user/1100/wayland-0 &&
        -S /run/user/1100/bus ]] &&
    pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1 &&
    pgrep -u robotgo -x plasmashell >/dev/null 2>&1 &&
    busctl --address=unix:path=/run/user/1100/bus \
      --no-pager --no-legend status org.freedesktop.portal.Desktop \
      >/dev/null 2>&1 &&
    busctl --address=unix:path=/run/user/1100/bus \
      --no-pager --no-legend status org.freedesktop.impl.portal.desktop.kde \
      >/dev/null 2>&1; then
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
elif ! busctl --address=unix:path=/run/user/1100/bus \
  --no-pager --no-legend status org.freedesktop.portal.Desktop \
  >/dev/null 2>&1; then
  stage=portal
elif ! busctl --address=unix:path=/run/user/1100/bus \
  --no-pager --no-legend status org.freedesktop.impl.portal.desktop.kde \
  >/dev/null 2>&1; then
  stage=portal-backend
fi
printf 'ROBOTGO_SESSION_STAGE=%s\n' "$stage" >&2
exit 1
