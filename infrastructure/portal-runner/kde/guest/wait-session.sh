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

echo "KDE portal session did not become ready" >&2
exit 1
