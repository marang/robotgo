package apicompat

import (
	"bufio"
	"fmt"
	"strings"
)

const (
	manifestHeader       = "# RobotGo public API manifest v1"
	maxManifestLineBytes = 4 * 1024 * 1024
)

// PackageAPI is the canonical exported API for one import path.
type PackageAPI struct {
	Path         string
	Declarations []string
}

// Manifest is the canonical public API for all discovered library packages.
type Manifest struct {
	Packages []PackageAPI
}

// Render returns the deterministic, human-reviewable baseline representation.
func (manifest Manifest) Render() string {
	var body strings.Builder
	body.WriteString(manifestHeader)
	body.WriteString("\n")
	for _, pkg := range manifest.Packages {
		body.WriteString("\npackage ")
		body.WriteString(pkg.Path)
		body.WriteString("\n")
		for _, declaration := range pkg.Declarations {
			body.WriteString(declaration)
			body.WriteString("\n")
		}
	}
	return body.String()
}

// ParseManifest validates and parses a checked-in baseline.
func ParseManifest(body string) (Manifest, error) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), maxManifestLineBytes)
	if !scanner.Scan() || scanner.Text() != manifestHeader {
		return Manifest{}, fmt.Errorf("invalid API manifest header")
	}

	var manifest Manifest
	var current *PackageAPI
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "package "))
			if path == "" {
				return Manifest{}, fmt.Errorf("line %d: empty package path", lineNumber)
			}
			if len(manifest.Packages) > 0 &&
				path <= manifest.Packages[len(manifest.Packages)-1].Path {
				return Manifest{}, fmt.Errorf(
					"line %d: package paths are not strictly sorted",
					lineNumber,
				)
			}
			manifest.Packages = append(manifest.Packages, PackageAPI{Path: path})
			current = &manifest.Packages[len(manifest.Packages)-1]
			continue
		}
		if current == nil {
			return Manifest{}, fmt.Errorf(
				"line %d: declaration appears before a package",
				lineNumber,
			)
		}
		if len(current.Declarations) > 0 &&
			line <= current.Declarations[len(current.Declarations)-1] {
			return Manifest{}, fmt.Errorf(
				"line %d: declarations are not strictly sorted",
				lineNumber,
			)
		}
		current.Declarations = append(current.Declarations, line)
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, fmt.Errorf("scan API manifest: %w", err)
	}
	if len(manifest.Packages) == 0 {
		return Manifest{}, fmt.Errorf("API manifest contains no packages")
	}
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if len(manifest.Packages) == 0 {
		return fmt.Errorf("manifest contains no packages")
	}
	for packageIndex, pkg := range manifest.Packages {
		if pkg.Path == "" {
			return fmt.Errorf("package %d has an empty path", packageIndex)
		}
		if packageIndex > 0 && pkg.Path <= manifest.Packages[packageIndex-1].Path {
			return fmt.Errorf("package paths are not strictly sorted")
		}
		for declarationIndex, declaration := range pkg.Declarations {
			if declaration == "" {
				return fmt.Errorf("package %s has an empty declaration", pkg.Path)
			}
			if declarationIndex > 0 &&
				declaration <= pkg.Declarations[declarationIndex-1] {
				return fmt.Errorf(
					"package %s declarations are not strictly sorted",
					pkg.Path,
				)
			}
		}
	}
	return nil
}
