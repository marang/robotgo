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
if [[ ! "$runner_temp" = /* || "$runner_temp" == / ||
	"$reason_file" != "$runner_temp"/hyprland-*-failure-reason ||
	"$reason_file" == *'/'*'/'*'/'* ]]; then
	printf 'Hyprland failure-reason path is outside runner temporary storage\n' >&2
	exit 1
fi
if [[ -L "$reason_file" || ! -f "$reason_file" ]]; then
	printf 'Hyprland failure-reason file is unsafe\n' >&2
	exit 1
fi

metadata="$(stat -Lc '%a %s %u' -- "$reason_file")"
readonly metadata
read -r mode size owner <<<"$metadata"
if [[ "$mode" != 600 || ! "$size" =~ ^[0-9]+$ || "$size" -gt 64 ||
	! "$owner" =~ ^[0-9]+$ || "$owner" -ne "$(id -u)" ]]; then
	printf 'Hyprland failure-reason file metadata is unsafe\n' >&2
	exit 1
fi

reason="$(<"$reason_file")"
readonly reason
if [[ -z "$reason" || "$reason" == *$'\n'* || "$reason" == *$'\r'* ]]; then
	printf 'Hyprland failure-reason content is invalid\n' >&2
	exit 1
fi
if ! robotgo_hyprland_failure_stage_is_allowed "$reason"; then
	printf 'Hyprland failure-reason content is invalid\n' >&2
	exit 1
fi

printf 'isolated Hyprland evidence failed at sanitized stage: %s\n' "$reason"

