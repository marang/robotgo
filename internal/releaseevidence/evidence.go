// Package releaseevidence creates and verifies versioned RobotGo release
// evidence snapshots.
package releaseevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	robotgo "github.com/marang/robotgo"
)

const (
	// SchemaVersion identifies the release-evidence JSON contract.
	SchemaVersion = "1"

	evidenceProvider = "github-actions"
	maxLabelLength   = 128
	maxTextLength    = 1024
	maxEvidenceBytes = 2 * 1024 * 1024
)

var (
	gitObjectPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	labelPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	digestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Source identifies the exact repository state used for a release check.
type Source struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	Ref    string `json:"ref"`
}

// CI identifies the GitHub Actions execution and matrix cell.
type CI struct {
	Provider   string `json:"provider"`
	RunID      string `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
	Matrix     string `json:"matrix"`
}

// Toolchain identifies the Go toolchain that generated the evidence.
type Toolchain struct {
	GoVersion string `json:"go_version"`
}

// Test records the passed command and the digest of its complete log.
type Test struct {
	Command   string `json:"command"`
	Result    string `json:"result"`
	LogFile   string `json:"log_file"`
	LogSHA256 string `json:"log_sha256"`
}

// Snapshot is the versioned, machine-readable release-evidence document.
type Snapshot struct {
	SchemaVersion string                     `json:"schema_version"`
	Source        Source                     `json:"source"`
	CI            CI                         `json:"ci"`
	Toolchain     Toolchain                  `json:"toolchain"`
	Test          Test                       `json:"test"`
	Diagnostics   robotgo.RuntimeDiagnostics `json:"runtime_diagnostics"`
}

// Config contains trusted workflow inputs used to create a Snapshot.
type Config struct {
	Commit      string
	Tree        string
	Ref         string
	RunID       string
	RunAttempt  int
	Matrix      string
	TestCommand string
	TestLogPath string
	ExpectedCGO bool
}

// Expectations bind a verified evidence file to its expected matrix cell and
// optionally to an exact source and test command.
type Expectations struct {
	Matrix      string
	Commit      string
	Tree        string
	Ref         string
	TestCommand string
}

// Generate builds a release-evidence snapshot and verifies that the running
// RobotGo build matches the requested CGO matrix cell.
func Generate(ctx context.Context, config Config) (Snapshot, error) {
	if err := validateConfig(config); err != nil {
		return Snapshot{}, err
	}

	logDigest, logName, err := digestRegularFile(config.TestLogPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("test log: %w", err)
	}

	diagnostics := robotgo.GetRuntimeDiagnostics(ctx)
	if diagnostics.SchemaVersion != robotgo.RuntimeDiagnosticsSchemaVersion {
		return Snapshot{}, fmt.Errorf(
			"runtime diagnostics schema %q, want %q",
			diagnostics.SchemaVersion,
			robotgo.RuntimeDiagnosticsSchemaVersion,
		)
	}
	if diagnostics.Runtime.CGOEnabled != config.ExpectedCGO {
		return Snapshot{}, fmt.Errorf(
			"runtime CGO state is %t, want %t",
			diagnostics.Runtime.CGOEnabled,
			config.ExpectedCGO,
		)
	}

	snapshot := Snapshot{
		SchemaVersion: SchemaVersion,
		Source: Source{
			Commit: config.Commit,
			Tree:   config.Tree,
			Ref:    config.Ref,
		},
		CI: CI{
			Provider:   evidenceProvider,
			RunID:      config.RunID,
			RunAttempt: config.RunAttempt,
			Matrix:     config.Matrix,
		},
		Toolchain: Toolchain{GoVersion: runtime.Version()},
		Test: Test{
			Command:   config.TestCommand,
			Result:    "pass",
			LogFile:   logName,
			LogSHA256: logDigest,
		},
		Diagnostics: diagnostics,
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Write stores a snapshot as indented JSON without replacing an existing
// evidence file.
func Write(path string, snapshot Snapshot) error {
	if path == "" {
		return errors.New("evidence output path is required")
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	data = append(data, '\n')

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create evidence %q: %w", path, err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write evidence %q: %w", path, writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close evidence %q: %w", path, closeErr)
	}
	return nil
}

// Verify reads an evidence file, validates its schema and expected identity,
// and verifies the adjacent test log against the recorded SHA-256 digest.
func Verify(path string, expected Expectations) (Snapshot, error) {
	if expected.Matrix == "" {
		return Snapshot{}, errors.New("expected matrix is required")
	}
	if _, _, err := matrixRuntime(expected.Matrix); err != nil {
		return Snapshot{}, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect evidence %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("evidence %q is not a regular file", path)
	}
	if info.Size() > maxEvidenceBytes {
		return Snapshot{}, fmt.Errorf("evidence %q exceeds %d bytes", path, maxEvidenceBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read evidence %q: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode evidence %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Snapshot{}, fmt.Errorf("decode evidence %q: trailing JSON value", path)
		}
		return Snapshot{}, fmt.Errorf("decode evidence %q: %w", path, err)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := validateExpectations(snapshot, expected); err != nil {
		return Snapshot{}, err
	}

	logPath := filepath.Join(filepath.Dir(path), snapshot.Test.LogFile)
	digest, name, err := digestRegularFile(logPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("test log: %w", err)
	}
	if name != snapshot.Test.LogFile {
		return Snapshot{}, errors.New("test log filename does not match evidence")
	}
	if digest != snapshot.Test.LogSHA256 {
		return Snapshot{}, fmt.Errorf(
			"test log SHA-256 %s, want %s",
			digest,
			snapshot.Test.LogSHA256,
		)
	}
	return snapshot, nil
}

func validateExpectations(snapshot Snapshot, expected Expectations) error {
	comparisons := []struct {
		name string
		got  string
		want string
	}{
		{name: "matrix", got: snapshot.CI.Matrix, want: expected.Matrix},
		{name: "commit", got: snapshot.Source.Commit, want: expected.Commit},
		{name: "tree", got: snapshot.Source.Tree, want: expected.Tree},
		{name: "ref", got: snapshot.Source.Ref, want: expected.Ref},
		{name: "test command", got: snapshot.Test.Command, want: expected.TestCommand},
	}
	for _, comparison := range comparisons {
		if comparison.want != "" && comparison.got != comparison.want {
			return fmt.Errorf(
				"%s %q, want %q",
				comparison.name,
				comparison.got,
				comparison.want,
			)
		}
	}
	return nil
}

func validateConfig(config Config) error {
	if !gitObjectPattern.MatchString(config.Commit) {
		return errors.New("commit must be a lowercase 40- or 64-character Git object ID")
	}
	if !gitObjectPattern.MatchString(config.Tree) {
		return errors.New("tree must be a lowercase 40- or 64-character Git object ID")
	}
	if err := validateText("ref", config.Ref); err != nil {
		return err
	}
	if !strings.HasPrefix(config.Ref, "refs/") {
		return errors.New("ref must start with refs/")
	}
	runID, err := strconv.ParseUint(config.RunID, 10, 64)
	if err != nil || runID == 0 || strconv.FormatUint(runID, 10) != config.RunID {
		return errors.New("run ID must be a positive decimal integer")
	}
	if config.RunAttempt <= 0 {
		return errors.New("run attempt must be positive")
	}
	if len(config.Matrix) > maxLabelLength || !labelPattern.MatchString(config.Matrix) {
		return errors.New("matrix must contain only letters, digits, dot, underscore, or hyphen")
	}
	return validateText("test command", config.TestCommand)
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SchemaVersion {
		return fmt.Errorf("release evidence schema %q, want %q", snapshot.SchemaVersion, SchemaVersion)
	}
	config := Config{
		Commit:      snapshot.Source.Commit,
		Tree:        snapshot.Source.Tree,
		Ref:         snapshot.Source.Ref,
		RunID:       snapshot.CI.RunID,
		RunAttempt:  snapshot.CI.RunAttempt,
		Matrix:      snapshot.CI.Matrix,
		TestCommand: snapshot.Test.Command,
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	if snapshot.CI.Provider != evidenceProvider {
		return fmt.Errorf("CI provider %q, want %q", snapshot.CI.Provider, evidenceProvider)
	}
	if err := validateText("go toolchain version", snapshot.Toolchain.GoVersion); err != nil {
		return err
	}
	if snapshot.Test.Result != "pass" {
		return fmt.Errorf("test result %q, want pass", snapshot.Test.Result)
	}
	if snapshot.Test.LogFile == "" ||
		filepath.Base(snapshot.Test.LogFile) != snapshot.Test.LogFile ||
		strings.ContainsAny(snapshot.Test.LogFile, `/\`) {
		return errors.New("test log must be a base filename")
	}
	if !digestPattern.MatchString(snapshot.Test.LogSHA256) {
		return errors.New("test log SHA-256 must be 64 lowercase hexadecimal characters")
	}
	if snapshot.Diagnostics.SchemaVersion != robotgo.RuntimeDiagnosticsSchemaVersion {
		return fmt.Errorf(
			"runtime diagnostics schema %q, want %q",
			snapshot.Diagnostics.SchemaVersion,
			robotgo.RuntimeDiagnosticsSchemaVersion,
		)
	}
	expectedGOOS, expectedCGO, err := matrixRuntime(snapshot.CI.Matrix)
	if err != nil {
		return err
	}
	if snapshot.Diagnostics.Runtime.GOOS != expectedGOOS {
		return fmt.Errorf(
			"matrix %q requires GOOS %q, got %q",
			snapshot.CI.Matrix,
			expectedGOOS,
			snapshot.Diagnostics.Runtime.GOOS,
		)
	}
	if snapshot.Diagnostics.Runtime.CGOEnabled != expectedCGO {
		return fmt.Errorf(
			"matrix %q requires CGO state %t, got %t",
			snapshot.CI.Matrix,
			expectedCGO,
			snapshot.Diagnostics.Runtime.CGOEnabled,
		)
	}
	identityFields := []struct {
		name  string
		value string
	}{
		{name: "runtime GOARCH", value: snapshot.Diagnostics.Runtime.GOARCH},
		{name: "RobotGo version", value: snapshot.Diagnostics.Runtime.RobotGoVersion},
		{
			name:  "runtime build implementation",
			value: string(snapshot.Diagnostics.Runtime.BuildImplementation),
		},
	}
	for _, field := range identityFields {
		if err := validateText(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateText(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > maxTextLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxTextLength)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%s contains a control character", name)
		}
	}
	return nil
}

func matrixRuntime(matrix string) (string, bool, error) {
	switch matrix {
	case "linux-native", "linux-native-validation":
		return "linux", true, nil
	case "linux-purego":
		return "linux", false, nil
	case "macos-native":
		return "darwin", true, nil
	case "macos-purego":
		return "darwin", false, nil
	case "windows-native":
		return "windows", true, nil
	case "windows-purego":
		return "windows", false, nil
	default:
		return "", false, fmt.Errorf("unsupported release matrix %q", matrix)
	}
}

func digestRegularFile(path string) (string, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", fmt.Errorf("inspect %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open %q: %w", path, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", "", fmt.Errorf("hash %q: %w", path, copyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("close %q: %w", path, closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), filepath.Base(path), nil
}
