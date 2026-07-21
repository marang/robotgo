package compositorevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/marang/robotgo/internal/command"
)

const (
	portalBusName            = "org.freedesktop.portal.Desktop"
	portalObjectPath         = "/org/freedesktop/portal/desktop"
	remoteDesktopInterface   = "org.freedesktop.portal.RemoteDesktop"
	screenCastInterface      = "org.freedesktop.portal.ScreenCast"
	portalVersionProperty    = "version"
	availableSourcesProperty = "AvailableSourceTypes"
	pipeWirePackage          = "libpipewire-0.3"
	operatorReadyContent     = "ready"
	defaultOSReleasePath     = "/etc/os-release"
	maxProbeOutputBytes      = 4096
)

// PreflightConfig contains trusted workflow inputs and private session values.
// Private values are validated but never copied into the report.
type PreflightConfig struct {
	Lane                Lane
	Cell                Cell
	CheckoutCommit      string
	ExpectedCommit      string
	Ref                 string
	Workflow            string
	RunID               string
	RunAttempt          int
	CurrentDesktop      string
	WaylandDisplay      string
	RuntimeDir          string
	SessionBusAddress   string
	OperatorReadyPath   string
	OutputCount         int
	MinimumOutputCount  int
	RequireHeadlessSway bool
	ProbeTimeout        time.Duration
}

// Platform identifies a sanitized runner software platform.
type Platform struct {
	OSID          string `json:"os_id"`
	OSVersion     string `json:"os_version"`
	KernelName    string `json:"kernel_name"`
	KernelRelease string `json:"kernel_release"`
	Architecture  string `json:"architecture"`
	GoVersion     string `json:"go_version"`
}

// Portal identifies the selected real desktop portal contract. Empty versions
// are allowed only for an explicit portal-availability cell.
type Portal struct {
	Observed             bool   `json:"observed"`
	Available            bool   `json:"available"`
	FrontendVersion      string `json:"frontend_version"`
	Backend              string `json:"backend"`
	BackendVersion       string `json:"backend_version"`
	RemoteDesktopVersion uint64 `json:"remote_desktop_version"`
	ScreenCastVersion    uint64 `json:"screencast_version"`
	AvailableSources     uint64 `json:"available_sources"`
}

// PipeWire identifies the persistent capture runtime required by ScreenCast.
type PipeWire struct {
	Required bool   `json:"required"`
	Version  string `json:"version"`
}

// Desktop identifies only allowlisted compositor facts required by evidence.
type Desktop struct {
	Lane              Lane     `json:"lane"`
	Cell              Cell     `json:"cell"`
	Compositor        string   `json:"compositor"`
	CompositorVersion string   `json:"compositor_version"`
	OutputCount       int      `json:"output_count"`
	OperatorReady     bool     `json:"operator_ready"`
	Portal            Portal   `json:"portal"`
	PipeWire          PipeWire `json:"pipewire"`
}

// PreflightReport is the private intermediate document consumed by finalization.
type PreflightReport struct {
	SchemaVersion string   `json:"schema_version"`
	Commit        string   `json:"commit"`
	Ref           string   `json:"ref"`
	Workflow      string   `json:"workflow"`
	RunID         string   `json:"run_id"`
	RunAttempt    int      `json:"run_attempt"`
	Platform      Platform `json:"platform"`
	Desktop       Desktop  `json:"desktop"`
}

type preflightDependencies struct {
	output        func(context.Context, string, ...string) ([]byte, error)
	socketReady   func(string, string) error
	operatorReady func(string, string) error
	readFile      func(string) ([]byte, error)
}

// Preflight validates the selected runner cell and returns a sanitized report.
func Preflight(ctx context.Context, config PreflightConfig) (PreflightReport, error) {
	return preflight(ctx, config, preflightDependencies{
		output:        command.Output,
		socketReady:   validateWaylandSocket,
		operatorReady: validateOperatorReady,
		readFile:      os.ReadFile,
	})
}

