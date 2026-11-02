#!/usr/bin/env bash

set -euo pipefail

# Called only by the fixed container entrypoint after environment isolation.
# Update inventory is report-only. Vulnerabilities and tool/network errors are
# failures; neither an inventory nor an unavailable scanner means "clean".
if (($# != 1)) || [[ "$1" != security && "$1" != dependencies ]]; then
    printf 'expected exactly one fixed dependency task\n' >&2
    exit 2
fi
export CGO_ENABLED=0
if [[ "$1" == dependencies ]]; then
    printf 'stage=go-module-update-inventory (not a vulnerability verdict)\n' >&2
    go list -mod=readonly -m -u -json all
fi
printf 'stage=go-reachable-vulnerabilities (Pure-Go Linux scope)\n' >&2
# Text mode supplies the scanner's vulnerability exit verdict. JSON mode is an
# event stream and must not be treated as a clean verdict merely from exit 0.
govulncheck ./...
