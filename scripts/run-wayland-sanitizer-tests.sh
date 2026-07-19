#!/usr/bin/env bash

set -euo pipefail

readonly go_bin="${ROBOTGO_GO_BIN:-go}"
readonly test_pattern='^TestScreencopy(WlShm|TimeoutIsBounded|DmabufFailureDoesNotCloseStdin)$'

expected="$(printf '%s\n' \
  TestScreencopyDmabufFailureDoesNotCloseStdin \
  TestScreencopyTimeoutIsBounded \
  TestScreencopyWlShm | LC_ALL=C sort)"
readonly expected

set +e
listed="$("$go_bin" test -asan -tags 'wayland test' ./screen \
  -list "$test_pattern" 2>&1)"
list_status=$?
set -e
printf '%s\n' "$listed"
if ((list_status != 0)); then
  exit "$list_status"
fi

actual="$(printf '%s\n' "$listed" | \
  awk '/^TestScreencopy/ { print }' | LC_ALL=C sort)"
readonly actual
if [[ "$actual" != "$expected" ]]; then
  printf 'sanitizer test manifest mismatch\nexpected:\n%s\nactual:\n%s\n' \
    "$expected" "${actual:-<empty>}" >&2
  exit 1
fi

set +e
output="$("$go_bin" test -asan -tags 'wayland test' ./screen \
  -run "$test_pattern" -count=1 -timeout=60s -v 2>&1)"
test_status=$?
set -e
printf '%s\n' "$output"
if ((test_status != 0)); then
  exit "$test_status"
fi

while IFS= read -r test_name; do
  if ! grep -Eq "^--- PASS: ${test_name} \\(.*\\)$" <<<"$output"; then
    printf 'sanitizer test did not pass: %s\n' "$test_name" >&2
    exit 1
  fi
done <<<"$expected"