func preflight(
	ctx context.Context,
	config PreflightConfig,
	dependencies preflightDependencies,
) (PreflightReport, error) {
	if err := validatePreflightConfig(config); err != nil {
		return PreflightReport{}, err
	}
	if dependencies.output == nil || dependencies.socketReady == nil ||
		dependencies.operatorReady == nil || dependencies.readFile == nil {
		return PreflightReport{}, errors.New("preflight dependencies are incomplete")
	}
	if err := dependencies.socketReady(config.RuntimeDir, config.WaylandDisplay); err != nil {
		return PreflightReport{}, fmt.Errorf("wayland socket check failed: %w", err)
	}
	if strings.TrimSpace(config.SessionBusAddress) == "" {
		return PreflightReport{}, errors.New("session bus check failed")
	}

	compositor, err := matchDesktop(config.Lane, config.CurrentDesktop)
	if err != nil {
		return PreflightReport{}, err
	}
	if config.Cell.NativeRequired() && compositor != "sway" {
		return PreflightReport{}, errors.New("protected native wlroots cells currently require the pinned Sway image")
	}

	probe := commandProbe{
		output:  dependencies.output,
		timeout: config.ProbeTimeout,
	}
	if err := probeBusName(ctx, probe, "org.freedesktop.DBus"); err != nil {
		return PreflightReport{}, fmt.Errorf("session bus check failed: %w", err)
	}
	platform, err := probePlatform(ctx, dependencies.readFile, probe)
	if err != nil {
		return PreflightReport{}, err
	}
	compositorVersion, err := probe.packageVersion(ctx, compositorPackage(config.Lane))
	if err != nil {
		return PreflightReport{}, fmt.Errorf("compositor version check failed: %w", err)
	}

	desktop := Desktop{
		Lane:              config.Lane,
		Cell:              config.Cell,
		Compositor:        compositor,
		CompositorVersion: compositorVersion,
		OutputCount:       config.OutputCount,
		PipeWire: PipeWire{
			Required: config.Cell.PipeWireRequired(),
		},
	}
	if config.Lane == LaneWlroots {
		if err := probeSway(
			ctx,
			probe,
			config.OutputCount,
			config.RequireHeadlessSway,
		); err != nil {
			return PreflightReport{}, err
		}
	}
	if config.Cell.PortalObserved() {
		desktop.Portal, err = probePortal(ctx, probe, config.Lane, config.Cell)
		if err != nil {
			return PreflightReport{}, err
		}
	}
	if config.Cell.PipeWireRequired() {
		desktop.PipeWire.Version, err = probePipeWire(ctx, probe)
		if err != nil {
			return PreflightReport{}, err
		}
	}
	if config.Cell.ConsentRequired() {
		if err := dependencies.operatorReady(
			config.OperatorReadyPath,
			operatorAttestation(config),
		); err != nil {
			return PreflightReport{}, fmt.Errorf("operator console readiness check failed: %w", err)
		}
		desktop.OperatorReady = true
	}

	report := PreflightReport{
		SchemaVersion: preflightSchemaVersion,
		Commit:        config.CheckoutCommit,
		Ref:           config.Ref,
		Workflow:      config.Workflow,
		RunID:         config.RunID,
		RunAttempt:    config.RunAttempt,
		Platform:      platform,
		Desktop:       desktop,
	}
	if err := validatePreflightReport(report); err != nil {
		return PreflightReport{}, err
	}
	return report, nil
}

func validatePreflightConfig(config PreflightConfig) error {
	if err := validateLaneCell(config.Lane, config.Cell); err != nil {
		return err
	}
	if err := validateGitIdentity(config.CheckoutCommit, config.Ref); err != nil {
		return err
	}
	if !gitObjectPattern.MatchString(config.ExpectedCommit) {
		return errors.New("expected commit must be a lowercase 40- or 64-character Git object ID")
	}
	if config.CheckoutCommit != config.ExpectedCommit {
		return errors.New("checked-out commit does not match the approved commit")
	}
	if err := validateWorkflow(config.Cell, config.Workflow); err != nil {
		return err
	}
	if !runIDPattern.MatchString(config.RunID) || config.RunAttempt <= 0 {
		return errors.New("preflight run identity is invalid")
	}
	if config.OutputCount <= 0 {
		return errors.New("declared output count must be positive")
	}
	if config.MinimumOutputCount <= 0 {
		return errors.New("minimum output count must be positive")
	}
	if config.OutputCount < config.MinimumOutputCount {
		return fmt.Errorf("declared output count is below the required minimum %d", config.MinimumOutputCount)
	}
	if config.RequireHeadlessSway && config.Lane != LaneWlroots {
		return errors.New("headless Sway isolation requires the wlroots lane")
	}
	return nil
}

