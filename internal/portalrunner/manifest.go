// Package portalrunner validates protected ephemeral portal-runner definitions
// and owns their host-side temporary state.
package portalrunner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// ManifestSchemaVersion identifies the protected runner definition format.
	ManifestSchemaVersion = "1"
	// HostedGuestEnvKey gates commands that may reconfigure the disposable
	// hosted guest desktop.
	HostedGuestEnvKey = "ROBOTGO_HOSTED_GUEST"
	// HostedExpectedOutputsEnvKey carries the encoded logical topology for
	// consent-free hosted display-bounds evidence.
	HostedExpectedOutputsEnvKey = "ROBOTGO_HOSTED_EXPECTED_OUTPUTS"
	// HostedBoundsVariantEnvKey identifies which public output implementation
	// the hosted bounds contract is exercising.
	HostedBoundsVariantEnvKey = "ROBOTGO_HOSTED_BOUNDS_VARIANT"
	// HostedBoundsVariantNativeCGO identifies the native-CGO Wayland client.
	HostedBoundsVariantNativeCGO = "native-cgo"
	// HostedBoundsVariantPureGo identifies the Pure-Go Wayland client.
	HostedBoundsVariantPureGo = "pure-go"
	// PortalMultiOutputEnvKey enables the hosted multi-output portal contract.
	PortalMultiOutputEnvKey = "ROBOTGO_PORTAL_MULTI_OUTPUT"
	// PortalExpectedOutputsEnvKey carries the encoded hosted output topology.
	PortalExpectedOutputsEnvKey = "ROBOTGO_PORTAL_EXPECTED_OUTPUTS"

	portalLaneGNOME = "gnome"
	portalLaneKDE   = "kde"

	maxManifestBytes = 64 * 1024
	minimumCPUs      = 2
	maximumCPUs      = 32
	minimumMemoryMiB = 4096
	maximumMemoryMiB = 64 * 1024
	minimumDiskGiB   = 20
	maximumDiskGiB   = 256
	minimumLifetime  = 10 * time.Minute
	maximumLifetime  = time.Hour

	minimumHostedOutputSize = 640
	maximumHostedOutputSize = 8192
	maximumHostedDesktop    = 8192
)

