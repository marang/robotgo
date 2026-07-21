package compositorevidence

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	outputSentinelName    = ".robotgo-compositor-evidence"
	outputSentinelContent = "robotgo-compositor-evidence-v1\n"
	validatedMarkerName   = ".validated"
	preflightFileName     = ".preflight.json"
	rawTestLogFileName    = "raw-test.log"
	testLogFileName       = "test.log"
	evidenceFileName      = "evidence.json"
	summaryFileName       = "summary.md"
	canonicalLogSchema    = "robotgo-compositor-test-log-v1"
)

var (
	runIDPattern            = regexp.MustCompile(`^[1-9][0-9]*$`)
	staticSensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|credential|secret|restore[ _-]?token)`),
		regexp.MustCompile(`(?i)(session[ _-]?handle|request[ _-]?handle|node[ _-]?id)`),
		regexp.MustCompile(`(?i)(wayland_display|dbus_session_bus_address)`),
		regexp.MustCompile(`(?i)(clipboard contents?|screen[ _-]?shot|frame pixels?)`),
		regexp.MustCompile(`(?:^|[[:space:]"'])/(?:home|Users)/`),
	}
)

// CI identifies the protected GitHub Actions execution.
type CI struct {
	Provider   string `json:"provider"`
	Workflow   string `json:"workflow"`
	RunID      string `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
}

// EvidenceTest records the canonical, privacy-safe integration result.
type EvidenceTest struct {
	Package        string `json:"package"`
	Name           string `json:"name"`
	Command        string `json:"command"`
	Result         string `json:"result"`
	DurationMillis int64  `json:"duration_ms"`
	LogFile        string `json:"log_file"`
	LogSHA256      string `json:"log_sha256"`
}

// Manifest is the schema-v1 protected compositor evidence document.
type Manifest struct {
	SchemaVersion string       `json:"schema_version"`
	CompletedAt   string       `json:"completed_at"`
	Commit        string       `json:"commit"`
	Ref           string       `json:"ref"`
	CI            CI           `json:"ci"`
	Platform      Platform     `json:"platform"`
	Desktop       Desktop      `json:"desktop"`
	Test          EvidenceTest `json:"test"`
}

// FinalizeConfig binds a passed raw integration test to its protected workflow
// identity. SensitiveValues are checked but never persisted.
type FinalizeConfig struct {
	RunnerTemp      string
	OutputDirectory string
	ExpectedCommit  string
	ExpectedLane    Lane
	ExpectedCell    Cell
	Workflow        string
	RunID           string
	RunAttempt      int
	TestExitCode    int
	CompletedAt     time.Time
	SensitiveValues []string
}

// VerifyExpectations bind existing evidence to an exact protected job.
type VerifyExpectations struct {
	Commit     string
	Lane       Lane
	Cell       Cell
	Workflow   string
	RunID      string
	RunAttempt int
}

// PrepareOutputDirectory creates a new evidence directory inside runnerTemp.
// The ownership sentinel lets Cleanup reject unrelated paths.
func PrepareOutputDirectory(runnerTemp, outputDirectory string) error {
	if err := validateOutputPath(runnerTemp, outputDirectory); err != nil {
		return err
	}
	if err := os.Mkdir(outputDirectory, 0o700); err != nil {
		return fmt.Errorf("create compositor evidence directory: %w", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.RemoveAll(outputDirectory)
		}
	}()
	if err := atomicWrite(
		filepath.Join(outputDirectory, outputSentinelName),
		[]byte(outputSentinelContent),
		0o600,
	); err != nil {
		return err
	}
	created = false
	return nil
}

// RawTestLogPath returns the fixed private log path owned by an evidence
// directory. The file is removed during finalization or cleanup.
func RawTestLogPath(outputDirectory string) string {
	return filepath.Join(outputDirectory, rawTestLogFileName)
}

// WritePreflight stores a validated intermediate report transactionally.
func WritePreflight(outputDirectory string, report PreflightReport) error {
	if err := requireOwnedOutput(outputDirectory); err != nil {
		return err
	}
	if err := validatePreflightReport(report); err != nil {
		return err
	}
	data, err := marshalDocument(report)
	if err != nil {
		return fmt.Errorf("marshal compositor preflight: %w", err)
	}
	return atomicWrite(filepath.Join(outputDirectory, preflightFileName), data, 0o600)
}

// Finalize converts a passed raw Go test log into canonical sanitized evidence,
// verifies the complete directory, and writes the validated marker last.
func Finalize(config FinalizeConfig) (_ Manifest, retErr error) {
	if err := validateFinalizeConfig(config); err != nil {
		return Manifest{}, err
	}
	if err := requireOwnedOutput(config.OutputDirectory); err != nil {
		return Manifest{}, err
	}
	rawPath := RawTestLogPath(config.OutputDirectory)
	defer func() {
		if removeErr := os.Remove(rawPath); removeErr != nil &&
			!errors.Is(removeErr, os.ErrNotExist) && retErr == nil {
			retErr = fmt.Errorf("remove private raw test log: %w", removeErr)
		}
	}()
	if config.TestExitCode != 0 {
		return Manifest{}, errors.New("protected integration test failed")
	}

	report, err := readPreflight(config.OutputDirectory)
	if err != nil {
		return Manifest{}, err
	}
	if report.Commit != config.ExpectedCommit ||
		report.Desktop.Lane != config.ExpectedLane ||
		report.Desktop.Cell != config.ExpectedCell ||
		report.Workflow != config.Workflow ||
		report.RunID != config.RunID ||
		report.RunAttempt != config.RunAttempt {
		return Manifest{}, errors.New("preflight identity does not match finalization request")
	}
	spec, err := config.ExpectedCell.TestSpec()
	if err != nil {
		return Manifest{}, err
	}
	rawLog, err := readRegularFile(rawPath, maxLogBytes)
	if err != nil {
		return Manifest{}, fmt.Errorf("private test log: %w", err)
	}
	if err := rejectSensitive(rawLog, config.SensitiveValues); err != nil {
		return Manifest{}, err
	}
	duration, err := parsePassedGoTest(rawLog, spec)
	if err != nil {
		return Manifest{}, err
	}
	canonicalLog := canonicalTestLog(spec, duration)
	if err := rejectSensitive(canonicalLog, nil); err != nil {
		return Manifest{}, err
	}
	logDigest := sha256Hex(canonicalLog)

	completedAt := config.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		CompletedAt:   completedAt.UTC().Format(time.RFC3339),
		Commit:        report.Commit,
		Ref:           report.Ref,
		CI: CI{
			Provider:   evidenceProvider,
			Workflow:   report.Workflow,
			RunID:      report.RunID,
			RunAttempt: report.RunAttempt,
		},
		Platform: report.Platform,
		Desktop:  report.Desktop,
		Test: EvidenceTest{
			Package:        spec.Package,
			Name:           spec.Name,
			Command:        spec.Command,
			Result:         "pass",
			DurationMillis: durationMilliseconds(duration),
			LogFile:        testLogFileName,
			LogSHA256:      logDigest,
		},
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	manifestData, err := marshalDocument(manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("marshal compositor evidence: %w", err)
	}
	if err := rejectSensitive(manifestData, config.SensitiveValues); err != nil {
		return Manifest{}, err
	}
	summary := renderSummary(manifest)
	if err := rejectSensitive(summary, config.SensitiveValues); err != nil {
		return Manifest{}, err
	}

	created := []string{}
	defer func() {
		if retErr == nil {
			return
		}
		for _, path := range created {
			_ = os.Remove(path)
		}
	}()
	for _, output := range []struct {
		name string
		data []byte
	}{
		{name: testLogFileName, data: canonicalLog},
		{name: evidenceFileName, data: manifestData},
		{name: summaryFileName, data: summary},
	} {
		path := filepath.Join(config.OutputDirectory, output.name)
		if err := atomicWrite(path, output.data, 0o600); err != nil {
			return Manifest{}, err
		}
		created = append(created, path)
	}
	if err := os.Remove(filepath.Join(config.OutputDirectory, preflightFileName)); err != nil {
		return Manifest{}, fmt.Errorf("remove private preflight report: %w", err)
	}
	if err := os.Remove(rawPath); err != nil {
		return Manifest{}, fmt.Errorf("remove private raw test log: %w", err)
	}
	if _, err := verifyDirectory(config.OutputDirectory, VerifyExpectations{
		Commit:     config.ExpectedCommit,
		Lane:       config.ExpectedLane,
		Cell:       config.ExpectedCell,
		Workflow:   config.Workflow,
		RunID:      config.RunID,
		RunAttempt: config.RunAttempt,
	}, false); err != nil {
		return Manifest{}, err
	}
	marker := []byte("sha256=" + sha256Hex(manifestData) + "\n")
	markerPath := filepath.Join(config.OutputDirectory, validatedMarkerName)
	if err := atomicWrite(markerPath, marker, 0o600); err != nil {
		return Manifest{}, err
	}
	created = append(created, markerPath)
	if _, err := verifyDirectory(config.OutputDirectory, VerifyExpectations{
		Commit:     config.ExpectedCommit,
		Lane:       config.ExpectedLane,
		Cell:       config.ExpectedCell,
		Workflow:   config.Workflow,
		RunID:      config.RunID,
		RunAttempt: config.RunAttempt,
	}, true); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// VerifyDirectory validates the schema, exact identity, canonical log digest,
// sensitive-data denylist, summary, marker, and directory file allowlist.
func VerifyDirectory(
	outputDirectory string,
	expected VerifyExpectations,
) (Manifest, error) {
	return verifyDirectory(outputDirectory, expected, true)
}

func verifyDirectory(
	outputDirectory string,
	expected VerifyExpectations,
	requireMarker bool,
) (Manifest, error) {
	if err := requireOwnedOutput(outputDirectory); err != nil {
		return Manifest{}, err
	}
	if err := validateVerifyExpectations(expected); err != nil {
		return Manifest{}, err
	}
	allowed := map[string]bool{
		outputSentinelName:  true,
		testLogFileName:     true,
		evidenceFileName:    true,
		summaryFileName:     true,
		validatedMarkerName: requireMarker,
	}
	entries, err := os.ReadDir(outputDirectory)
	if err != nil {
		return Manifest{}, fmt.Errorf("read evidence directory: %w", err)
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return Manifest{}, errors.New("evidence directory contains an unexpected file")
		}
	}
	for name, required := range allowed {
		if !required {
			continue
		}
		if _, err := os.Lstat(filepath.Join(outputDirectory, name)); err != nil {
			return Manifest{}, fmt.Errorf("required evidence file %q is unavailable", name)
		}
	}

	manifestData, err := readRegularFile(
		filepath.Join(outputDirectory, evidenceFileName),
		maxEvidenceBytes,
	)
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectSensitive(manifestData, nil); err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrict(manifestData, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode compositor evidence: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	if err := matchExpectations(manifest, expected); err != nil {
		return Manifest{}, err
	}
	testLog, err := readRegularFile(filepath.Join(outputDirectory, testLogFileName), maxLogBytes)
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectSensitive(testLog, nil); err != nil {
		return Manifest{}, err
	}
	if sha256Hex(testLog) != manifest.Test.LogSHA256 {
		return Manifest{}, errors.New("canonical test log digest does not match evidence")
	}
	if !bytes.Equal(testLog, canonicalTestLog(TestSpec{
		Package: manifest.Test.Package,
		Name:    manifest.Test.Name,
		Command: manifest.Test.Command,
	}, time.Duration(manifest.Test.DurationMillis)*time.Millisecond)) {
		return Manifest{}, errors.New("test log is not canonical")
	}
	summary, err := readRegularFile(filepath.Join(outputDirectory, summaryFileName), maxLogBytes)
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectSensitive(summary, nil); err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(summary, renderSummary(manifest)) {
		return Manifest{}, errors.New("evidence summary does not match manifest")
	}
	if requireMarker {
		marker, err := readRegularFile(filepath.Join(outputDirectory, validatedMarkerName), 128)
		if err != nil {
			return Manifest{}, err
		}
		wantMarker := "sha256=" + sha256Hex(manifestData) + "\n"
		if string(marker) != wantMarker {
			return Manifest{}, errors.New("validated marker does not match evidence")
		}
	}
	return manifest, nil
}

// Cleanup removes an owned evidence directory and refuses paths without the
// repository sentinel or outside runnerTemp.
func Cleanup(runnerTemp, outputDirectory string) error {
	if err := validateOutputPath(runnerTemp, outputDirectory); err != nil {
		return err
	}
	if _, err := os.Lstat(outputDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect evidence directory: %w", err)
	}
	if err := requireOwnedOutput(outputDirectory); err != nil {
		return err
	}
	if err := os.RemoveAll(outputDirectory); err != nil {
		return fmt.Errorf("remove compositor evidence directory: %w", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("compositor evidence directory survived cleanup")
		}
		return fmt.Errorf("verify compositor evidence cleanup: %w", err)
	}
	return nil
}

func validateOutputPath(runnerTemp, outputDirectory string) error {
	if !filepath.IsAbs(runnerTemp) || filepath.Clean(runnerTemp) != runnerTemp {
		return errors.New("runner temporary directory must be a clean absolute path")
	}
	if !filepath.IsAbs(outputDirectory) || filepath.Clean(outputDirectory) != outputDirectory {
		return errors.New("evidence output directory must be a clean absolute path")
	}
	if filepath.Dir(outputDirectory) != runnerTemp {
		return errors.New("evidence output directory must be a direct child of runner temporary storage")
	}
	info, err := os.Lstat(runnerTemp)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runner temporary directory is unavailable")
	}
	return nil
}

func requireOwnedOutput(outputDirectory string) error {
	info, err := os.Lstat(outputDirectory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("compositor evidence directory is unavailable")
	}
	sentinel, err := readRegularFile(
		filepath.Join(outputDirectory, outputSentinelName),
		len(outputSentinelContent),
	)
	if err != nil || string(sentinel) != outputSentinelContent {
		return errors.New("compositor evidence ownership sentinel is invalid")
	}
	return nil
}

func readPreflight(outputDirectory string) (PreflightReport, error) {
	data, err := readRegularFile(
		filepath.Join(outputDirectory, preflightFileName),
		maxEvidenceBytes,
	)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("read compositor preflight: %w", err)
	}
	var report PreflightReport
	if err := decodeStrict(data, &report); err != nil {
		return PreflightReport{}, fmt.Errorf("decode compositor preflight: %w", err)
	}
	if err := validatePreflightReport(report); err != nil {
		return PreflightReport{}, err
	}
	return report, nil
}

func validateFinalizeConfig(config FinalizeConfig) error {
	if err := validateOutputPath(config.RunnerTemp, config.OutputDirectory); err != nil {
		return err
	}
	if !gitObjectPattern.MatchString(config.ExpectedCommit) {
		return errors.New("expected commit is invalid")
	}
	if err := validateLaneCell(config.ExpectedLane, config.ExpectedCell); err != nil {
		return err
	}
	if _, err := config.ExpectedCell.TestSpec(); err != nil {
		return err
	}
	if err := validateWorkflow(config.ExpectedCell, config.Workflow); err != nil {
		return err
	}
	if !runIDPattern.MatchString(config.RunID) {
		return errors.New("run ID must be a positive decimal integer")
	}
	if config.RunAttempt <= 0 {
		return errors.New("run attempt must be positive")
	}
	if config.TestExitCode < 0 {
		return errors.New("test exit code must not be negative")
	}
	return nil
}

func validateVerifyExpectations(expected VerifyExpectations) error {
	if !gitObjectPattern.MatchString(expected.Commit) {
		return errors.New("expected commit is invalid")
	}
	if err := validateLaneCell(expected.Lane, expected.Cell); err != nil {
		return err
	}
	if err := validateWorkflow(expected.Cell, expected.Workflow); err != nil {
		return err
	}
	if !runIDPattern.MatchString(expected.RunID) {
		return errors.New("expected run ID must be a positive decimal integer")
	}
	if expected.RunAttempt <= 0 {
		return errors.New("expected run attempt must be positive")
	}
	return nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("compositor evidence schema %q, want %q", manifest.SchemaVersion, SchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, manifest.CompletedAt); err != nil {
		return errors.New("completed timestamp must be RFC3339 UTC")
	}
	if !strings.HasSuffix(manifest.CompletedAt, "Z") {
		return errors.New("completed timestamp must use UTC")
	}
	if err := validateGitIdentity(manifest.Commit, manifest.Ref); err != nil {
		return err
	}
	if manifest.CI.Provider != evidenceProvider {
		return errors.New("unsupported compositor evidence provider")
	}
	if err := validateText("workflow", manifest.CI.Workflow); err != nil {
		return err
	}
	if !runIDPattern.MatchString(manifest.CI.RunID) || manifest.CI.RunAttempt <= 0 {
		return errors.New("invalid compositor evidence run identity")
	}
	if err := validatePreflightReport(PreflightReport{
		SchemaVersion: preflightSchemaVersion,
		Commit:        manifest.Commit,
		Ref:           manifest.Ref,
		Workflow:      manifest.CI.Workflow,
		RunID:         manifest.CI.RunID,
		RunAttempt:    manifest.CI.RunAttempt,
		Platform:      manifest.Platform,
		Desktop:       manifest.Desktop,
	}); err != nil {
		return err
	}
	spec, err := manifest.Desktop.Cell.TestSpec()
	if err != nil {
		return err
	}
	if manifest.Test.Package != spec.Package || manifest.Test.Name != spec.Name ||
		manifest.Test.Command != spec.Command {
		return errors.New("test identity does not match evidence cell")
	}
	if manifest.Test.Result != "pass" || manifest.Test.DurationMillis < 0 {
		return errors.New("compositor evidence test did not pass")
	}
	if manifest.Test.LogFile != testLogFileName {
		return errors.New("compositor evidence test log name is invalid")
	}
	if !digestPattern.MatchString(manifest.Test.LogSHA256) {
		return errors.New("test log SHA-256 is invalid")
	}
	return nil
}

func matchExpectations(manifest Manifest, expected VerifyExpectations) error {
	comparisons := []struct {
		name string
		got  string
		want string
	}{
		{name: "commit", got: manifest.Commit, want: expected.Commit},
		{name: "lane", got: string(manifest.Desktop.Lane), want: string(expected.Lane)},
		{name: "cell", got: string(manifest.Desktop.Cell), want: string(expected.Cell)},
		{name: "workflow", got: manifest.CI.Workflow, want: expected.Workflow},
		{name: "run ID", got: manifest.CI.RunID, want: expected.RunID},
	}
	for _, comparison := range comparisons {
		if comparison.got != comparison.want {
			return fmt.Errorf("%s does not match expected protected job", comparison.name)
		}
	}
	if manifest.CI.RunAttempt != expected.RunAttempt {
		return errors.New("run attempt does not match expected protected job")
	}
	return nil
}

func parsePassedGoTest(data []byte, spec TestSpec) (time.Duration, error) {
	if !utf8Safe(data) {
		return 0, errors.New("private test log is not valid bounded text")
	}
	runLine := "=== RUN   " + spec.Name
	passPattern := regexp.MustCompile(`^--- PASS: ` + regexp.QuoteMeta(spec.Name) + ` \(([^)]+)\)$`)
	packagePattern := regexp.MustCompile(
		`^ok  \t` + regexp.QuoteMeta(spec.Package) + `\t([0-9]+(?:\.[0-9]+)?s)$`,
	)
	const (
		awaitRun = iota
		awaitTestPass
		awaitOverallPass
		awaitPackagePass
		complete
	)
	state := awaitRun
	var duration time.Duration
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if state == complete && line != "" {
			return 0, errors.New("private test log contains trailing output")
		}
		switch {
		case line == runLine:
			if state != awaitRun {
				return 0, errors.New("integration test run marker is out of order or duplicated")
			}
			state = awaitTestPass
		case strings.HasPrefix(line, "=== RUN"):
			return 0, errors.New("private test log contains an unexpected test run")
		case passPattern.MatchString(line):
			if state != awaitTestPass {
				return 0, errors.New("integration test pass marker is out of order or duplicated")
			}
			match := passPattern.FindStringSubmatch(line)
			parsed, err := time.ParseDuration(match[1])
			if err != nil || parsed < 0 {
				return 0, errors.New("integration test duration is malformed")
			}
			duration = parsed
			state = awaitOverallPass
		case strings.HasPrefix(line, "--- PASS:"):
			return 0, errors.New("private test log contains an unexpected passing test")
		case line == "PASS":
			if state != awaitOverallPass {
				return 0, errors.New("overall pass marker is out of order or duplicated")
			}
			state = awaitPackagePass
		case packagePattern.MatchString(line):
			if state != awaitPackagePass {
				return 0, errors.New("package pass marker is out of order or duplicated")
			}
			match := packagePattern.FindStringSubmatch(line)
			if _, err := time.ParseDuration(match[1]); err != nil {
				return 0, errors.New("package pass duration is malformed")
			}
			state = complete
		case strings.HasPrefix(line, "ok  \t"):
			return 0, errors.New("private test log contains an unexpected package result")
		case strings.HasPrefix(line, "--- FAIL:") ||
			strings.HasPrefix(line, "--- SKIP:") || line == "FAIL":
			return 0, errors.New("integration test did not produce a non-skipping pass")
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, errors.New("scan private test log")
	}
	if state != complete {
		return 0, errors.New("integration test pass evidence is incomplete")
	}
	return duration, nil
}

func canonicalTestLog(spec TestSpec, duration time.Duration) []byte {
	return []byte(fmt.Sprintf(
		"schema=%s\npackage=%s\ntest=%s\nresult=pass\nduration_ms=%d\n",
		canonicalLogSchema,
		spec.Package,
		spec.Name,
		durationMilliseconds(duration),
	))
}

func durationMilliseconds(duration time.Duration) int64 {
	milliseconds := duration.Milliseconds()
	if duration > 0 && milliseconds == 0 {
		return 1
	}
	return milliseconds
}

func renderSummary(manifest Manifest) []byte {
	return []byte(fmt.Sprintf(
		"### Protected compositor evidence\n\n- Status: validated\n- Lane: `%s`\n- Cell: `%s`\n- Commit: `%s`\n- Outputs: `%d`\n- Test: `%s`\n",
		manifest.Desktop.Lane,
		manifest.Desktop.Cell,
		manifest.Commit,
		manifest.Desktop.OutputCount,
		manifest.Test.Name,
	))
}

func rejectSensitive(data []byte, sensitiveValues []string) error {
	for _, pattern := range staticSensitivePatterns {
		if pattern.FindIndex(data) != nil {
			return errors.New("evidence contains a sensitive-data denylist match")
		}
	}
	for _, value := range sensitiveValues {
		value = strings.TrimSpace(value)
		if len(value) >= 4 && bytes.Contains(data, []byte(value)) {
			return errors.New("evidence contains private runtime identity")
		}
	}
	return nil
}

func marshalDocument(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func readRegularFile(path string, maximum int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("evidence input is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, errors.New("evidence input changed during validation")
	}
	if openedInfo.Size() > int64(maximum) {
		return nil, fmt.Errorf("evidence input exceeds %d bytes", maximum)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, fmt.Errorf("evidence input exceeds %d bytes", maximum)
	}
	return data, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) (retErr error) {
	if _, err := os.Lstat(path); err == nil {
		return errors.New("evidence output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect evidence output: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".robotgo-evidence-*")
	if err != nil {
		return fmt.Errorf("create transactional evidence output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set evidence output permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write transactional evidence output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync transactional evidence output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close transactional evidence output: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("publish transactional evidence output: %w", err)
	}
	return nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func utf8Safe(data []byte) bool {
	if !bytes.Equal(bytes.ToValidUTF8(data, nil), data) {
		return false
	}
	for _, char := range string(data) {
		if (char < 0x20 && char != '\n' && char != '\t') || char == 0x7f {
			return false
		}
	}
	return true
}
