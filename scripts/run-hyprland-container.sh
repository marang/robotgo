#!/usr/bin/env bash

set -euo pipefail

if (($# < 1 || $# > 2)); then
	printf 'usage: %s <image> [induced-failure]\n' "$0" >&2
	exit 2
fi

readonly image="$1"
readonly mode="${2:-normal}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}"
: "${ROBOTGO_APPROVED_COMMIT:?ROBOTGO_APPROVED_COMMIT is required}"
: "${ROBOTGO_HYPRLAND_DRM_DEVICE:?ROBOTGO_HYPRLAND_DRM_DEVICE is required}"
: "${ROBOTGO_HYPRLAND_DRM_DRIVER:?ROBOTGO_HYPRLAND_DRM_DRIVER is required}"

if [[ ! "$image" =~ ^robotgo-hyprland-e2e:[0-9a-f]{40}$ ||
	"$mode" != 'normal' && "$mode" != 'induced-failure' ]]; then
	printf 'invalid isolated Hyprland container request\n' >&2
	exit 2
fi
if [[ ! "$RUNNER_TEMP" = /* || "$RUNNER_TEMP" == / ||
	! "$GITHUB_WORKSPACE" = /* || "$GITHUB_WORKSPACE" == / ]]; then
	printf 'isolated Hyprland paths must be non-root absolute paths\n' >&2
	exit 2
fi
if [[ "$(git -C "$GITHUB_WORKSPACE" rev-parse HEAD)" != "$ROBOTGO_APPROVED_COMMIT" ||
	-n "$(git -C "$GITHUB_WORKSPACE" status --porcelain=v1 --untracked-files=all)" ]]; then
	printf 'isolated Hyprland requires a clean exact-commit checkout\n' >&2
	exit 1
fi
if [[ ! "$ROBOTGO_HYPRLAND_DRM_DEVICE" =~ ^/dev/dri/card[0-9]+$ ||
	"$ROBOTGO_HYPRLAND_DRM_DRIVER" != 'vkms' ||
	-L "$ROBOTGO_HYPRLAND_DRM_DEVICE" ||
	! -c "$ROBOTGO_HYPRLAND_DRM_DEVICE" ]]; then
	printf 'isolated Hyprland requires one verified vkms DRM card\n' >&2
	exit 1
fi
drm_sysfs_path="$(
	readlink -f "/sys/class/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
)"
readonly drm_sysfs_path
if [[ ! -d /sys/module/vkms ]]; then
	printf 'isolated Hyprland DRM device is not backed by vkms\n' >&2
	exit 1
fi
readonly exact_drm_sysfs_path="/sys/devices/platform/vkms/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
readonly faux_drm_sysfs_path="/sys/devices/faux/vkms/drm/${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}"
if [[ "$drm_sysfs_path" != "$faux_drm_sysfs_path" &&
	"$drm_sysfs_path" != "$exact_drm_sysfs_path" &&
	"$drm_sysfs_path" != /sys/devices/platform/vkms.*/drm/"${ROBOTGO_HYPRLAND_DRM_DEVICE##*/}" ]]; then
	printf 'isolated Hyprland DRM device is not backed by vkms\n' >&2
	exit 1
fi

readonly container_name="robotgo-hyprland-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-${mode}"
readonly failure_reason_file="$RUNNER_TEMP/hyprland-hyprland-window-failure-reason"
readonly container_runner_temp='/runner-temp'
readonly evidence_directory_name='hyprland-e2e-hyprland-window'
private_runner_temp=''
cleanup() {
	docker rm -f "$container_name" >/dev/null 2>&1 || true
	if [[ -n "$private_runner_temp" &&
		"$private_runner_temp" == "$RUNNER_TEMP"/robotgo-hyprland-container.* ]]; then
		rm -rf -- "$private_runner_temp"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

if [[ -e "$failure_reason_file" || -L "$failure_reason_file" ]]; then
	printf 'isolated Hyprland failure-reason path already exists\n' >&2
	exit 1
fi
private_runner_temp="$(mktemp -d "$RUNNER_TEMP/robotgo-hyprland-container.XXXXXX")"
chmod 700 "$private_runner_temp"

arguments=(
	run
	--rm
	--name "$container_name"
	--network none
	--read-only
	--cap-drop ALL
	--security-opt no-new-privileges
	--pids-limit 1024
	--memory 6g
	--cpus 4
	--user "$(id -u):$(id -g)"
	--device "$ROBOTGO_HYPRLAND_DRM_DEVICE:$ROBOTGO_HYPRLAND_DRM_DEVICE:rwm"
	--tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777,size=256m
	--tmpfs /run:rw,nosuid,nodev,noexec,mode=1777,size=16m
	--volume "$GITHUB_WORKSPACE:/workspace:ro"
	--volume "$private_runner_temp:$container_runner_temp:rw"
	--workdir /workspace
	--env "RUNNER_TEMP=$container_runner_temp"
	--env GITHUB_WORKFLOW
	--env GITHUB_RUN_ID
	--env GITHUB_RUN_ATTEMPT
	--env GITHUB_REF
	--env GITHUB_EVENT_NAME
	--env GITHUB_HEAD_REF
	--env ROBOTGO_APPROVED_COMMIT
	--env ROBOTGO_HYPRLAND_DRM_DEVICE
	--env ROBOTGO_HYPRLAND_DRM_DRIVER
	--env GIT_OPTIONAL_LOCKS=0
	--env ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1
)
if [[ "$mode" == 'induced-failure' ]]; then
	arguments+=(--env ROBOTGO_HYPRLAND_E2E_FAIL_AFTER_START=1)
fi
arguments+=(
	"$image"
	/usr/bin/bash
	./scripts/run-hyprland-e2e.sh
)

if docker "${arguments[@]}"; then
	status=0
else
	status=$?
fi

container_reason_file="$private_runner_temp/${failure_reason_file##*/}"
if ((status != 0)) &&
	[[ -f "$container_reason_file" && ! -L "$container_reason_file" ]]; then
	mv -T -- "$container_reason_file" "$failure_reason_file"
elif ((status != 0)); then
	temporary_reason="$(mktemp "$RUNNER_TEMP/.hyprland-failure-reason.XXXXXX")"
	printf '%s\n' 'container-runtime' >"$temporary_reason"
	chmod 600 "$temporary_reason"
	mv -T -- "$temporary_reason" "$failure_reason_file"
fi

if ((status == 0)) && [[ "$mode" == 'normal' ]]; then
	container_evidence="$private_runner_temp/$evidence_directory_name"
	host_evidence="$RUNNER_TEMP/$evidence_directory_name"
	if [[ -d "$container_evidence" && ! -L "$container_evidence" &&
		! -e "$host_evidence" && ! -L "$host_evidence" ]]; then
		mv -T -- "$container_evidence" "$host_evidence"
	else
		status=1
		temporary_reason="$(mktemp "$RUNNER_TEMP/.hyprland-failure-reason.XXXXXX")"
		printf '%s\n' 'container-transfer' >"$temporary_reason"
		chmod 600 "$temporary_reason"
		mv -T -- "$temporary_reason" "$failure_reason_file"
	fi
fi
cleanup
trap - EXIT INT TERM HUP
exit "$status"