func matchDesktop(lane Lane, current string) (string, error) {
	tokens := strings.FieldsFunc(strings.ToLower(current), func(char rune) bool {
		switch char {
		case ':', ';', ',', ' ', '\t':
			return true
		default:
			return false
		}
	})
	contains := func(values ...string) bool {
		for _, token := range tokens {
			for _, value := range values {
				if token == value {
					return true
				}
			}
		}
		return false
	}
	switch lane {
	case LaneGNOME:
		if contains("gnome", "gnome-classic") {
			return "gnome", nil
		}
	case LaneKDE:
		if contains("kde", "plasma") {
			return "kde", nil
		}
	case LaneWlroots:
		for _, compositor := range []string{
			"sway", "hyprland", "wayfire", "river", "labwc", "dwl", "gamescope",
		} {
			if contains(compositor) {
				return compositor, nil
			}
		}
	}
	return "", fmt.Errorf("desktop identity does not match protected lane %q", lane)
}

func validateWaylandSocket(runtimeDir, display string) error {
	if !filepath.IsAbs(runtimeDir) || filepath.Clean(runtimeDir) != runtimeDir {
		return errors.New("runtime directory must be a clean absolute path")
	}
	if display == "" || filepath.Base(display) != display ||
		strings.ContainsAny(display, `/\\`) {
		return errors.New("wayland display must be a base name")
	}
	info, err := os.Lstat(filepath.Join(runtimeDir, display))
	if err != nil {
		return errors.New("wayland display is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("wayland display is not a socket")
	}
	return nil
}

func validateOperatorReady(path, expected string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("readiness path must be a clean absolute path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("operator console is not ready")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxTextLength {
		return errors.New("operator readiness attestation is invalid")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("operator readiness attestation is writable by group or others")
	}
	if err := validateOperatorReadyOwner(info); err != nil {
		return err
	}
	parentInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("operator readiness directory is invalid")
	}
	if parentInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("operator readiness directory is writable by group or others")
	}
	if err := validateOperatorReadyOwner(parentInfo); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil || !validOperatorAttestation(data, expected) {
		return errors.New("operator readiness attestation is invalid")
	}
	return nil
}

func validOperatorAttestation(data []byte, expected string) bool {
	return string(data) == expected || string(data) == expected+"\n"
}

func operatorAttestation(config PreflightConfig) string {
	return fmt.Sprintf(
		"%s commit=%s run=%s attempt=%d lane=%s cell=%s",
		operatorReadyContent,
		config.ExpectedCommit,
		config.RunID,
		config.RunAttempt,
		config.Lane,
		config.Cell,
	)
}

type commandProbe struct {
	output  func(context.Context, string, ...string) ([]byte, error)
	timeout time.Duration
}

type probeFailure uint8

const (
	probeFailureInfrastructure probeFailure = iota
	probeFailureExecutableMissing
	probeFailureUnavailable
)

type probeError struct {
	failure probeFailure
}

func (err *probeError) Error() string {
	switch err.failure {
	case probeFailureExecutableMissing:
		return "probe executable is unavailable"
	case probeFailureUnavailable:
		return "probed capability is unavailable"
	default:
		return "probe command failed"
	}
}

func probeFailureIs(err error, failure probeFailure) bool {
	var classified *probeError
	return errors.As(err, &classified) && classified.failure == failure
}

func (probe commandProbe) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	timeout := probe.timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := probe.output(probeContext, name, args...)
	if err != nil {
		if errors.Is(probeContext.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("probe timed out: %w", &probeError{failure: probeFailureInfrastructure})
		}
		var classified *probeError
		if errors.As(err, &classified) {
			return nil, classified
		}
		var executableError *exec.Error
		if errors.As(err, &executableError) {
			return nil, &probeError{failure: probeFailureExecutableMissing}
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, &probeError{failure: probeFailureUnavailable}
		}
		return nil, &probeError{failure: probeFailureInfrastructure}
	}
	if len(output) > maxProbeOutputBytes {
		return nil, errors.New("probe output exceeds safety limit")
	}
	return output, nil
}

