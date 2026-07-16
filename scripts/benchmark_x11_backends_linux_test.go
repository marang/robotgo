package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceOutputLockUsesPhysicalDirectory(t *testing.T) {
	t.Parallel()

	temporaryDirectory := t.TempDir()
	realOutput := filepath.Join(temporaryDirectory, "real-output")
	aliasOutput := filepath.Join(temporaryDirectory, "alias-output")
	if err := os.Mkdir(realOutput, 0o700); err != nil {
		t.Fatalf("create real output directory: %v", err)
	}
	if err := os.Symlink(realOutput, aliasOutput); err != nil {
		t.Fatalf("create output symlink: %v", err)
	}
	if err := os.Mkdir(realOutput+".lock", 0o700); err != nil {
		t.Fatalf("create physical output lock: %v", err)
	}

	command := exec.Command("bash", "benchmark-x11-backends.sh", aliasOutput)
	command.Env = evidenceTestEnvironment()
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("benchmark script unexpectedly acquired aliased output lock:\n%s", output)
	}
	if !strings.Contains(string(output), "evidence output is locked") {
		t.Fatalf("benchmark script output = %q, want physical-lock rejection", output)
	}

	entries, err := os.ReadDir(realOutput)
	if err != nil {
		t.Fatalf("read real output directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("physical output was modified before lock rejection: %v", entries)
	}
}

func evidenceTestEnvironment() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "ROBOTGO_X11_EVIDENCE_LOCK_TOKEN=") ||
			strings.HasPrefix(entry, "ROBOTGO_X11_EVIDENCE_LOCK_DIR=") ||
			strings.HasPrefix(entry, "ROBOTGO_X11_EVIDENCE_SNAPSHOT_ROOT=") ||
			strings.HasPrefix(entry, "ROBOTGO_X11_EVIDENCE_COUNT=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "ROBOTGO_X11_EVIDENCE_COUNT=0")
}
