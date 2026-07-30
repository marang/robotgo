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

shell_recovery_observed=0
shell_recovery_reported=0
session_state_root=/run/robotgo-session-state
shell_recovery_marker=$session_state_root/shell-recovery-attempted
shell_recovery_complete=$session_state_root/shell-recovery-complete
shell_recovery_failed=$session_state_root/shell-recovery-failed
shell_recovery_reset_failed=$session_state_root/shell-reset-failed
shell_recovery_queue_failed=$session_state_root/shell-queue-failed
shell_recovery_start_failed=$session_state_root/shell-start-failed
shell_start_result_prefix=$session_state_root/shell-start-result-
recovery_display_manager_failed=$session_state_root/recovery-display-manager
recovery_runtime_directory_failed=$session_state_root/recovery-runtime-directory
recovery_user_bus_failed=$session_state_root/recovery-user-bus
recovery_wayland_failed=$session_state_root/recovery-wayland
recovery_compositor_failed=$session_state_root/recovery-compositor
recovery_session_failed=$session_state_root/recovery-session
session_ready_marker=$session_state_root/session-ready
session_decision_lock=$session_state_root/session-decision.lock
session_decision_lock_open=0

display_manager_active() {
  timeout --kill-after=1s 2s systemctl is-active --quiet \
    sddm.service >/dev/null 2>&1
}

base_ready() {
  display_manager_active &&
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

shell_unit_failed() {
  timeout --kill-after=1s 2s systemctl --user is-failed --quiet \
    plasma-plasmashell.service >/dev/null 2>&1
}

shell_unit_active() {
  timeout --kill-after=1s 2s systemctl --user is-active --quiet \
    plasma-plasmashell.service >/dev/null 2>&1
}

fail_base_stage() {
  if ! display_manager_active; then
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
  if shell_unit_failed; then
    if ((shell_recovery_observed)) || [[ -d "$shell_recovery_marker" ]]; then
      fail_stage desktop-shell-recovery-exhausted
    fi
    fail_stage desktop-shell-failed
  elif shell_unit_active; then
    fail_stage desktop-shell-process-missing
  else
    fail_stage desktop-shell-unstable
  fi
}

report_shell_recovery() {
  if ((!shell_recovery_reported)); then
    shell_recovery_reported=1
    printf 'ROBOTGO_SESSION_RECOVERY=desktop-shell\n' >&2
  fi
}

acquire_session_decision() {
  if ((!session_decision_lock_open)); then
    exec 9>"$session_decision_lock" ||
      fail_stage session-decision-failed
    session_decision_lock_open=1
  fi
  flock --exclusive --wait 15 9 >/dev/null 2>&1 ||
    fail_stage session-decision-timeout
}

release_session_decision() {
  flock --unlock 9 >/dev/null 2>&1 ||
    fail_stage session-decision-failed
}

require_recovery_base_ready() {
  if [[ -d "$recovery_display_manager_failed" ]]; then
    fail_stage display-manager
  elif [[ -d "$recovery_runtime_directory_failed" ]]; then
    fail_stage runtime-directory
  elif [[ -d "$recovery_user_bus_failed" ]]; then
    fail_stage user-bus
  elif [[ -d "$recovery_wayland_failed" ]]; then
    fail_stage wayland
  elif [[ -d "$recovery_compositor_failed" ]]; then
    fail_stage compositor
  elif [[ -d "$recovery_session_failed" ]]; then
    fail_stage session-unstable
  fi
  if base_ready; then
    return 0
  fi
  if ! display_manager_active; then
    fail_shell_recovery "$recovery_display_manager_failed" display-manager
  elif [[ ! -d /run/user/1100 ]]; then
    fail_shell_recovery \
      "$recovery_runtime_directory_failed" runtime-directory
  elif [[ ! -S /run/user/1100/bus ]]; then
    fail_shell_recovery "$recovery_user_bus_failed" user-bus
  elif [[ ! -S /run/user/1100/wayland-0 ]]; then
    fail_shell_recovery "$recovery_wayland_failed" wayland
  elif ! pgrep -u robotgo -x kwin_wayland >/dev/null 2>&1; then
    fail_shell_recovery "$recovery_compositor_failed" compositor
  else
    fail_shell_recovery "$recovery_session_failed" session-unstable
  fi
}

wait_for_shell_recovery() {
  local stage
  # The recovery winner may spend up to six additional seconds reading the
  # strictly allowlisted Result and ExecMainStatus properties after its
  # 30-second settlement window. Keep observers alive beyond all winner-side
  # bounded work so they consume the same precise shared outcome.
  local deadline=$((SECONDS + 60))
  while ((SECONDS < deadline)); do
    require_recovery_base_ready
    if [[ -d "$shell_recovery_reset_failed" ]]; then
      fail_stage desktop-shell-reset-failed
    fi
    if [[ -d "$shell_recovery_queue_failed" ]]; then
      fail_stage desktop-shell-queue-failed
    fi
    if stage=$(shared_shell_start_failure_stage); then
      fail_stage "$stage"
    fi
    if [[ -d "$shell_recovery_start_failed" ]]; then
      fail_stage desktop-shell-start-failed
    fi
    if [[ -d "$shell_recovery_failed" ]]; then
      fail_stage desktop-shell-recovery-failed
    fi
    if [[ -d "$shell_recovery_complete" ]]; then
      return 0
    fi
    sleep 1
  done
  fail_stage desktop-shell-recovery-failed
}

valid_shell_status() {
  [[ $1 =~ ^[0-9]{1,3}$ ]] && ((10#$1 <= 255))
}

shell_start_failure_stage() {
  local result
  local status
  result=$(timeout --kill-after=1s 2s systemctl --user show \
    --property=Result --value plasma-plasmashell.service 2>/dev/null) ||
    return 1
  case "$result" in
    exit-code)
      status=$(timeout --kill-after=1s 2s systemctl --user show \
        --property=ExecMainStatus --value \
        plasma-plasmashell.service 2>/dev/null) || return 1
      valid_shell_status "$status" || return 1
      printf 'desktop-shell-start-exit-%d\n' "$((10#$status))"
      ;;
    signal)
      status=$(timeout --kill-after=1s 2s systemctl --user show \
        --property=ExecMainStatus --value \
        plasma-plasmashell.service 2>/dev/null) || return 1
      valid_shell_status "$status" || return 1
      printf 'desktop-shell-start-signal-%d\n' "$((10#$status))"
      ;;
    core-dump)
      status=$(timeout --kill-after=1s 2s systemctl --user show \
        --property=ExecMainStatus --value \
        plasma-plasmashell.service 2>/dev/null) || return 1
      valid_shell_status "$status" || return 1
      printf 'desktop-shell-start-core-%d\n' "$((10#$status))"
      ;;
    timeout)
      printf '%s\n' desktop-shell-start-timeout
      ;;
    start-limit-hit)
      printf '%s\n' desktop-shell-start-limit
      ;;
    protocol)
      printf '%s\n' desktop-shell-start-protocol
      ;;
    watchdog)
      printf '%s\n' desktop-shell-start-watchdog
      ;;
    oom-kill)
      printf '%s\n' desktop-shell-start-oom
      ;;
    resources)
      printf '%s\n' desktop-shell-start-resources
      ;;
    *)
      return 1
      ;;
  esac
}

