package releaseevidence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	robotgo "github.com/marang/robotgo"
)

const (
	testCommit = "0123456789abcdef0123456789abcdef01234567"
	testTree   = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestGenerateWriteAndVerify(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	logPath := filepath.Join(directory, "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	expectedCGO := robotgo.GetRuntimeCapabilities().Runtime.CGOEnabled
	matrix := currentMatrix(expectedCGO)
	snapshot, err := Generate(context.Background(), Config{
		Commit:      testCommit,
		Tree:        testTree,
		Ref:         "refs/tags/v1.2.3",
		RunID:       "12345",
		RunAttempt:  2,
		Matrix:      matrix,
		TestCommand: "go test -count=1 ./...",
		TestLogPath: logPath,
		ExpectedCGO: expectedCGO,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if snapshot.Toolchain.GoVersion != runtime.Version() {
		t.Fatalf("Go version = %q, want %q", snapshot.Toolchain.GoVersion, runtime.Version())
	}
	if snapshot.Diagnostics.Runtime.CGOEnabled != expectedCGO {
		t.Fatalf("CGO enabled = %t, want %t", snapshot.Diagnostics.Runtime.CGOEnabled, expectedCGO)
	}

	evidencePath := filepath.Join(directory, "evidence.json")
	if err := Write(evidencePath, snapshot); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	verified, err := Verify(evidencePath, Expectations{Matrix: matrix})
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if verified.Source.Commit != testCommit {
		t.Fatalf("commit = %q, want %q", verified.Source.Commit, testCommit)
	}
}

func TestVerifyRejectsTamperedLog(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	logPath := filepath.Join(directory, "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(t, logPath)
	evidencePath := filepath.Join(directory, "evidence.json")
	if err := Write(evidencePath, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("FAIL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(evidencePath, Expectations{
		Matrix: snapshot.CI.Matrix,
	}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Verify error = %v, want SHA-256 mismatch", err)
	}
}

func TestVerifyRejectsUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	logPath := filepath.Join(directory, "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(t, logPath)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	unknownPath := filepath.Join(directory, "unknown.json")
	unknown := append([]byte(nil), data[:len(data)-1]...)
	unknown = append(unknown, []byte(`,"unexpected":true}`)...)
	if err := os.WriteFile(unknownPath, unknown, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(unknownPath, Expectations{
		Matrix: snapshot.CI.Matrix,
	}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Verify unknown-field error = %v", err)
	}

	trailingPath := filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(trailingPath, append(data, []byte(`{}`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(trailingPath, Expectations{
		Matrix: snapshot.CI.Matrix,
	}); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("Verify trailing-value error = %v", err)
	}
}

func TestGenerateRejectsWrongCGOMatrix(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actualCGO := robotgo.GetRuntimeCapabilities().Runtime.CGOEnabled
	_, err := Generate(context.Background(), Config{
		Commit:      testCommit,
		Tree:        testTree,
		Ref:         "refs/heads/main",
		RunID:       "1",
		RunAttempt:  1,
		Matrix:      currentMatrix(actualCGO),
		TestCommand: "go test ./...",
		TestLogPath: logPath,
		ExpectedCGO: !actualCGO,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime CGO state") {
		t.Fatalf("Generate error = %v, want CGO mismatch", err)
	}
}

func TestValidateSnapshotRejectsLogTraversal(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(t, logPath)
	snapshot.Test.LogFile = "../test.log"
	if err := validateSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "base filename") {
		t.Fatalf("validateSnapshot error = %v, want base filename rejection", err)
	}
	snapshot.Test.LogFile = `..\test.log`
	if err := validateSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "base filename") {
		t.Fatalf("validateSnapshot Windows-style error = %v, want base filename rejection", err)
	}
}

func TestVerifyRejectsUnexpectedIdentity(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	logPath := filepath.Join(directory, "test.log")
	if err := os.WriteFile(logPath, []byte("PASS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot(t, logPath)
	evidencePath := filepath.Join(directory, "evidence.json")
	if err := Write(evidencePath, snapshot); err != nil {
		t.Fatal(err)
	}
	_, err := Verify(evidencePath, Expectations{
		Matrix: snapshot.CI.Matrix,
		Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("Verify error = %v, want commit mismatch", err)
	}
}

func validSnapshot(t *testing.T, logPath string) Snapshot {
	t.Helper()
	snapshot, err := Generate(context.Background(), Config{
		Commit:      testCommit,
		Tree:        testTree,
		Ref:         "refs/heads/main",
		RunID:       "123",
		RunAttempt:  1,
		Matrix:      currentMatrix(robotgo.GetRuntimeCapabilities().Runtime.CGOEnabled),
		TestCommand: "go test ./...",
		TestLogPath: logPath,
		ExpectedCGO: robotgo.GetRuntimeCapabilities().Runtime.CGOEnabled,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	return snapshot
}

func currentMatrix(cgoEnabled bool) string {
	platform := runtime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	implementation := "purego"
	if cgoEnabled {
		implementation = "native"
	}
	return platform + "-" + implementation
}
