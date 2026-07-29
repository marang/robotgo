package scripts

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestPrimaryWorkflowsAvoidDuplicateBranchRuns(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"../.github/workflows/go.yml",
		"../.github/workflows/x11-default-suite.yml",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			text := readWorkflow(t, path)
			for _, required := range []string{
				"  push:\n    branches: [main]\n    tags: ['v*']\n  pull_request:",
				"cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
			} {
				if !strings.Contains(text, required) {
					t.Errorf("%s omits %q", path, required)
				}
			}
		})
	}
}

func TestReleaseEvidenceDispatchCannotCancelMainCompositorProof(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		path  string
		group string
	}{
		{
			path:  "../.github/workflows/sway-e2e.yml",
			group: "group: sway-e2e-${{ github.event_name }}-${{ github.ref }}",
		},
		{
			path:  "../.github/workflows/hyprland-e2e.yml",
			group: "group: hyprland-e2e-${{ github.event_name }}-${{ github.ref }}",
		},
	} {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			text := readWorkflow(t, test.path)
			for _, required := range []string{
				test.group,
				"cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
			} {
				if !strings.Contains(text, required) {
					t.Errorf("%s omits %q", test.path, required)
				}
			}
		})
	}
}

func TestLocalCIPreflightCoversFastFailureContracts(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("run-local-ci-preflight.sh")
	if err != nil {
		t.Fatalf("read local CI preflight: %v", err)
	}
	text := string(body)
	for _, required := range []string{
		`mktemp -d "${TMPDIR:-/tmp}/robotgo-ci-preflight.XXXXXX"`,
		"trap cleanup EXIT INT TERM",
		`run_logged "diff hygiene" git diff --check`,
		`run_logged "module integrity" go mod verify`,
		"go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7",
		`run_logged "native default tests" go test ./...`,
		`run_logged "Pure-Go default tests" env CGO_ENABLED=0 go test ./...`,
		`run_logged "native vet" go vet ./...`,
		`run_logged "Pure-Go vet" env CGO_ENABLED=0 go vet ./...`,
		"go run ./internal/cmd/supportmatrix",
		"go run ./internal/cmd/apicompat",
		`run_logged "native lint" env CGO_ENABLED=1`,
		`run_logged "Pure-Go lint" env CGO_ENABLED=0`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("local CI preflight omits %q", required)
		}
	}
	info, err := os.Stat("run-local-ci-preflight.sh")
	if err != nil {
		t.Fatalf("stat local CI preflight: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatal("local CI preflight is not executable")
	}
}

func readWorkflow(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return normalizeWorkflowText(body)
}
