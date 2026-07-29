#!/usr/bin/env bash
set -euo pipefail

portal_ping() {
  local service=$1
  busctl --address=unix:path=/run/user/1100/bus \
    --no-pager --timeout=2s call \
    "$service" \
    /org/freedesktop/portal/desktop \
    org.freedesktop.DBus.Peer \
    Ping \
    >/dev/null 2>&1
}

fail_stage() {
  printf 'ROBOTGO_SESSION_STAGE=%s\n' "$1" >&2
  exit 1
}

base_ready() {
  systemctl is-active --quiet sddm.service &&
    [[ -d /run/user/1100 ]] &&
    [[ -S /run/user/1100/bus ]] &&
    [[ -S /run/user/1100/wayland-0 ]] &&
    pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1
}

all_ready() {
  base_ready &&
    pgrep -u robotgo -x plasmashell >/dev/null 2>&1 &&
    portal_ping org.freedesktop.portal.Desktop &&
    portal_ping org.freedesktop.impl.portal.desktop.kde
}

fail_base_stage() {
  if ! systemctl is-active --quiet sddm.service; then
    fail_stage display-manager
  elif [[ ! -d /run/user/1100 ]]; then
    fail_stage runtime-directory
  elif [[ ! -S /run/user/1100/bus ]]; then
    fail_stage user-bus
  elif [[ ! -S /run/user/1100/wayland-0 ]]; then
    fail_stage wayland
  elif ! pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1; then
    fail_stage compositor
  else
    fail_stage session-unstable
  fi
}

require_base_ready() {
  if ! base_ready; then
    fail_base_stage
  fi
}

fail_shell_stage() {
  if systemctl --user is-failed --quiet plasma-plasmashell.service; then
    fail_stage desktop-shell-failed
  elif systemctl --user is-active --quiet plasma-plasmashell.service; then
    fail_stage desktop-shell-process-missing
  else
    fail_stage desktop-shell-unstable
  fi
}

# Give each startup layer its own bounded budget. A single shared deadline made
# a slow display-manager/compositor startup consume nearly all of the time
# intended for Plasma Shell and misclassified the terminal failure.
deadline=$((SECONDS + 60))
until base_ready; do
  if ((SECONDS >= deadline)); then
    fail_base_stage
  fi
  sleep 1
done

deadline=$((SECONDS + 90))
while true; do
  require_base_ready
  if pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
    break
  fi
  if ((SECONDS >= deadline)); then
    if systemctl --user is-failed --quiet plasma-plasmashell.service; then
      fail_stage desktop-shell-failed
    elif systemctl --user is-active --quiet plasma-plasmashell.service; then
      fail_stage desktop-shell-process-missing
    else
      fail_stage desktop-shell-never-seen
    fi
  fi
  sleep 1
done

deadline=$((SECONDS + 30))
while true; do
  require_base_ready
  if ! pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
    if ((SECONDS >= deadline)); then
      fail_shell_stage
    fi
    sleep 1
    continue
  fi
  if portal_ping org.freedesktop.portal.Desktop; then
    break
  fi
  if ((SECONDS >= deadline)); then
    fail_stage portal
  fi
  sleep 1
done

deadline=$((SECONDS + 30))
while true; do
  require_base_ready
  if ! pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
    if ((SECONDS >= deadline)); then
      fail_shell_stage
    fi
    sleep 1
    continue
  fi
  if ! portal_ping org.freedesktop.portal.Desktop; then
    if ((SECONDS >= deadline)); then
      fail_stage portal-unstable
    fi
    sleep 1
    continue
  fi
  if portal_ping org.freedesktop.impl.portal.desktop.kde; then
    break
  fi
  if ((SECONDS >= deadline)); then
    fail_stage portal-backend
  fi
  sleep 1
done

# Require the complete contract to remain true for consecutive probes. This
# prevents a one-poll process appearance or portal activation from starting
# evidence while the session is still collapsing.
stable=0
deadline=$((SECONDS + 10))
while ((SECONDS < deadline)); do
  if all_ready; then
    ((stable += 1))
    if ((stable >= 3)); then
      exit 0
    fi
  else
    stable=0
  fi
  sleep 1
done

if ! base_ready; then
  fail_base_stage
elif ! pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
  fail_shell_stage
elif ! portal_ping org.freedesktop.portal.Desktop; then
  fail_stage portal-unstable
elif ! portal_ping org.freedesktop.impl.portal.desktop.kde; then
  fail_stage portal-backend-unstable
else
  fail_stage session-unstable
fi