shared_shell_start_failure_stage() {
  local marker
  local stage
  local status
  for stage in \
    desktop-shell-start-timeout \
    desktop-shell-start-limit \
    desktop-shell-start-protocol \
    desktop-shell-start-watchdog \
    desktop-shell-start-oom \
    desktop-shell-start-resources; do
    if [[ -d "$shell_start_result_prefix$stage" ]]; then
      printf '%s\n' "$stage"
      return 0
    fi
  done
  for marker in \
    "$shell_start_result_prefix"desktop-shell-start-exit-* \
    "$shell_start_result_prefix"desktop-shell-start-signal-* \
    "$shell_start_result_prefix"desktop-shell-start-core-*; do
    [[ -d "$marker" ]] || continue
    stage=${marker#"$shell_start_result_prefix"}
    status=${stage##*-}
    valid_shell_status "$status" || continue
    printf '%s\n' "$stage"
    return 0
  done
  return 1
}

fail_shell_recovery() {
  local marker=$1
  local stage=$2
  if ! mkdir -m 0700 "$marker" 2>/dev/null; then
    mkdir -m 0700 "$shell_recovery_failed" 2>/dev/null || true
  fi
  fail_stage "$stage"
}

wait_for_restarted_shell() {
  local deadline=$((SECONDS + 30))
  while ((SECONDS < deadline)); do
    require_recovery_base_ready
    if shell_unit_active &&
      pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
      return 0
    fi
    if shell_unit_failed; then
      return 1
    fi
    sleep 1
  done
  return 1
}

recover_shell_once() {
  local stage
  if ((shell_recovery_observed)); then
    return 1
  fi
  acquire_session_decision
  if [[ -d "$session_ready_marker" ]]; then
    release_session_decision
    return 1
  fi
  if [[ -d "$shell_recovery_marker" ]]; then
    shell_recovery_observed=1
    release_session_decision
    wait_for_shell_recovery
    report_shell_recovery
    return 0
  fi
  if ! shell_unit_failed; then
    release_session_decision
    return 1
  fi
  if ! mkdir -m 0700 "$shell_recovery_marker" 2>/dev/null; then
    shell_recovery_observed=1
    release_session_decision
    wait_for_shell_recovery
    report_shell_recovery
    return 0
  fi
  shell_recovery_observed=1
  release_session_decision
  if ! timeout --kill-after=1s 2s systemctl --user reset-failed \
    plasma-plasmashell.service >/dev/null 2>&1; then
    fail_shell_recovery \
      "$shell_recovery_reset_failed" desktop-shell-reset-failed
  fi
  if ! timeout --kill-after=1s 2s systemctl --user --no-block restart \
    plasma-plasmashell.service >/dev/null 2>&1; then
    fail_shell_recovery \
      "$shell_recovery_queue_failed" desktop-shell-queue-failed
  fi
  if ! wait_for_restarted_shell; then
    if stage=$(shell_start_failure_stage); then
      fail_shell_recovery "$shell_start_result_prefix$stage" "$stage"
    fi
    fail_shell_recovery \
      "$shell_recovery_start_failed" desktop-shell-start-failed
  fi
  if ! mkdir -m 0700 "$shell_recovery_complete" 2>/dev/null; then
    mkdir -m 0700 "$shell_recovery_failed" 2>/dev/null || true
    fail_stage desktop-shell-recovery-failed
  fi
  report_shell_recovery
}

claim_session_ready() {
  # Return 1 only for an unobserved recovery claim and 2 when the locked
  # contract revalidation fails. The caller may restart phase budgets only
  # after return 1 has actually observed recovery completion.
  acquire_session_decision
  if ! all_ready; then
    release_session_decision
    return 2
  fi
  if [[ -d "$session_ready_marker" ]]; then
    release_session_decision
    return 0
  fi
  if [[ -d "$shell_recovery_marker" ]] &&
    ((!shell_recovery_observed)); then
    release_session_decision
    return 1
  fi
  if ! mkdir -m 0700 "$session_ready_marker" 2>/dev/null &&
    [[ ! -d "$session_ready_marker" ]]; then
    release_session_decision
    fail_stage session-decision-failed
  fi
  release_session_decision
}

if [[ ! -d "$session_state_root" ||
      -L "$session_state_root" ||
      ! -O "$session_state_root" ||
      ! -w "$session_state_root" ]]; then
  fail_stage session-decision-failed
fi

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
    if recover_shell_once; then
      deadline=$((SECONDS + 30))
      continue
    fi
    if ((shell_recovery_observed)) ||
      [[ -d "$shell_recovery_marker" ]]; then
      fail_shell_stage
    elif shell_unit_active; then
      fail_stage desktop-shell-process-missing
    fi
    fail_stage desktop-shell-never-seen
  fi
  sleep 1
done

# A recovery during the final full-contract probes must repeat both portal
# phases before stability can pass. The guest-wide marker makes this outer
# cycle repeatable at most once with a mutating restart.
while true; do
  deadline=$((SECONDS + 30))
  while true; do
    require_base_ready
    if ! pgrep -u robotgo -x plasmashell >/dev/null 2>&1; then
      if ((SECONDS >= deadline)); then
        if recover_shell_once; then
          deadline=$((SECONDS + 30))
          continue
        fi
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
        if recover_shell_once; then
          deadline=$((SECONDS + 30))
          continue
        fi
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
        claim_status=0
        if claim_session_ready; then
          claim_status=0
        else
          claim_status=$?
        fi
        if ((claim_status == 1)); then
          if recover_shell_once; then
            continue 2
          fi
          # Another waiter may have completed recovery and claimed readiness
          # between our two serialized decisions. Revalidate exactly once
          # without resetting any phase budget.
          if claim_session_ready; then
            claim_status=0
          else
            claim_status=$?
          fi
        fi
        if ((claim_status != 0)); then
          # Fall through to the existing allowlisted terminal classification.
          # A failed locked revalidation must not reset all phase budgets.
          break
        fi
        if [[ -d "$shell_recovery_complete" ]]; then
          report_shell_recovery
        fi
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
    if recover_shell_once; then
      continue
    fi
    fail_shell_stage
  elif ! portal_ping org.freedesktop.portal.Desktop; then
    fail_stage portal-unstable
  elif ! portal_ping org.freedesktop.impl.portal.desktop.kde; then
    fail_stage portal-backend-unstable
  else
    fail_stage session-unstable
  fi
done