func (probe commandProbe) packageVersion(ctx context.Context, packageName string) (string, error) {
	output, err := probe.run(ctx, "dpkg-query", "-W", "-f=${Version}", packageName)
	if err != nil && probeFailureIs(err, probeFailureUnavailable) {
		return "", fmt.Errorf("package version is unavailable: %w", err)
	}
	if err != nil && !probeFailureIs(err, probeFailureExecutableMissing) {
		return "", fmt.Errorf("package version probe failed: %w", err)
	}
	if err != nil {
		output, err = probe.run(
			ctx,
			"rpm",
			"-q",
			"--qf",
			"%{VERSION}-%{RELEASE}",
			packageName,
		)
	}
	if err != nil {
		if probeFailureIs(err, probeFailureUnavailable) {
			return "", fmt.Errorf("package version is unavailable: %w", err)
		}
		return "", fmt.Errorf("package version probe failed: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if err := validateVersion("package version", version, true); err != nil {
		return "", err
	}
	return version, nil
}

func compositorPackage(lane Lane) string {
	switch lane {
	case LaneGNOME:
		return "gnome-shell"
	case LaneKDE:
		return "kwin-wayland"
	default:
		return "sway"
	}
}

func portalBackend(lane Lane) (string, string, string) {
	switch lane {
	case LaneGNOME:
		return "gnome", "xdg-desktop-portal-gnome", "org.freedesktop.impl.portal.desktop.gnome"
	case LaneKDE:
		return "kde", "xdg-desktop-portal-kde", "org.freedesktop.impl.portal.desktop.kde"
	default:
		return "wlr", "xdg-desktop-portal-wlr", "org.freedesktop.impl.portal.desktop.wlr"
	}
}

func probePlatform(
	ctx context.Context,
	readFile func(string) ([]byte, error),
	probe commandProbe,
) (Platform, error) {
	osData, err := readFile(defaultOSReleasePath)
	if err != nil {
		return Platform{}, errors.New("operating-system identity check failed")
	}
	osID, osVersion, err := parseOSRelease(osData)
	if err != nil {
		return Platform{}, err
	}
	kernelName, err := probe.run(ctx, "uname", "-s")
	if err != nil {
		return Platform{}, fmt.Errorf("kernel name check failed: %w", err)
	}
	kernelRelease, err := probe.run(ctx, "uname", "-r")
	if err != nil {
		return Platform{}, fmt.Errorf("kernel release check failed: %w", err)
	}
	platform := Platform{
		OSID:          osID,
		OSVersion:     osVersion,
		KernelName:    strings.TrimSpace(string(kernelName)),
		KernelRelease: strings.TrimSpace(string(kernelRelease)),
		Architecture:  runtime.GOARCH,
		GoVersion:     runtime.Version(),
	}
	if !kernelPattern.MatchString(platform.KernelName) ||
		!kernelPattern.MatchString(platform.KernelRelease) {
		return Platform{}, errors.New("kernel identity is not sanitized")
	}
	return platform, nil
}

func parseOSRelease(data []byte) (string, string, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found || (name != "ID" && name != "VERSION_ID") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		values[name] = value
	}
	if err := validateLabel("operating-system ID", values["ID"]); err != nil {
		return "", "", err
	}
	if err := validateVersion("operating-system version", values["VERSION_ID"], true); err != nil {
		return "", "", err
	}
	return values["ID"], values["VERSION_ID"], nil
}

