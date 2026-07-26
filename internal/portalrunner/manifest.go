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

	maxManifestBytes = 64 * 1024
	minimumCPUs      = 2
	maximumCPUs      = 32
	minimumMemoryMiB = 4096
	maximumMemoryMiB = 64 * 1024
	minimumDiskGiB   = 20
	maximumDiskGiB   = 256
	minimumLifetime  = 10 * time.Minute
	maximumLifetime  = time.Hour
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
	if manifest.Lane != "gnome" {
		return fmt.Errorf("unsupported protected runner lane %q", manifest.Lane)
	}
	if !repositoryPattern.MatchString(manifest.Repository) {
		return errors.New("portal runner repository is invalid")
	}
	if !slices.Equal(
		manifest.Labels,
		[]string{"self-hosted", "linux", "wayland", manifest.Lane},
	) {
		return errors.New("portal runner labels must be the exact protected GNOME label set")
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
	for _, required := range []string{
		"gdm3",
		"gnome-shell",
		"libpam-gnome-keyring",
		"libpipewire-0.3-dev",
		"linux-modules-extra-" + manifest.VM.KernelRelease,
		"pipewire",
		"wireplumber",
		"xdg-desktop-portal",
		"xdg-desktop-portal-gnome",
	} {
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
