package portalrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Lane:          "gnome",
		Repository:    "marang/robotgo",
		Labels:        []string{"self-hosted", "linux", "wayland", "gnome"},
		BaseImage: Artifact{
			Version: "ubuntu-24.04-20260705",
			URL:     "https://cloud-images.ubuntu.com/noble/20260705/image.img",
			SHA256:  strings.Repeat("a", 64),
		},
		APTSnapshot: "https://snapshot.ubuntu.com/ubuntu/20260705T000000Z/",
		Go: Artifact{
			Version: "go1.25.12",
			URL:     "https://go.dev/dl/go1.25.12.linux-amd64.tar.gz",
			SHA256:  strings.Repeat("b", 64),
		},
		ActionsRunner: Artifact{
			Version: "2.336.0",
			URL:     "https://github.com/actions/runner/releases/download/v2.336.0/runner.tar.gz",
			SHA256:  strings.Repeat("c", 64),
		},
		VM: VMConfig{
			CPUs:            4,
			MemoryMiB:       8192,
			DiskGiB:         40,
			KernelRelease:   "6.8.0-134-generic",
			MaximumLifetime: "30m",
		},
		Packages: []string{
			"gdm3",
			"gnome-shell",
			"libpam-gnome-keyring",
			"libpipewire-0.3-dev",
			"linux-modules-extra-6.8.0-134-generic",
			"pipewire",
			"wireplumber",
			"xdg-desktop-portal",
			"xdg-desktop-portal-gnome",
		},
		Network: NetworkConfig{
			ProxyPort: 3128,
			AllowedHosts: []string{
				"*.actions.githubusercontent.com",
				"api.github.com",
				"github.com",
			},
		},
	}
}

func TestRepositoryGNOMEManifest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(
		"..",
		"..",
		"infrastructure",
		"portal-runner",
		"gnome",
		"manifest.json",
	)
	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.MaximumLifetime() != 30*time.Minute {
		t.Fatalf("maximum lifetime = %s", manifest.MaximumLifetime())
	}
	if manifest.ProxyAddress() != "10.0.2.2:3128" {
		t.Fatalf("proxy address = %q", manifest.ProxyAddress())
	}
}

func TestManifestRejectsUnsafeContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Manifest)
		want   string
	}{
		{
			name: "wrong lane",
			change: func(manifest *Manifest) {
				manifest.Lane = "kde"
			},
			want: "unsupported protected runner lane",
		},
		{
			name: "extra label",
			change: func(manifest *Manifest) {
				manifest.Labels = append(manifest.Labels, "personal-desktop")
			},
			want: "exact protected GNOME label set",
		},
		{
			name: "mutable image host",
			change: func(manifest *Manifest) {
				manifest.BaseImage.URL = "https://example.com/image.img"
			},
			want: "not a pinned HTTPS source",
		},
		{
			name: "weak digest",
			change: func(manifest *Manifest) {
				manifest.ActionsRunner.SHA256 = "latest"
			},
			want: "SHA-256 digest is invalid",
		},
		{
			name: "small guest",
			change: func(manifest *Manifest) {
				manifest.VM.MemoryMiB = 1024
			},
			want: "memory must be between",
		},
		{
			name: "kernel package mismatch",
			change: func(manifest *Manifest) {
				manifest.VM.KernelRelease = "6.8.0-135-generic"
			},
			want: `package set omits "linux-modules-extra-6.8.0-135-generic"`,
		},
		{
			name: "unbounded lifetime",
			change: func(manifest *Manifest) {
				manifest.VM.MaximumLifetime = "24h"
			},
			want: "maximum lifetime must be between",
		},
		{
			name: "unsorted packages",
			change: func(manifest *Manifest) {
				manifest.Packages[0], manifest.Packages[1] =
					manifest.Packages[1], manifest.Packages[0]
			},
			want: "package set must be sorted",
		},
		{
			name: "missing portal backend",
			change: func(manifest *Manifest) {
				manifest.Packages = manifest.Packages[:len(manifest.Packages)-1]
			},
			want: `package set omits "xdg-desktop-portal-gnome"`,
		},
		{
			name: "unsafe proxy port",
			change: func(manifest *Manifest) {
				manifest.Network.ProxyPort = 80
			},
			want: "unprivileged TCP port",
		},
		{
			name: "IP egress",
			change: func(manifest *Manifest) {
				manifest.Network.AllowedHosts[0] = "192.0.2.1"
			},
			want: "allowed host",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest()
			test.change(&manifest)
			if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLoadManifestRejectsUnknownAndTrailingFields(t *testing.T) {
	t.Parallel()
	manifest := validManifest()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "unknown",
			data: strings.TrimSuffix(string(data), "}") + `,"registration_token":"secret"}`,
			want: "unknown field",
		},
		{
			name: "trailing",
			data: string(data) + `{}`,
			want: "trailing JSON value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(path); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadManifest() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestNetworkHostAllowed(t *testing.T) {
	t.Parallel()
	network := validManifest().Network
	tests := []struct {
		host string
		want bool
	}{
		{host: "github.com", want: true},
		{host: "github.com:443", want: true},
		{host: "results-receiver.actions.githubusercontent.com", want: true},
		{host: "actions.githubusercontent.com", want: false},
		{host: "github.com:8443", want: false},
		{host: "github.com.example.org", want: false},
		{host: "127.0.0.1", want: false},
		{host: "[::1]:443", want: false},
	}
	for _, test := range tests {
		if got := network.HostAllowed(test.host); got != test.want {
			t.Errorf("HostAllowed(%q) = %v, want %v", test.host, got, test.want)
		}
	}
}
