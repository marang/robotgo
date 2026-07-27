package apicompat

import (
	"context"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const packageLoadPattern = "./..."

// Snapshot loads and renders the public library API for one build variant.
func Snapshot(
	ctx context.Context,
	root string,
	cfg Config,
	variant Variant,
) (Manifest, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve repository root: %w", err)
	}

	buildFlags := []string{"-mod=readonly"}
	if len(variant.Tags) != 0 {
		buildFlags = append(buildFlags, "-tags="+strings.Join(variant.Tags, ","))
	}
	loadConfig := &packages.Config{
		Context: ctx,
		Dir:     absoluteRoot,
		Env:     variantEnvironment(variant),
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedModule,
		BuildFlags: buildFlags,
		Tests:      false,
	}

	loaded, err := packages.Load(loadConfig, packageLoadPattern)
	if err != nil {
		return Manifest{}, fmt.Errorf("load %s API packages: %w", variant.Name, err)
	}

	publicPackages := make([]*types.Package, 0, len(loaded))
	seen := make(map[string]struct{}, len(loaded))
	for _, pkg := range loaded {
		if !isPublicLibraryPackage(cfg, pkg) {
			continue
		}
		if len(pkg.Errors) != 0 {
			return Manifest{}, fmt.Errorf(
				"load public package %s for %s: %s",
				pkg.PkgPath,
				variant.Name,
				pkg.Errors[0],
			)
		}
		if pkg.Types == nil {
			return Manifest{}, fmt.Errorf(
				"public package %s has no type information for %s",
				pkg.PkgPath,
				variant.Name,
			)
		}
		if _, duplicate := seen[pkg.PkgPath]; duplicate {
			return Manifest{}, fmt.Errorf(
				"public package %s loaded more than once for %s",
				pkg.PkgPath,
				variant.Name,
			)
		}
		seen[pkg.PkgPath] = struct{}{}
		publicPackages = append(publicPackages, pkg.Types)
	}
	if len(publicPackages) == 0 {
		return Manifest{}, fmt.Errorf("no public packages discovered for %s", variant.Name)
	}

	slices.SortFunc(publicPackages, func(left, right *types.Package) int {
		return strings.Compare(left.Path(), right.Path())
	})

	manifest := Manifest{Packages: make([]PackageAPI, 0, len(publicPackages))}
	for _, pkg := range publicPackages {
		manifest.Packages = append(manifest.Packages, renderPackageAPI(pkg))
	}
	return manifest, nil
}

func variantEnvironment(variant Variant) []string {
	cgoEnabled := "0"
	if variant.CGOEnabled {
		cgoEnabled = "1"
	}
	overrides := map[string]string{
		"CGO_ENABLED":  cgoEnabled,
		"GOARCH":       variant.GOARCH,
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFLAGS":      "",
		"GOOS":         variant.GOOS,
		"GOWORK":       "off",
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if _, overridden := overrides[key]; found && overridden {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	slices.Sort(environment)
	return environment
}

func isPublicLibraryPackage(cfg Config, pkg *packages.Package) bool {
	if pkg == nil || pkg.Module == nil || pkg.Module.Path != cfg.Module {
		return false
	}
	if pkg.PkgPath != cfg.Module && !strings.HasPrefix(pkg.PkgPath, cfg.Module+"/") {
		return false
	}
	if pkg.Name == "main" || len(pkg.CompiledGoFiles) == 0 {
		return false
	}
	if containsPathSegment(pkg.PkgPath, "internal") {
		return false
	}
	for _, prefix := range cfg.ExcludedPackagePrefixes {
		prefix = strings.TrimSuffix(prefix, "/")
		if pkg.PkgPath == prefix || strings.HasPrefix(pkg.PkgPath, prefix+"/") {
			return false
		}
	}
	return true
}

func containsPathSegment(path, segment string) bool {
	for _, candidate := range strings.Split(path, "/") {
		if candidate == segment {
			return true
		}
	}
	return false
}
