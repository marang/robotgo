package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseCommit = "0123456789abcdef0123456789abcdef01234567"

func TestOriginReleasePreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tag         string
		environment map[string]string
		wantError   string
	}{
		{name: "clean authoritative origin"},
		{name: "clean stable release", tag: "v1.0.0"},
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

	environment := append(os.Environ(),
		"ROBOTGO_RELEASE_GIT_BIN="+gitStub,
		"ROBOTGO_RELEASE_GH_BIN="+ghStub,
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