func probeSway(
	ctx context.Context,
	probe commandProbe,
	expectedOutputs int,
	requireHeadless bool,
) error {
	output, err := probe.run(ctx, "swaymsg", "-t", "get_version", "-r")
	var version struct {
		HumanReadable string `json:"human_readable"`
	}
	if err != nil || json.Unmarshal(output, &version) != nil ||
		validateText("Sway version", version.HumanReadable) != nil {
		return errors.New("native Sway capability check failed")
	}
	output, err = probe.run(ctx, "swaymsg", "-t", "get_outputs", "-r")
	var outputs []struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	if err != nil || json.Unmarshal(output, &outputs) != nil {
		return errors.New("native Sway output check failed")
	}
	active := 0
	for _, candidate := range outputs {
		if !candidate.Active {
			continue
		}
		active++
		if requireHeadless && !strings.HasPrefix(candidate.Name, "HEADLESS-") {
			return errors.New("isolated Sway output check failed")
		}
	}
	if active != expectedOutputs {
		return errors.New("native Sway output count does not match the declared topology")
	}
	if !requireHeadless {
		return nil
	}
	output, err = probe.run(ctx, "swaymsg", "-t", "get_inputs", "-r")
	var inputs []json.RawMessage
	if err != nil || json.Unmarshal(output, &inputs) != nil {
		return errors.New("isolated Sway input check failed")
	}
	if len(inputs) != 0 {
		return errors.New("isolated Sway session exposes an input device before the test")
	}
	return nil
}

func probeBusName(ctx context.Context, probe commandProbe, busName string) error {
	output, err := probe.run(
		ctx,
		"busctl",
		"--user",
		"call",
		"org.freedesktop.DBus",
		"/org/freedesktop/DBus",
		"org.freedesktop.DBus",
		"GetNameOwner",
		"s",
		busName,
	)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[0] != "s" ||
		len(fields[1]) < 3 || fields[1][0] != '"' || fields[1][len(fields[1])-1] != '"' {
		return errors.New("D-Bus name owner response is malformed")
	}
	return nil
}

func probePortal(ctx context.Context, probe commandProbe, lane Lane, cell Cell) (Portal, error) {
	backend, backendPackage, backendBusName := portalBackend(lane)
	portal := Portal{Observed: true, Backend: backend}
	frontendVersion, frontendErr := probe.packageVersion(ctx, "xdg-desktop-portal")
	backendVersion, backendErr := probe.packageVersion(ctx, backendPackage)
	remoteVersion, remoteErr := probePortalUint(ctx, probe, remoteDesktopInterface, portalVersionProperty)
	screenVersion, screenErr := probePortalUint(ctx, probe, screenCastInterface, portalVersionProperty)
	sources, sourcesErr := probePortalUint(ctx, probe, screenCastInterface, availableSourcesProperty)
	ownerErr := probeBusName(ctx, probe, portalBusName)
	backendOwnerErr := probeBusName(ctx, probe, backendBusName)
	probeErrors := []error{
		ownerErr,
		backendOwnerErr,
		frontendErr,
		backendErr,
		remoteErr,
		screenErr,
	}
	if cell == CellRemoteDesktop || cell == CellScreenCast {
		probeErrors = append(probeErrors, sourcesErr)
	}
	for _, probeErr := range probeErrors {
		if probeErr != nil && !probeFailureIs(probeErr, probeFailureUnavailable) {
			return Portal{}, errors.New("portal probe infrastructure failed")
		}
	}

	portalAvailable := ownerErr == nil && backendOwnerErr == nil &&
		frontendErr == nil && backendErr == nil && remoteErr == nil && screenErr == nil
	if cell.PortalRequired() && !portalAvailable {
		return Portal{}, errors.New("required portal interface check failed")
	}
	if (cell == CellRemoteDesktop || cell == CellScreenCast) &&
		(sourcesErr != nil || sources == 0) {
		return Portal{}, errors.New("ScreenCast source capability check failed")
	}
	if !portalAvailable {
		return portal, nil
	}
	portal.Available = true
	portal.FrontendVersion = frontendVersion
	portal.BackendVersion = backendVersion
	portal.RemoteDesktopVersion = remoteVersion
	portal.ScreenCastVersion = screenVersion
	if sourcesErr == nil {
		portal.AvailableSources = sources
	}
	return portal, nil
}

func probePortalUint(
	ctx context.Context,
	probe commandProbe,
	interfaceName string,
	property string,
) (uint64, error) {
	output, err := probe.run(
		ctx,
		"busctl",
		"--user",
		"get-property",
		portalBusName,
		portalObjectPath,
		interfaceName,
		property,
	)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[0] != "u" {
		return 0, errors.New("portal property is malformed")
	}
	value, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return 0, errors.New("portal property is malformed")
	}
	if value == 0 {
		return 0, &probeError{failure: probeFailureUnavailable}
	}
	return value, nil
}

