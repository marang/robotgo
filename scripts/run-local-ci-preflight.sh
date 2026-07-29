#!/usr/bin/env bash

set -euo pipefail

readonly script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly repository_root="$(cd -- "$script_dir/.." && pwd)"
readonly lint_version="2.11.4"
readonly temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/robotgo-ci-preflight.XXXXXX")"
readonly base_ref="${ROBOTGO_PREFLIGHT_BASE_REF:-origin/main}"

cleanup() {
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT INT TERM
chmod 700 "$temporary_root"
cd -- "$repository_root"

step=0
run_logged() {
  local label="$1"
  shift
  step=$((step + 1))
  local log="$temporary_root/$step.log"
  if ! "$@" >"$log" 2>&1; then
    printf 'failed: %s\n' "$label" >&2
    cat -- "$log" >&2
    return 1
  fi
  printf 'ok: %s\n' "$label"
}

if ! command -v golangci-lint >/dev/null 2>&1; then
  printf 'golangci-lint %s is required; install it with:\n' "$lint_version" >&2
  printf '  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v%s\n' \
    "$lint_version" >&2
  exit 1
fi
if ! golangci-lint version | grep -Fq "version $lint_version"; then
  printf 'golangci-lint %s is required; found:\n' "$lint_version" >&2
  golangci-lint version >&2
  exit 1
fi

go_files=()
while IFS= read -r -d '' file; do
  go_files+=("$file")
done < <(git ls-files -z --cached --others --exclude-standard -- '*.go')
if ((${#go_files[@]} == 0)); then
  printf 'no Go files found in the repository\n' >&2
  exit 1
fi
unformatted="$(gofmt -l "${go_files[@]}")"
if [[ -n "$unformatted" ]]; then
  printf 'Go files require gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi

if ! git rev-parse --verify --quiet "$base_ref^{commit}" >/dev/null; then
  printf 'preflight base ref does not exist: %s\n' "$base_ref" >&2
  exit 1
fi
merge_base="$(git merge-base HEAD "$base_ref")"
if [[ -z "$merge_base" ]]; then
  printf 'no merge base between HEAD and %s\n' "$base_ref" >&2
  exit 1
fi
run_logged "branch diff hygiene" git diff --check "$merge_base"
run_logged "working-tree diff hygiene" git diff --check HEAD
run_logged "module integrity" go mod verify
run_logged "workflow syntax" \
  go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
run_logged "native default tests" go test ./...
run_logged "Pure-Go default tests" env CGO_ENABLED=0 go test ./...
run_logged "native vet" go vet ./...
run_logged "Pure-Go vet" env CGO_ENABLED=0 go vet ./...
run_logged "runtime support contract" go run ./internal/cmd/supportmatrix
run_logged "public API compatibility" go run ./internal/cmd/apicompat \
  -variant linux-cgo \
  -variant linux-cgo-wayland \
  -variant linux-cgo-portal \
  -variant linux-cgo-pipewire \
  -variant linux-cgo-full \
  -variant linux-nocgo \
  -variant linux-nocgo-arm64 \
  -variant windows-nocgo \
  -variant windows-nocgo-arm64 \
  -variant darwin-nocgo \
  -variant darwin-nocgo-amd64
run_logged "native lint" env CGO_ENABLED=1 \
  golangci-lint run --timeout=5m ./...
run_logged "Pure-Go lint" env CGO_ENABLED=0 \
  golangci-lint run --timeout=5m ./...
