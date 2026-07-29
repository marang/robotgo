package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	releaseCommit                = "0123456789abcdef0123456789abcdef01234567"
	stableQualificationBoundary  = "1785924826"
	stableQualificationTooEarly  = "1785924825"
	stableQualificationAfterGate = "1785924827"
)

func TestOriginReleasePreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tag         string
		environment map[string]string
		wantError   string
	}{
		{name: "clean authoritative origin"},
		{
			name: "clean stable release at qualification boundary",
			tag:  "v1.0.0",
		},
		{
			name: "clean stable release after qualification boundary",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_DATE":  "Wed, 05 Aug 2026 10:13:47 GMT",
				"FAKE_GITHUB_EPOCH": stableQualificationAfterGate,
			},
		},
		{
			name: "clean stable release with BSD date parser",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GNU_DATE_STATUS": "1",
			},
		},
		{
			name: "stable release before qualification boundary",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_DATE":  "Wed, 05 Aug 2026 10:13:45 GMT",
				"FAKE_GITHUB_EPOCH": stableQualificationTooEarly,
			},
			wantError: "stable qualification window is still open",
		},
		{
			name: "stable release rejects missing GitHub date header",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_DATE_MISSING": "1",
			},
			wantError: "did not contain exactly one Date header",
		},
		{
			name: "stable release rejects duplicate GitHub date headers",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_DATE_DUPLICATE": "1",
			},
			wantError: "did not contain exactly one Date header",
		},
		{
			name: "stable release rejects invalid authoritative time",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_EPOCH": "not-an-epoch",
			},
			wantError: "invalid authoritative GitHub epoch",
		},
		{
			name: "stable release rejects overflowing authoritative time",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_EPOCH": "99999999999",
			},
			wantError: "invalid authoritative GitHub epoch",
		},
		{
			name: "stable release fails closed when GitHub time lookup fails",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_GITHUB_CLOCK_STATUS": "17",
			},
			wantError: "failed to obtain authoritative GitHub time",
		},
		{
			name: "stable release fails closed when GitHub time parsing fails",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_DATE_STATUS": "17",
			},
			wantError: "failed to parse authoritative GitHub time",
		},
		{
			name: "release candidate does not require stable qualification time",
			environment: map[string]string{
				"FAKE_GITHUB_CLOCK_STATUS": "17",
				"FAKE_DATE_STATUS":         "17",
			},
		},
		{
			name: "wrong origin repository",
			environment: map[string]string{
				"FAKE_REMOTE_URL": "git@github.com:go-vgo/robotgo.git",
			},
			wantError: "refusing non-authoritative release remote",
		},
		{
			name: "candidate is not origin main",
			environment: map[string]string{
				"FAKE_MAIN_COMMIT": "abcdef0123456789abcdef0123456789abcdef01",
			},
			wantError: "release commit is not authoritative origin/main",
		},
		{
			name: "local stable tag collision",
			tag:  "v1.0.0",
			environment: map[string]string{
				"FAKE_LOCAL_TAG": "v1.0.0",
			},
			wantError: "refusing existing local tag collision",
		},
		{
			name: "local tag lookup error",
			environment: map[string]string{
				"FAKE_LOCAL_TAG_LOOKUP_STATUS": "128",
			},
			wantError: "failed to inspect local tag",
		},
		{
			name: "origin tag already exists",
			environment: map[string]string{
				"FAKE_ORIGIN_TAGS": releaseCommit + "\trefs/tags/v1.0.0-rc.1\n",
			},
			wantError: "refusing existing authoritative origin tag",
		},
		{
			name: "GitHub tag already exists",
			environment: map[string]string{
				"FAKE_GITHUB_TAGS": "refs/tags/v1.0.0-rc.1\n",
			},
			wantError: "refusing existing GitHub tag ref",
		},
		{
			name: "GitHub release already exists",
			environment: map[string]string{
				"FAKE_GITHUB_RELEASES": "v1.0.0-rc.1\n",
			},
			wantError: "refusing existing GitHub release",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, err := runOriginReleasePreflight(
				t,
				test.tag,
				test.environment,
			)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("preflight failed: %v\n%s", err, output)
				}
				if !strings.Contains(output, "release preflight passed") ||
					!strings.Contains(output, "commit="+releaseCommit) {
					t.Fatalf("preflight output missing success identity:\n%s", output)
				}
				if test.tag == "v1.0.0" &&
					!strings.Contains(
						output,
						"stable-not-before=2026-08-05T10:13:46Z",
					) {
					t.Fatalf(
						"stable preflight output missing qualification boundary:\n%s",
						output,
					)
				}
				return
			}
			if err == nil {
				t.Fatalf("preflight unexpectedly succeeded:\n%s", output)
			}
			if !strings.Contains(output, test.wantError) {
				t.Fatalf("preflight output missing %q:\n%s", test.wantError, output)
			}
		})
	}
}

