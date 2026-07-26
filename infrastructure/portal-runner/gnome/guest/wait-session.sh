#!/usr/bin/env bash
set -euo pipefail

deadline=$((SECONDS + 120))
while ((SECONDS < deadline)); do
  if [[ -S /run/user/1100/wayland-0 &&
        -S /run/user/1100/bus ]] &&
    busctl --address=unix:path=/run/user/1100/bus \
      --no-pager --no-legend status org.freedesktop.portal.Desktop \
      >/dev/null 2>&1; then
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
elif ! busctl --address=unix:path=/run/user/1100/bus \
  --no-pager --no-legend status org.freedesktop.portal.Desktop \
  >/dev/null 2>&1; then
  stage=portal
fi
printf 'ROBOTGO_SESSION_STAGE=%s\n' "$stage" >&2
exit 1
