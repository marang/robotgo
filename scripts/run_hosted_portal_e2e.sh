#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 7 ]; then
  echo "usage: run_hosted_portal_e2e.sh RUNNER MANIFEST REPOSITORY STATE COMMIT CELL TOPOLOGY" >&2
  exit 2
fi

portal_runner="$1"
manifest="$2"
repository="$3"
state_root="$4"
commit="$5"
cell="$6"
topology="$7"

case "$cell" in
  remote-desktop|screencast) ;;
  *) echo "unsupported hosted portal cell" >&2; exit 2 ;;
esac
case "$topology" in
  single-output|multi-output) ;;
  *) echo "unsupported hosted portal topology" >&2; exit 2 ;;
esac

"$portal_runner" validate \
  -manifest "$manifest" \
  -repository-root "$repository" \
  -state-root "$state_root"
"$portal_runner" build \
  -manifest "$manifest" \
  -repository-root "$repository" \
  -state-root "$state_root"

hosted_run=(
  "$portal_runner" hosted-run
  -manifest "$manifest"
  -repository-root "$repository"
  -state-root "$state_root"
  -commit "$commit"
  -cell "$cell"
  -topology "$topology"
)
if [ "$topology" = "multi-output" ]; then
  exec xvfb-run -a \
    -s '-screen 0 1280x800x24 -nolisten tcp -noreset' \
    env ROBOTGO_HOSTED_XVFB=1 "${hosted_run[@]}"
fi
exec "${hosted_run[@]}"
