#!/usr/bin/env bash

set -euo pipefail

readonly repository="marang/robotgo"
readonly remote="origin"
readonly git_bin="${ROBOTGO_RELEASE_GIT_BIN:-git}"
readonly gh_bin="${ROBOTGO_RELEASE_GH_BIN:-gh}"
readonly date_bin="${ROBOTGO_RELEASE_DATE_BIN:-date}"
readonly stable_qualification_release="v1.0.0-rc.2"
# The maintainer waived the seven-day wait on 2026-10-03 (LAB-68).
# Keep fail-closed clock validation; stable cannot precede rc.2 publication.
readonly stable_qualification_not_before="2026-10-03T08:41:23Z"
readonly stable_qualification_not_before_epoch="1791016883"

usage() {
  printf 'usage: %s <tag> <40-character-origin-main-commit>\n' \
    "$(basename -- "$0")" >&2
}

parse_http_date_epoch() {
  local http_date="$1"
  if LC_ALL=C "$date_bin" -u -d "$http_date" +%s 2>/dev/null; then
    return 0
  fi
  LC_ALL=C "$date_bin" -j -u -f "%a, %d %b %Y %H:%M:%S GMT" \
    "$http_date" +%s
}

if (($# != 2)); then
  usage
  exit 2
fi

readonly tag="$1"
readonly expected_commit="$2"
readonly stable_tag="v1.0.0"

if [[ ! "$tag" =~ ^v1\.0\.(0(-rc\.[1-9][0-9]*)?|[1-9][0-9]*)$ ]]; then
  printf 'refusing malformed stable-line tag: %s\n' "$tag" >&2
  exit 1
fi
# Initial RCs/stable must not collide with the first stable publication.
# Maintenance patches inspect only their own identity: v1.0.0 remains immutable.
collision_tags=("$tag")
if [[ "$tag" == "$stable_tag" || "$tag" == v1.0.0-rc.* ]]; then
  collision_tags+=("$stable_tag")
fi
if [[ ! "$expected_commit" =~ ^[0-9a-f]{40}$ ]]; then
  printf 'expected commit must be a lowercase 40-character Git SHA\n' >&2
  exit 1
fi

remote_url="$("$git_bin" remote get-url "$remote")"
case "$remote_url" in
  "git@github.com:${repository}.git" | \
    "ssh://git@github.com/${repository}.git" | \
    "https://github.com/${repository}" | \
    "https://github.com/${repository}.git")
    ;;
  *)
    printf 'refusing non-authoritative release remote %s: %s\n' \
      "$remote" "$remote_url" >&2
    exit 1
    ;;
esac

