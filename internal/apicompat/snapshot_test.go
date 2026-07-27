package apicompat

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestIsPublicLibraryPackage(t *testing.T) {
	t.Parallel()

	const module = "example.test/module"
	cfg := Config{
		Module: module,
		ExcludedPackagePrefixes: []string{
			module + "/examples",
		},
	}
	moduleInfo := &packages.Module{Path: module}

	tests := []struct {
		name string
		pkg  *packages.Package
		want bool
	}{
		{
			name: "root",
			pkg: &packages.Package{
				PkgPath:         module,
				Name:            "module",
				CompiledGoFiles: []string{"api.go"},
				Module:          moduleInfo,
			},
			want: true,
		},
		{
			name: "new public package",
			pkg: &packages.Package{
				PkgPath:         module + "/newpkg",
				Name:            "newpkg",
				CompiledGoFiles: []string{"api.go"},
				Module:          moduleInfo,
			},
			want: true,
		},
		{
			name: "internal",
			pkg: &packages.Package{
				PkgPath:         module + "/agent/internal/private",
				Name:            "private",
				CompiledGoFiles: []string{"private.go"},
				Module:          moduleInfo,
			},
		},
		{
			name: "command",
			pkg: &packages.Package{
				PkgPath:         module + "/cmd/tool",
				Name:            "main",
				CompiledGoFiles: []string{"main.go"},
				Module:          moduleInfo,
			},
		},
		{
			name: "excluded tree",
			pkg: &packages.Package{
				PkgPath:         module + "/examples/helper",
				Name:            "helper",
				CompiledGoFiles: []string{"helper.go"},
				Module:          moduleInfo,
			},
		},
		{
			name: "test only",
			pkg: &packages.Package{
				PkgPath: module + "/scripts",
				Name:    "scripts",
				Module:  moduleInfo,
			},
		},
		{
			name: "dependency",
			pkg: &packages.Package{
				PkgPath:         "example.test/dependency",
				Name:            "dependency",
				CompiledGoFiles: []string{"api.go"},
				Module:          &packages.Module{Path: "example.test/dependency"},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isPublicLibraryPackage(cfg, test.pkg); got != test.want {
				t.Fatalf("isPublicLibraryPackage() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestVariantEnvironmentOverridesPersistentGoConfiguration(t *testing.T) {
	for key, value := range map[string]string{
		"GOENV":        "/tmp/untrusted-goenv",
		"GOEXPERIMENT": "fieldtrack",
		"GOFLAGS":      "-tags=untrusted",
		"GOWORK":       "/tmp/untrusted.work",
	} {
		t.Setenv(key, value)
	}

	environment := variantEnvironment(Variant{
		GOOS:       "linux",
		GOARCH:     "amd64",
		CGOEnabled: true,
	})
	got := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			got[key] = value
		}
	}
	for key, want := range map[string]string{
		"CGO_ENABLED":  "1",
		"GOARCH":       "amd64",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFLAGS":      "",
		"GOOS":         "linux",
		"GOWORK":       "off",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
	if _, exists := got["PATH"]; !exists && os.Getenv("PATH") != "" {
		t.Error("unrelated environment was not preserved")
	}
}
