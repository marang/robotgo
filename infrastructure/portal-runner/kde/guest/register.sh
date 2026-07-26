#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $EUID -ne 0 || $# -ne 7 ]]; then
  echo "invalid protected runner registration invocation" >&2
  exit 1
fi

repository=$1
token=$2
runner_name=$3
labels=$4
commit=$5
run_id=$6
run_attempt=$7

test "$repository" = marang/robotgo
[[ $token =~ ^[A-Za-z0-9_-]{20,255}$ ]]
[[ $runner_name =~ ^robotgo-kde-[0-9]+-[1-9][0-9]*-(remote-desktop|screencast)$ ]]
test "$labels" = self-hosted,linux,wayland,kde
[[ $commit =~ ^[0-9a-f]{40}$ ]]
[[ $run_id =~ ^[0-9]+$ ]]
[[ $run_attempt =~ ^[1-9][0-9]*$ ]]
cell=${runner_name##*-}
if [[ $runner_name == *-remote-desktop ]]; then
  cell=remote-desktop
fi
runner_label=robotgo-$cell

install -d -m 0700 /run/robotgo-operator
printf 'ready commit=%s run=%s attempt=%s lane=kde cell=%s\n' \
  "$commit" "$run_id" "$run_attempt" "$cell" \
  >/run/robotgo-operator/console-ready
chmod 0600 /run/robotgo-operator/console-ready

cd /opt/actions-runner
runuser -u robotgo -- ./config.sh \
  --unattended \
  --url "https://github.com/$repository" \
  --token "$token" \
  --name "$runner_name" \
  --labels "$labels,$runner_label" \
  --no-default-labels \
  --ephemeral \
  --disableupdate
unset token
systemctl enable --now robotgo-runner.service