if [[ "$tag" == "$stable_tag" ]]; then
  if [[ -z "$stable_qualification_not_before" ]] ||
    [[ -z "$stable_qualification_not_before_epoch" ]]; then
    printf 'stable qualification boundary for %s has not been recorded\n' \
      "$stable_qualification_release" >&2
    exit 1
  fi
  if ! github_headers="$(
    "$gh_bin" api "repos/$repository" --include --jq empty
  )"; then
    printf 'failed to obtain authoritative GitHub time for stable qualification\n' >&2
    exit 1
  fi
  github_date="$(
    awk '
      tolower($1) == "date:" {
        sub(/^[^:]*:[[:space:]]*/, "")
        sub(/\r$/, "")
        print
      }
    ' <<<"$github_headers"
  )"
  if [[ -z "$github_date" ]] || [[ "$(wc -l <<<"$github_date")" -ne 1 ]]; then
    printf 'authoritative GitHub response did not contain exactly one Date header\n' >&2
    exit 1
  fi
  if ! current_epoch="$(parse_http_date_epoch "$github_date")"; then
    printf 'failed to parse authoritative GitHub time for stable qualification\n' >&2
    exit 1
  fi
  if [[ ! "$current_epoch" =~ ^[0-9]{1,10}$ ]]; then
    printf 'invalid authoritative GitHub epoch for stable qualification: %s\n' \
      "$current_epoch" >&2
    exit 1
  fi
  if ((10#$current_epoch < stable_qualification_not_before_epoch)); then
    printf 'stable qualification window is still open\n' >&2
    printf 'not-before=%s\ngithub-date=%s\n' \
      "$stable_qualification_not_before" "$github_date" >&2
    exit 1
  fi
fi

main_rows="$("$git_bin" ls-remote --heads "$remote" refs/heads/main)"
main_commit="$(awk '$2 == "refs/heads/main" { print $1 }' <<<"$main_rows")"
if [[ ! "$main_commit" =~ ^[0-9a-f]{40}$ ]] ||
  [[ "$(wc -l <<<"$main_commit")" -ne 1 ]]; then
  printf 'authoritative %s/main did not resolve to exactly one commit\n' \
    "$remote" >&2
  exit 1
fi
if [[ "$main_commit" != "$expected_commit" ]]; then
  printf 'release commit is not authoritative %s/main\n' "$remote" >&2
  printf 'expected: %s\nactual:   %s\n' \
    "$expected_commit" "$main_commit" >&2
  exit 1
fi

if "$git_bin" show-ref --verify --quiet "refs/tags/$tag"; then
  printf 'refusing existing local tag collision: refs/tags/%s\n' "$tag" >&2
  printf 'inspect and delete the non-authoritative local tag before retrying; never force-replace it\n' >&2
  exit 1
else
  local_tag_status=$?
  if ((local_tag_status != 1)); then
    printf 'failed to inspect local tag refs/tags/%s (status %d)\n' \
      "$tag" "$local_tag_status" >&2
    exit 1
  fi
fi

remote_tag_patterns=()
for collision_tag in "${collision_tags[@]}"; do
  remote_tag_patterns+=("refs/tags/$collision_tag" "refs/tags/$collision_tag^{}")
done
remote_tag_rows="$(
  "$git_bin" ls-remote --tags "$remote" "${remote_tag_patterns[@]}"
)"
if [[ -n "$remote_tag_rows" ]]; then
  printf 'refusing existing authoritative origin tag:\n%s\n' \
    "$remote_tag_rows" >&2
  exit 1
fi

tag_filter=".[] | select(.ref == \"refs/tags/$tag\") | .ref"
release_filter=".[] | select(.tag_name == \"$tag\") | .tag_name"
if ((${#collision_tags[@]} > 1)); then
  tag_filter=".[] | select(.ref == \"refs/tags/$tag\" or .ref == \"refs/tags/$stable_tag\") | .ref"
  release_filter=".[] | select(.tag_name == \"$tag\" or .tag_name == \"$stable_tag\") | .tag_name"
fi
github_tag_refs="$(
  "$gh_bin" api "repos/$repository/git/matching-refs/tags/v1.0." \
    --paginate --jq "$tag_filter"
)"
if [[ -n "$github_tag_refs" ]]; then
  printf 'refusing existing GitHub tag ref:\n%s\n' "$github_tag_refs" >&2
  exit 1
fi

github_releases="$(
  "$gh_bin" api "repos/$repository/releases?per_page=100" \
    --paginate --jq "$release_filter"
)"
if [[ -n "$github_releases" ]]; then
  printf 'refusing existing GitHub release:\n%s\n' "$github_releases" >&2
  exit 1
fi

printf 'release preflight passed\n'
printf 'repository=%s\nremote=%s\ntag=%s\ncommit=%s\n' \
  "$repository" "$remote" "$tag" "$expected_commit"
if [[ "$tag" == "$stable_tag" ]]; then
  printf 'stable-qualification-release=%s\n' "$stable_qualification_release"
  printf 'stable-not-before=%s\n' "$stable_qualification_not_before"
fi
printf 'publish only the explicit tag ref; never use git push --tags\n'