func probePipeWire(ctx context.Context, probe commandProbe) (string, error) {
	versionOutput, err := probe.run(ctx, "pkg-config", "--modversion", pipeWirePackage)
	if err != nil {
		return "", errors.New("PipeWire package check failed")
	}
	version := strings.TrimSpace(string(versionOutput))
	if err := validateVersion("PipeWire version", version, true); err != nil {
		return "", err
	}
	for _, service := range []string{"pipewire.service", "wireplumber.service"} {
		output, err := probe.run(ctx, "systemctl", "--user", "is-active", service)
		if err != nil || strings.TrimSpace(string(output)) != "active" {
			return "", errors.New("PipeWire user service check failed")
		}
	}
	return version, nil
}

func validatePreflightReport(report PreflightReport) error {
	if report.SchemaVersion != preflightSchemaVersion {
		return errors.New("invalid preflight schema version")
	}
	if err := validateGitIdentity(report.Commit, report.Ref); err != nil {
		return err
	}
	if err := validateWorkflow(report.Desktop.Cell, report.Workflow); err != nil {
		return err
	}
	if !runIDPattern.MatchString(report.RunID) || report.RunAttempt <= 0 {
		return errors.New("preflight run identity is invalid")
	}
	if err := validateLaneCell(report.Desktop.Lane, report.Desktop.Cell); err != nil {
		return err
	}
	if err := validateLabel("operating-system ID", report.Platform.OSID); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"operating-system version": report.Platform.OSVersion,
		"Go version":               report.Platform.GoVersion,
		"compositor version":       report.Desktop.CompositorVersion,
	} {
		if err := validateVersion(name, value, true); err != nil {
			return err
		}
	}
	if !kernelPattern.MatchString(report.Platform.KernelName) ||
		!kernelPattern.MatchString(report.Platform.KernelRelease) {
		return errors.New("kernel identity is not sanitized")
	}
	if err := validateLabel("architecture", report.Platform.Architecture); err != nil {
		return err
	}
	if err := validateLabel("compositor", report.Desktop.Compositor); err != nil {
		return err
	}
	if report.Desktop.OutputCount <= 0 {
		return errors.New("output count must be positive")
	}
	if report.Desktop.Cell.ConsentRequired() && !report.Desktop.OperatorReady {
		return errors.New("interactive portal cell lacks operator readiness")
	}
	if err := validatePortal(report.Desktop.Portal, report.Desktop.Cell); err != nil {
		return err
	}
	if report.Desktop.PipeWire.Required != report.Desktop.Cell.PipeWireRequired() {
		return errors.New("PipeWire requirement does not match evidence cell")
	}
	return validateVersion(
		"PipeWire version",
		report.Desktop.PipeWire.Version,
		report.Desktop.PipeWire.Required,
	)
}

func validatePortal(portal Portal, cell Cell) error {
	if portal.Observed != cell.PortalObserved() {
		return errors.New("portal observation does not match evidence cell")
	}
	if !portal.Observed {
		return nil
	}
	if err := validateLabel("portal backend", portal.Backend); err != nil {
		return err
	}
	if !portal.Available {
		if cell.PortalRequired() {
			return errors.New("required portal is unavailable")
		}
		if portal.FrontendVersion != "" || portal.BackendVersion != "" ||
			portal.RemoteDesktopVersion != 0 || portal.ScreenCastVersion != 0 ||
			portal.AvailableSources != 0 {
			return errors.New("unavailable portal contains capability values")
		}
		return nil
	}
	if err := validateVersion("portal frontend version", portal.FrontendVersion, true); err != nil {
		return err
	}
	if err := validateVersion("portal backend version", portal.BackendVersion, true); err != nil {
		return err
	}
	if portal.RemoteDesktopVersion == 0 || portal.ScreenCastVersion == 0 {
		return errors.New("available portal lacks interface versions")
	}
	if cell == CellScreenCast && portal.AvailableSources == 0 {
		return errors.New("ScreenCast cell lacks available source types")
	}
	return nil
}
