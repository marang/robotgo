package apicompat

import (
	"bufio"
	"fmt"
	"slices"
	"strings"
)

const deltaHeader = "# RobotGo public API delta v1"

// Delta describes the exact removals and additions relative to a full
// baseline.
type Delta struct {
	Base    string
	Removed []string
	Added   []string
}

// ManifestDelta computes a deterministic overlay from base to current.
func ManifestDelta(base, current Manifest, baseName string) (Delta, error) {
	if err := validateManifest(base); err != nil {
		return Delta{}, fmt.Errorf("invalid delta base: %w", err)
	}
	if err := validateManifest(current); err != nil {
		return Delta{}, fmt.Errorf("invalid delta target: %w", err)
	}

	baseLines := manifestAPISet(base)
	currentLines := manifestAPISet(current)
	delta := Delta{Base: baseName}
	for line := range baseLines {
		if _, exists := currentLines[line]; !exists {
			delta.Removed = append(delta.Removed, line)
		}
	}
	for line := range currentLines {
		if _, exists := baseLines[line]; !exists {
			delta.Added = append(delta.Added, line)
		}
	}
	slices.Sort(delta.Removed)
	slices.Sort(delta.Added)
	return delta, nil
}

// ApplyDelta reconstructs a variant manifest and rejects stale overlays.
func ApplyDelta(base Manifest, delta Delta) (Manifest, error) {
	if err := validateManifest(base); err != nil {
		return Manifest{}, fmt.Errorf("invalid delta base: %w", err)
	}

	lines := manifestAPISet(base)
	for _, line := range delta.Removed {
		if _, exists := lines[line]; !exists {
			return Manifest{}, fmt.Errorf("delta removes missing API %q", line)
		}
		delete(lines, line)
	}
	for _, line := range delta.Added {
		if _, exists := lines[line]; exists {
			return Manifest{}, fmt.Errorf("delta adds existing API %q", line)
		}
		lines[line] = struct{}{}
	}
	return manifestFromAPISet(lines)
}

// Render returns the deterministic, human-reviewable delta representation.
func (delta Delta) Render() string {
	var body strings.Builder
	body.WriteString(deltaHeader)
	body.WriteString("\nbase ")
	body.WriteString(delta.Base)
	body.WriteString("\n")
	for _, line := range delta.Removed {
		body.WriteString("- ")
		body.WriteString(line)
		body.WriteString("\n")
	}
	for _, line := range delta.Added {
		body.WriteString("+ ")
		body.WriteString(line)
		body.WriteString("\n")
	}
	return body.String()
}

// ParseDelta validates and parses a checked-in baseline overlay.
func ParseDelta(body string) (Delta, error) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), maxManifestLineBytes)
	if !scanner.Scan() || scanner.Text() != deltaHeader {
		return Delta{}, fmt.Errorf("invalid API delta header")
	}
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "base ") {
		return Delta{}, fmt.Errorf("API delta omits its base")
	}
	delta := Delta{Base: strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "base "))}
	if delta.Base == "" {
		return Delta{}, fmt.Errorf("API delta has an empty base")
	}

	lineNumber := 2
	seenAdded := false
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "- "):
			if seenAdded {
				return Delta{}, fmt.Errorf(
					"line %d: removal follows an addition",
					lineNumber,
				)
			}
			delta.Removed = append(delta.Removed, strings.TrimPrefix(line, "- "))
		case strings.HasPrefix(line, "+ "):
			seenAdded = true
			delta.Added = append(delta.Added, strings.TrimPrefix(line, "+ "))
		default:
			return Delta{}, fmt.Errorf("line %d: invalid API delta line", lineNumber)
		}
	}
	if err := scanner.Err(); err != nil {
		return Delta{}, fmt.Errorf("scan API delta: %w", err)
	}
	if !slices.IsSorted(delta.Removed) || !slices.IsSorted(delta.Added) {
		return Delta{}, fmt.Errorf("API delta entries are not sorted")
	}
	if slices.Contains(delta.Removed, "") || slices.Contains(delta.Added, "") {
		return Delta{}, fmt.Errorf("API delta contains an empty entry")
	}
	if hasAdjacentDuplicate(delta.Removed) || hasAdjacentDuplicate(delta.Added) {
		return Delta{}, fmt.Errorf("API delta contains a duplicate entry")
	}
	removed := make(map[string]struct{}, len(delta.Removed))
	for _, line := range delta.Removed {
		removed[line] = struct{}{}
	}
	for _, line := range delta.Added {
		if _, exists := removed[line]; exists {
			return Delta{}, fmt.Errorf("API delta both removes and adds %q", line)
		}
	}
	return delta, nil
}

func manifestFromAPISet(lines map[string]struct{}) (Manifest, error) {
	packagesByPath := make(map[string][]string)
	packageNames := make(map[string]string)
	for line := range lines {
		if strings.HasPrefix(line, "package ") {
			path, name, err := parsePackageLine(line)
			if err != nil {
				return Manifest{}, fmt.Errorf("invalid API package entry %q: %w", line, err)
			}
			if _, exists := packageNames[path]; exists {
				return Manifest{}, fmt.Errorf(
					"API set contains duplicate package %s",
					path,
				)
			}
			packageNames[path] = name
			if _, exists := packagesByPath[path]; !exists {
				packagesByPath[path] = nil
			}
			continue
		}
		path, declaration, found := strings.Cut(line, ": ")
		if !found || path == "" || declaration == "" {
			return Manifest{}, fmt.Errorf("invalid API set entry %q", line)
		}
		packagesByPath[path] = append(packagesByPath[path], declaration)
	}

	paths := make([]string, 0, len(packagesByPath))
	for path := range packagesByPath {
		if _, exists := packageNames[path]; !exists {
			return Manifest{}, fmt.Errorf(
				"API declarations exist without package %s",
				path,
			)
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)

	manifest := Manifest{Packages: make([]PackageAPI, 0, len(paths))}
	for _, path := range paths {
		declarations := packagesByPath[path]
		slices.Sort(declarations)
		manifest.Packages = append(manifest.Packages, PackageAPI{
			Path:         path,
			Name:         packageNames[path],
			Declarations: declarations,
		})
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func hasAdjacentDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
