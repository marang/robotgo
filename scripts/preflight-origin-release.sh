#!/usr/bin/env bash

set -euo pipefail

readonly repository="marang/robotgo"
readonly remote="origin"
readonly git_bin="${ROBOTGO_RELEASE_GIT_BIN:-git}"
readonly gh_bin="${ROBOTGO_RELEASE_GH_BIN:-gh}"

usage() {
  printf 'usage: %s <tag> <40-character-origin-main-commit>\n' \
    "$(basename -- "$0")" >&2
}

if (($# != 2)); then
  usage
  exit 2
fi

readonly tag="$1"
readonly expected_commit="$2"
readonly stable_tag="v1.0.0"

case "$tag" in
  v1.0.0-rc.[1-9]*|v1.0.0)
    ;;
  *)
    printf 'refusing unexpected stable-line tag: %s\n' "$tag" >&2
    exit 1
    ;;
esac
if [[ ! "$tag" =~ ^v1\.0\.0(-rc\.[1-9][0-9]*)?$ ]]; then
  printf 'refusing malformed stable-line tag: %s\n' "$tag" >&2
  exit 1
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

remote_tag_rows="$(
  "$git_bin" ls-remote --tags "$remote" \
    "refs/tags/$tag" "refs/tags/$tag^{}" \
    "refs/tags/$stable_tag" "refs/tags/$stable_tag^{}"
)"
if [[ -n "$remote_tag_rows" ]]; then
  printf 'refusing existing authoritative origin tag:\n%s\n' \
    "$remote_tag_rows" >&2
  exit 1
fi

github_tag_refs="$(
  "$gh_bin" api "repos/$repository/git/matching-refs/tags/v1.0.0" \
    --jq ".[] | select(.ref == \"refs/tags/$tag\" or .ref == \"refs/tags/$stable_tag\") | .ref"
)"
if [[ -n "$github_tag_refs" ]]; then
  printf 'refusing existing GitHub tag ref:\n%s\n' "$github_tag_refs" >&2
  exit 1
fi

github_releases="$(
  "$gh_bin" api "repos/$repository/releases?per_page=100" \
    --jq ".[] | select(.tag_name == \"$tag\" or .tag_name == \"$stable_tag\") | .tag_name"
)"
if [[ -n "$github_releases" ]]; then
  printf 'refusing existing GitHub release:\n%s\n' "$github_releases" >&2
  exit 1
fi

printf 'release preflight passed\n'
printf 'repository=%s\nremote=%s\ntag=%s\ncommit=%s\n' \
  "$repository" "$remote" "$tag" "$expected_commit"
printf 'publish only the explicit tag ref; never use git push --tags\n'