var (
	repositoryPattern = regexp.MustCompile(
		`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,99})/[A-Za-z0-9](?:[A-Za-z0-9._-]{0,99})$`,
	)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	kernelPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+-[0-9]+-generic$`)
	packagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,127}$`)
	hostPattern    = regexp.MustCompile(
		`^(?:\*\.)?[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
	)
)

// Manifest is the complete non-secret definition of one protected runner
// image and its runtime boundary.
type Manifest struct {
	SchemaVersion string        `json:"schema_version"`
	Lane          string        `json:"lane"`
	Repository    string        `json:"repository"`
	Labels        []string      `json:"labels"`
	BaseImage     Artifact      `json:"base_image"`
	APTSnapshot   string        `json:"apt_snapshot"`
	Go            Artifact      `json:"go"`
	ActionsRunner Artifact      `json:"actions_runner"`
	VM            VMConfig      `json:"vm"`
	HostedDisplay HostedDisplay `json:"hosted_display"`
	Packages      []string      `json:"packages"`
	Network       NetworkConfig `json:"network"`
}

// Artifact identifies immutable downloaded image content.
type Artifact struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// VMConfig contains bounded disposable-guest resources.
type VMConfig struct {
	CPUs            int    `json:"cpus"`
	MemoryMiB       int    `json:"memory_mib"`
	DiskGiB         int    `json:"disk_gib"`
	KernelRelease   string `json:"kernel_release"`
	MaximumLifetime string `json:"maximum_lifetime"`
}

// HostedDisplay defines the exact logical monitor topology used by explicit
// multi-output runs. Outputs are ordered by virtual scanout; the first output
// is the primary display used for consent interaction.
type HostedDisplay struct {
	Outputs []HostedOutput `json:"outputs"`
}

// HostedOutput is one logical monitor rectangle in compositor coordinates.
type HostedOutput struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

// NetworkConfig is the sole guest egress path during untrusted job execution.
type NetworkConfig struct {
	ProxyPort    int      `json:"proxy_port"`
	AllowedHosts []string `json:"allowed_hosts"`
}

// LoadManifest decodes and validates a bounded manifest without accepting
// unknown fields or trailing JSON values.
func LoadManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open portal runner manifest: %w", err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("read portal runner manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("portal runner manifest exceeds size limit")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode portal runner manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("portal runner manifest has trailing JSON value")
		}
		return Manifest{}, fmt.Errorf("decode portal runner manifest trailer: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate verifies the immutable image, guest, and egress contract.
func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported portal runner manifest schema %q", manifest.SchemaVersion)
	}
	if manifest.Lane != portalLaneGNOME && manifest.Lane != portalLaneKDE {
		return fmt.Errorf("unsupported protected runner lane %q", manifest.Lane)
	}
	if !repositoryPattern.MatchString(manifest.Repository) {
		return errors.New("portal runner repository is invalid")
	}
	if !slices.Equal(
		manifest.Labels,
		[]string{"self-hosted", "linux", "wayland", manifest.Lane},
	) {
		return errors.New("portal runner labels must be the exact protected lane label set")
	}

	if err := validateArtifact(
		"base image",
		manifest.BaseImage,
		"cloud-images.ubuntu.com",
	); err != nil {
		return err
	}
	if err := validateArtifact("Go", manifest.Go, "go.dev"); err != nil {
		return err
	}
	if err := validateArtifact("Actions runner", manifest.ActionsRunner, "github.com"); err != nil {
		return err
	}
	if err := validateHTTPSURL(
		"APT snapshot",
		manifest.APTSnapshot,
		"snapshot.ubuntu.com",
	); err != nil {
		return err
	}
	if !strings.HasSuffix(manifest.APTSnapshot, "/") ||
		!strings.Contains(manifest.APTSnapshot, "/ubuntu/") {
		return errors.New("APT snapshot must identify an immutable Ubuntu snapshot root")
	}

	if manifest.VM.CPUs < minimumCPUs || manifest.VM.CPUs > maximumCPUs {
		return fmt.Errorf("portal runner CPU count must be between %d and %d", minimumCPUs, maximumCPUs)
	}
	if manifest.VM.MemoryMiB < minimumMemoryMiB ||
		manifest.VM.MemoryMiB > maximumMemoryMiB {
		return fmt.Errorf(
			"portal runner memory must be between %d and %d MiB",
			minimumMemoryMiB,
			maximumMemoryMiB,
		)
	}
	if manifest.VM.DiskGiB < minimumDiskGiB || manifest.VM.DiskGiB > maximumDiskGiB {
		return fmt.Errorf(
			"portal runner disk must be between %d and %d GiB",
			minimumDiskGiB,
			maximumDiskGiB,
		)
	}
	if !kernelPattern.MatchString(manifest.VM.KernelRelease) {
		return errors.New("portal runner kernel release is invalid")
	}
	lifetime, err := time.ParseDuration(manifest.VM.MaximumLifetime)
	if err != nil || lifetime < minimumLifetime || lifetime > maximumLifetime {
		return fmt.Errorf(
			"portal runner maximum lifetime must be between %s and %s",
			minimumLifetime,
			maximumLifetime,
		)
	}
	if err := manifest.HostedDisplay.Validate(); err != nil {
		return err
	}

	if len(manifest.Packages) == 0 {
		return errors.New("portal runner package set is empty")
	}
	if !slices.IsSorted(manifest.Packages) {
		return errors.New("portal runner package set must be sorted")
	}
	for index, packageName := range manifest.Packages {
		if !packagePattern.MatchString(packageName) {
			return fmt.Errorf("portal runner package %q is invalid", packageName)
		}
		if index > 0 && packageName == manifest.Packages[index-1] {
			return fmt.Errorf("portal runner package %q is duplicated", packageName)
		}
	}
	requiredPackages := []string{
		"libpipewire-0.3-dev",
		"linux-modules-extra-" + manifest.VM.KernelRelease,
		"pipewire",
		"wireplumber",
		"xdg-desktop-portal",
	}
	switch manifest.Lane {
	case portalLaneGNOME:
		requiredPackages = append(requiredPackages,
			"gdm3",
			"gnome-shell",
			"libpam-gnome-keyring",
			"xdg-desktop-portal-gnome",
		)
	case portalLaneKDE:
		requiredPackages = append(requiredPackages,
			"kwin-wayland",
			"libkf5screen-bin",
			"plasma-desktop",
			"plasma-workspace-wayland",
			"sddm",
			"xdg-desktop-portal-kde",
		)
	}
	for _, required := range requiredPackages {
		if !slices.Contains(manifest.Packages, required) {
			return fmt.Errorf("portal runner package set omits %q", required)
		}
	}

	if manifest.Network.ProxyPort < 1024 || manifest.Network.ProxyPort > 65535 {
		return errors.New("portal runner proxy port must be an unprivileged TCP port")
	}
	if len(manifest.Network.AllowedHosts) == 0 ||
		!slices.IsSorted(manifest.Network.AllowedHosts) {
		return errors.New("portal runner allowed hosts must be a sorted non-empty set")
	}
	for index, host := range manifest.Network.AllowedHosts {
		plainHost := strings.TrimPrefix(host, "*.")
		if host != strings.ToLower(host) ||
			!hostPattern.MatchString(host) ||
			net.ParseIP(plainHost) != nil {
			return fmt.Errorf("portal runner allowed host %q is invalid", host)
		}
		if index > 0 && host == manifest.Network.AllowedHosts[index-1] {
			return fmt.Errorf("portal runner allowed host %q is duplicated", host)
		}
	}
	for _, required := range []string{"api.github.com", "github.com"} {
		if !slices.Contains(manifest.Network.AllowedHosts, required) {
			return fmt.Errorf("portal runner allowed hosts omit %q", required)
		}
	}
	return nil
}

// Validate rejects ambiguous, overlapping, or unbounded hosted topologies.
func (display HostedDisplay) Validate() error {
	if len(display.Outputs) != 2 {
		return errors.New("hosted display must define exactly two outputs")
	}
	primary := display.Outputs[0]
	if primary.X != 0 || primary.Y != 0 {
		return errors.New("hosted display primary output must start at origin")
	}
	if err := validateHostedOutput(primary); err != nil {
		return fmt.Errorf("hosted display output 0: %w", err)
	}
	secondary := display.Outputs[1]
	if err := validateHostedOutput(secondary); err != nil {
		return fmt.Errorf("hosted display output 1: %w", err)
	}
	if primary.Width == secondary.Width && primary.Height == secondary.Height {
		return errors.New("hosted display outputs must have distinct sizes")
	}
	if secondary.X == 0 && secondary.Y == 0 {
		return errors.New("hosted display secondary output must have a non-zero origin")
	}
	if hostedOutputsOverlap(primary, secondary) {
		return errors.New("hosted display outputs must not overlap")
	}
	minX := min(primary.X, secondary.X)
	minY := min(primary.Y, secondary.Y)
	maxX := max(
		int64(primary.X)+int64(primary.Width),
		int64(secondary.X)+int64(secondary.Width),
	)
	maxY := max(
		int64(primary.Y)+int64(primary.Height),
		int64(secondary.Y)+int64(secondary.Height),
	)
	if maxX-int64(minX) > maximumHostedDesktop ||
		maxY-int64(minY) > maximumHostedDesktop {
		return errors.New("hosted display aggregate bounds exceed limit")
	}
	return nil
}

func validateHostedOutput(output HostedOutput) error {
	if output.Width < minimumHostedOutputSize ||
		output.Width > maximumHostedOutputSize ||
		output.Height < minimumHostedOutputSize ||
		output.Height > maximumHostedOutputSize {
		return fmt.Errorf(
			"size must be between %d and %d pixels per axis",
			minimumHostedOutputSize,
			maximumHostedOutputSize,
		)
	}
	if output.X < -maximumHostedDesktop ||
		output.X > maximumHostedDesktop ||
		output.Y < -maximumHostedDesktop ||
		output.Y > maximumHostedDesktop {
		return errors.New("origin exceeds hosted desktop limit")
	}
	return nil
}

func hostedOutputsOverlap(first, second HostedOutput) bool {
	firstRight := int64(first.X) + int64(first.Width)
	firstBottom := int64(first.Y) + int64(first.Height)
	secondRight := int64(second.X) + int64(second.Width)
	secondBottom := int64(second.Y) + int64(second.Height)
	return int64(first.X) < secondRight &&
		int64(second.X) < firstRight &&
		int64(first.Y) < secondBottom &&
		int64(second.Y) < firstBottom
}

// Encode returns the canonical, non-sensitive environment representation of
// the exact hosted logical topology.
func (display HostedDisplay) Encode() (string, error) {
	if err := display.Validate(); err != nil {
		return "", err
	}
	encoded := make([]string, 0, len(display.Outputs))
	for _, output := range display.Outputs {
		encoded = append(encoded, strings.Join([]string{
			strconv.Itoa(output.X),
			strconv.Itoa(output.Y),
			strconv.Itoa(output.Width),
			strconv.Itoa(output.Height),
		}, ","))
	}
	return strings.Join(encoded, ";"), nil
}

// ParseHostedDisplay decodes only the canonical representation emitted by
// Encode and re-applies every topology safety bound.
func ParseHostedDisplay(encoded string) (HostedDisplay, error) {
	rawOutputs := strings.Split(encoded, ";")
	outputs := make([]HostedOutput, 0, len(rawOutputs))
	for _, rawOutput := range rawOutputs {
		fields := strings.Split(rawOutput, ",")
		if len(fields) != 4 {
			return HostedDisplay{}, errors.New(
				"hosted display encoding is invalid",
			)
		}
		values := make([]int, len(fields))
		for index, field := range fields {
			value, err := strconv.Atoi(field)
			if err != nil {
				return HostedDisplay{}, errors.New(
					"hosted display encoding is invalid",
				)
			}
			values[index] = value
		}
		outputs = append(outputs, HostedOutput{
			X: values[0], Y: values[1],
			Width: values[2], Height: values[3],
		})
	}
	display := HostedDisplay{Outputs: outputs}
	canonical, err := display.Encode()
	if err != nil {
		return HostedDisplay{}, err
	}
	if canonical != encoded {
		return HostedDisplay{}, errors.New(
			"hosted display encoding is not canonical",
		)
	}
	return display, nil
}

func validateArtifact(name string, artifact Artifact, expectedHost string) error {
	if !versionPattern.MatchString(artifact.Version) {
		return fmt.Errorf("%s version is invalid", name)
	}
	if err := validateHTTPSURL(name, artifact.URL, expectedHost); err != nil {
		return err
	}
	if len(artifact.SHA256) != sha256.Size*2 {
		return fmt.Errorf("%s SHA-256 digest is invalid", name)
	}
	digest, err := hex.DecodeString(artifact.SHA256)
	if err != nil || hex.EncodeToString(digest) != artifact.SHA256 {
		return fmt.Errorf("%s SHA-256 digest is invalid", name)
	}
	return nil
}

func validateHTTPSURL(name, value, expectedHost string) error {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Hostname() != expectedHost ||
		parsed.Port() != "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Path == "" ||
		parsed.Path == "/" {
		return fmt.Errorf("%s URL is not a pinned HTTPS source", name)
	}
	return nil
}

// HostAllowed reports whether a CONNECT destination is covered by the exact
// or wildcard-suffix egress set. IP literals and port-bearing hosts are denied.
func (network NetworkConfig) HostAllowed(value string) bool {
	host := strings.ToLower(strings.TrimSuffix(value, "."))
	if parsedHost, port, err := net.SplitHostPort(host); err == nil {
		if port != "443" {
			return false
		}
		host = parsedHost
	} else if strings.Contains(host, ":") {
		return false
	}
	if net.ParseIP(host) != nil || !hostPattern.MatchString(host) {
		return false
	}
	for _, allowed := range network.AllowedHosts {
		if strings.HasPrefix(allowed, "*.") {
			suffix := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(host, suffix) &&
				len(host) > len(suffix) {
				return true
			}
			continue
		}
		if host == allowed {
			return true
		}
	}
	return false
}

// MaximumLifetime returns the validated guest lifetime.
func (manifest Manifest) MaximumLifetime() time.Duration {
	lifetime, _ := time.ParseDuration(manifest.VM.MaximumLifetime)
	return lifetime
}

// ProxyAddress returns the fixed guest-visible proxy endpoint.
func (manifest Manifest) ProxyAddress() string {
	return net.JoinHostPort("10.0.2.2", strconv.Itoa(manifest.Network.ProxyPort))
}
