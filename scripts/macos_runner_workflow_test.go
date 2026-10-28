package scripts

import (
	"strings"
	"testing"
)

func TestMacOSWorkflowKeepsARM64CoverageOnOneRunner(t *testing.T) {
	t.Parallel()
	workflow := readWorkflow(t, "../.github/workflows/go.yml")
	native := macOSWorkflowSection(t, workflow, "  test:\n", "  nocgo:\n")
	for _, required := range []string{
		"name: test (${{ matrix.os }})",
		"os: [ubuntu-latest, macOS-latest, windows-latest]",
		"- os: macOS-latest\n            runner: macos-15",
		"runs-on: ${{ matrix.runner || matrix.os }}",
		"run: go build -v .",
		"run: go test -v ./...",
		"go run ./internal/cmd/apicompat -variant darwin-cgo",
	} {
		if !strings.Contains(native, required) {
			t.Errorf("native ARM64 job omits %q", required)
		}
	}
	for _, step := range []struct {
		name     string
		required []string
	}{
		{
			name: "Verify native macOS ARM64 runner",
			required: []string{
				`test "$(uname -m)" = arm64`,
				`test "$(go env GOARCH)" = arm64`,
				`test "$(go env CGO_ENABLED)" = 1`,
			},
		},
		{
			name: "Test Pure-Go macOS ARM64 build",
			required: []string{
				`CGO_ENABLED: "0"`,
				"run: go test -count=1 -v ./...",
			},
		},
		{
			name: "Test Pure-Go macOS ARM64 input preflight",
			required: []string{
				`CGO_ENABLED: "0"`,
				`-run "^TestPureGoDarwinInputRuntime$"`,
				"-count=1 -timeout=30s -v .",
			},
		},
	} {
		block := macOSWorkflowSection(t, native,
			"      - name: "+step.name+"\n", "      - ")
		if !strings.Contains(block, "if: runner.os == 'macOS'") {
			t.Errorf("%s must execute on the native macOS lane", step.name)
		}
		for _, required := range step.required {
			if !strings.Contains(block, required) {
				t.Errorf("%s omits %q", step.name, required)
			}
		}
	}
	if strings.Contains(native, "continue-on-error:") {
		t.Fatal("native and Pure-Go ARM64 failures must block the protected job")
	}
}

func TestMacOSPureGoWorkflowUsesIntelWithoutRenamingProtectedChecks(t *testing.T) {
	t.Parallel()
	workflow := readWorkflow(t, "../.github/workflows/go.yml")
	pureGo := macOSWorkflowSection(t, workflow, "  nocgo:\n", "  x11-backend-evidence:\n")
	for _, required := range []string{
		"name: nocgo (${{ matrix.os }})",
		"runs-on: ${{ matrix.runner }}",
		"- os: ubuntu-latest\n            runner: ubuntu-latest",
		"- os: macOS-latest\n            runner: macos-26-intel",
		"- os: windows-latest\n            runner: windows-latest",
		"name: Test Pure-Go build",
		`CGO_ENABLED: "0"`,
		"run: go test -v ./...",
		"name: Test Pure-Go macOS input preflight",
		`-run "^TestPureGoDarwinInputRuntime$"`,
		"name: Test Pure-Go Windows input desktop",
		"name: Test Pure-Go Windows window and paste desktop",
		"name: Test Pure-Go X11 input with Xvfb",
	} {
		if !strings.Contains(pureGo, required) {
			t.Errorf("Pure-Go runner allocation omits %q", required)
		}
	}
	if strings.Contains(pureGo, "continue-on-error:") {
		t.Fatal("Pure-Go failures must remain blocking")
	}
	intel := macOSWorkflowSection(t, pureGo,
		"      - name: Verify Pure-Go macOS Intel runner\n", "      - ")
	for _, required := range []string{
		"if: runner.os == 'macOS'",
		`test "$(uname -m)" = x86_64`,
		`test "$(go env GOARCH)" = amd64`,
	} {
		if !strings.Contains(intel, required) {
			t.Errorf("Pure-Go Intel host verification omits %q", required)
		}
	}
}

func macOSWorkflowSection(t *testing.T, text, start, end string) string {
	t.Helper()
	first := strings.Index(text, start)
	if first < 0 {
		t.Fatalf("workflow section %q is missing", start)
	}
	rest := text[first+len(start):]
	last := strings.Index(rest, end)
	if last < 0 {
		t.Fatalf("workflow section %q has no boundary %q", start, end)
	}
	return rest[:last]
}
