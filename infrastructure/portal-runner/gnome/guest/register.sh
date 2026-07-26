#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $EUID -ne 0 || $# -ne 5 ]]; then
  echo "invalid protected runner registration invocation" >&2
  exit 1
fi

repository=$1
runner_name=$2
commit=$3
run_id=$4
run_attempt=$5

manifest=/var/lib/robotgo-runner/manifest.json
test -f "$manifest"
test "$repository" = "$(jq -r '.repository' "$manifest")"
[[ $runner_name =~ ^robotgo-gnome-[0-9]+-[1-9][0-9]*-(remote-desktop|screencast)$ ]]
[[ $commit =~ ^[0-9a-f]{40}$ ]]
[[ $run_id =~ ^[0-9]+$ ]]
[[ $run_attempt =~ ^[1-9][0-9]*$ ]]

case "$runner_name" in
  *-remote-desktop) cell=remote-desktop ;;
  *-screencast) cell=screencast ;;
  *)
    echo "protected runner cell is invalid" >&2
    exit 1
    ;;
esac

IFS= read -r registration_token
if [[ ! $registration_token =~ ^[A-Za-z0-9_=-]{20,512}$ ]]; then
  echo "protected runner registration token is invalid" >&2
  exit 1
fi

console_dir=/run/robotgo-operator
install -d -m 0700 -o root -g root "$console_dir"
temporary=$(mktemp "$console_dir/.console-ready.XXXXXX")
trap 'rm -f -- "$temporary"; unset registration_token' EXIT
printf 'ready commit=%s run=%s attempt=%s lane=gnome cell=%s\n' \
  "$commit" "$run_id" "$run_attempt" "$cell" >"$temporary"
chmod 0600 "$temporary"
chown root:root "$temporary"
mv -fT "$temporary" "$console_dir/console-ready"

test ! -e /opt/actions-runner/.runner
labels=$(jq -er '.labels | join(",")' "$manifest")
runner_label=robotgo-$cell
runuser -u robotgo -- /opt/actions-runner/config.sh \
  --url "https://github.com/$repository" \
  --token "$registration_token" \
  --name "$runner_name" \
  --labels "$labels,$runner_label" \
  --no-default-labels \
  --work _work \
  --unattended \
  --ephemeral \
  --disableupdate
unset registration_token

systemctl start --no-block robotgo-runner.service
trap - EXIT
