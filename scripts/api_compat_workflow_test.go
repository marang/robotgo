package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestGoWorkflowBlocksOnEveryPublicAPIVariant(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile("../.github/workflows/go.yml")
	if err != nil {
		t.Fatalf("read Go workflow: %v", err)
	}
	text := string(workflow)
	for _, required := range []string{
		"  api-compat:",
		"name: Check stable public Go API",
		"libpipewire-0.3-dev",
		"go run ./internal/cmd/apicompat \\",
		"-variant linux-cgo",
		"-variant linux-cgo-wayland",
		"-variant linux-cgo-portal",
		"-variant linux-cgo-pipewire",
		"-variant linux-cgo-full",
		"-variant linux-nocgo",
		"-variant linux-nocgo-arm64",
		"-variant windows-nocgo",
		"-variant windows-nocgo-arm64",
		"-variant darwin-nocgo",
		"-variant darwin-nocgo-amd64",
		"name: Check OCR public API invariance",
		"go run ./internal/cmd/apicompat -variant linux-cgo-ocr",
		"name: Check native macOS public Go API",
		"if: runner.os == 'macOS'",
		"go run ./internal/cmd/apicompat -variant darwin-cgo",
		"name: Check native Windows public Go API",
		"if: runner.os == 'Windows'",
		"go run ./internal/cmd/apicompat -variant windows-cgo",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Go workflow omits API compatibility contract %q", required)
		}
	}

	start := strings.Index(text, "  api-compat:")
	end := strings.Index(text[start+1:], "\n  vet:")
	if start < 0 || end < 0 {
		t.Fatal("api-compat job boundaries not found")
	}
	job := text[start : start+1+end]
	for _, forbidden := range []string{
		"continue-on-error:",
		"persist-credentials: true",
	} {
		if strings.Contains(job, forbidden) {
			t.Errorf("api-compat job contains %q", forbidden)
		}
	}
}
