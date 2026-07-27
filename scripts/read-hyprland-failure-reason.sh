#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly script_dir
# shellcheck source=hyprland-failure-stages.sh
source "$script_dir/hyprland-failure-stages.sh"

if (($# != 2)); then
	printf 'usage: %s <runner-temp> <reason-file>\n' "$0" >&2
	exit 2
fi

readonly runner_temp="$1"
readonly reason_file="$2"
if [[ "$runner_temp" != /* || "$runner_temp" == / ||
	"$reason_file" != "$runner_temp"/hyprland-*-failure-reason ||
	"$(dirname -- "$reason_file")" != "$runner_temp" ]]; then
	printf 'Hyprland failure-reason path is outside runner temporary storage\n' >&2
	exit 1
fi
if [[ "$(readlink -f -- "$runner_temp")" != "$runner_temp" ||
	! -f "$reason_file" || -L "$reason_file" ]]; then
	printf 'Hyprland failure-reason file is unsafe\n' >&2
	exit 1
fi

IFS=: read -r owner mode kind size < <(
	stat -c '%u:%a:%F:%s' -- "$reason_file"
)
if [[ "$owner" != "$(id -u)" || "$mode" != 600 ||
	"$kind" != 'regular file' || ! "$size" =~ ^[1-9][0-9]*$ ||
	"$size" -gt 64 ]]; then
	printf 'Hyprland failure-reason file metadata is unsafe\n' >&2
	exit 1
fi

if [[ "$(od -An -v -t x1 -- "$reason_file")" == *' 00'* ]]; then
	printf 'Hyprland failure-reason content is invalid\n' >&2
	exit 1
fi
mapfile -t lines <"$reason_file"
if ((${#lines[@]} != 1)); then
	printf 'Hyprland failure-reason content is invalid\n' >&2
	exit 1
fi
readonly reason="${lines[0]}"
if ! robotgo_hyprland_failure_stage_is_allowed "$reason"; then
	printf 'Hyprland failure-reason content is invalid\n' >&2
	exit 1
fi

printf 'isolated Hyprland evidence failed at sanitized stage: %s\n' "$reason"