func TestOriginReleasePreflightRejectsMalformedInputs(t *testing.T) {
	t.Parallel()

	script := filepath.Join(".", "preflight-origin-release.sh")
	for _, arguments := range [][]string{
		{"v1.0.0-beta.3", releaseCommit},
		{"v1.0.0-rc.0", releaseCommit},
		{"v1.0.0-rc.1", "not-a-commit"},
	} {
		command := exec.Command("bash", append([]string{script}, arguments...)...)
		output, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("preflight accepted %q:\n%s", arguments, output)
		}
	}
}

func runOriginReleasePreflight(
	t *testing.T,
	tag string,
	overrides map[string]string,
) (string, error) {
	t.Helper()
	if tag == "" {
		tag = "v1.0.0-rc.1"
	}

	temporaryDirectory := t.TempDir()
	gitStub := filepath.Join(temporaryDirectory, "git")
	ghStub := filepath.Join(temporaryDirectory, "gh")
	dateStub := filepath.Join(temporaryDirectory, "date")
	writeExecutable(t, gitStub, `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  remote)
    printf '%s\n' "${FAKE_REMOTE_URL:-git@github.com:marang/robotgo.git}"
    ;;
  ls-remote)
    case "$2" in
      --heads)
        printf '%s\trefs/heads/main\n' "${FAKE_MAIN_COMMIT}"
        ;;
      --tags)
        printf '%s' "${FAKE_ORIGIN_TAGS:-}"
        ;;
      *)
        exit 2
        ;;
    esac
    ;;
  show-ref)
    if [[ "$2" != "--verify" || "$3" != "--quiet" ]]; then
      exit 2
    fi
    if [[ -n "${FAKE_LOCAL_TAG_LOOKUP_STATUS:-}" ]]; then
      exit "$FAKE_LOCAL_TAG_LOOKUP_STATUS"
    fi
    if [[ "${FAKE_LOCAL_TAG:-}" == "${4#refs/tags/}" ]]; then
      exit 0
    fi
    exit 1
    ;;
  *)
    exit 2
    ;;
esac
`)
	writeExecutable(t, ghStub, `#!/usr/bin/env bash
set -euo pipefail
case "$2" in
  repos/marang/robotgo)
    if [[ "$3" != "--include" || "$4" != "--jq" || "$5" != "empty" ]]; then
      exit 2
    fi
    if [[ -n "${FAKE_GITHUB_CLOCK_STATUS:-}" ]]; then
      exit "$FAKE_GITHUB_CLOCK_STATUS"
    fi
    printf 'HTTP/2.0 200 OK\n'
    if [[ -z "${FAKE_GITHUB_DATE_MISSING:-}" ]]; then
      printf 'Date: %s\n' \
        "${FAKE_GITHUB_DATE:-Wed, 05 Aug 2026 10:13:46 GMT}"
      if [[ -n "${FAKE_GITHUB_DATE_DUPLICATE:-}" ]]; then
        printf 'date: Wed, 05 Aug 2026 10:13:47 GMT\n'
      fi
    fi
    printf '\n'
    ;;
  */git/matching-refs/tags/v1.0.0)
    printf '%s' "${FAKE_GITHUB_TAGS:-}"
    ;;
  */releases?per_page=100)
    printf '%s' "${FAKE_GITHUB_RELEASES:-}"
    ;;
  *)
    exit 2
    ;;
esac
`)
	writeExecutable(t, dateStub, `#!/usr/bin/env bash
set -euo pipefail
expected_date="${FAKE_GITHUB_DATE:-Wed, 05 Aug 2026 10:13:46 GMT}"
if (($# == 4)) &&
  [[ "$1" == "-u" && "$2" == "-d" && "$3" == "$expected_date" && "$4" == "+%s" ]]; then
  if [[ -n "${FAKE_GNU_DATE_STATUS:-}" ]]; then
    exit "$FAKE_GNU_DATE_STATUS"
  fi
elif (($# == 6)) &&
  [[ "$1" == "-j" && "$2" == "-u" && "$3" == "-f" ]] &&
  [[ "$4" == "%a, %d %b %Y %H:%M:%S GMT" ]] &&
  [[ "$5" == "$expected_date" && "$6" == "+%s" ]]; then
  :
else
  exit 2
fi
if [[ -n "${FAKE_DATE_STATUS:-}" ]]; then
  exit "$FAKE_DATE_STATUS"
fi
printf '%s\n' "${FAKE_GITHUB_EPOCH:-1785924826}"
`)

	environment := append(os.Environ(),
		"ROBOTGO_RELEASE_GIT_BIN="+gitStub,
		"ROBOTGO_RELEASE_GH_BIN="+ghStub,
		"ROBOTGO_RELEASE_DATE_BIN="+dateStub,
		"FAKE_MAIN_COMMIT="+releaseCommit,
	)
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	command := exec.Command(
		"bash",
		filepath.Join(".", "preflight-origin-release.sh"),
		tag,
		releaseCommit,
	)
	command.Env = environment
	output, err := command.CombinedOutput()
	return string(output), err
}

func writeExecutable(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
