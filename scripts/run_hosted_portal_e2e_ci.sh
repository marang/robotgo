#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 7 ]; then
  echo "usage: run_hosted_portal_e2e_ci.sh RUNNER MANIFEST REPOSITORY STATE COMMIT CELL TOPOLOGY" >&2
  exit 2
fi

script_directory="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly script_directory

exec timeout --preserve-status \
  --signal=TERM --kill-after=2m 30m \
  bash "$script_directory/run_hosted_portal_e2e.sh" "$@"
