#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -ne 0 || $# -ne 0 ]]; then
  echo "invalid protected runner cleanup invocation" >&2
  exit 1
fi

rm -rf -- /run/robotgo-evidence /run/robotgo-operator
