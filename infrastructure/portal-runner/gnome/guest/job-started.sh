#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $EUID -ne 0 || $# -ne 4 ]]; then
  echo "invalid protected runner hook invocation" >&2
  exit 1
fi

commit=$1
run_id=$2
run_attempt=$3
workflow=$4

[[ $commit =~ ^[0-9a-f]{40}$ ]]
[[ $run_id =~ ^[0-9]+$ ]]
[[ $run_attempt =~ ^[1-9][0-9]*$ ]]

case "$workflow" in
  "RemoteDesktop E2E") cell=remote-desktop ;;
  "ScreenCast E2E") cell=screencast ;;
  *)
    echo "workflow is not authorized for the protected GNOME runner" >&2
    exit 1
    ;;
esac

console_dir=/run/robotgo-operator
console_ready=$console_dir/console-ready
test -d "$console_dir"
test "$(stat -c '%u:%a:%F' "$console_dir")" = "0:700:directory"
test -f "$console_ready"
test "$(stat -c '%u:%a:%F' "$console_ready")" = "0:600:regular file"
expected="ready commit=$commit run=$run_id attempt=$run_attempt lane=gnome cell=$cell"
test "$(cat "$console_ready")" = "$expected"

evidence_dir=/run/robotgo-evidence
install -d -m 0755 -o root -g root "$evidence_dir"
temporary=$(mktemp "$evidence_dir/.operator-ready.XXXXXX")
trap 'rm -f -- "$temporary"' EXIT
printf 'ready commit=%s run=%s attempt=%s lane=gnome cell=%s\n' \
  "$commit" "$run_id" "$run_attempt" "$cell" >"$temporary"
chmod 0444 "$temporary"
chown root:root "$temporary"
mv -fT "$temporary" "$evidence_dir/operator-ready"
rm -f -- "$console_ready"
trap - EXIT
