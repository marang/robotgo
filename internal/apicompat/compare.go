package apicompat

import (
	"fmt"
	"slices"
	"strings"
)

const maxReportedManifestChanges = 80

// Compare returns nil only when the checked-in and current APIs are identical.
// Exact comparison makes additive changes intentional baseline reviews too.
func Compare(baseline, current Manifest) error {
	if err := validateManifest(baseline); err != nil {
		return fmt.Errorf("invalid baseline manifest: %w", err)
	}
	if err := validateManifest(current); err != nil {
		return fmt.Errorf("invalid current manifest: %w", err)
	}

	oldLines := manifestAPISet(baseline)
	newLines := manifestAPISet(current)

	var removed []string
	var added []string
	for line := range oldLines {
		if _, ok := newLines[line]; !ok {
			removed = append(removed, line)
		}
	}
	for line := range newLines {
		if _, ok := oldLines[line]; !ok {
			added = append(added, line)
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		return nil
	}

	sortManifestChanges(removed)
	sortManifestChanges(added)

	var message strings.Builder
	fmt.Fprintf(
		&message,
		"public API differs from the checked-in baseline (%d removed/changed, %d added/changed)",
		len(removed),
		len(added),
	)
	reported := 0
	for _, line := range removed {
		if reported == maxReportedManifestChanges {
			break
		}
		message.WriteString("\n- ")
		message.WriteString(line)
		reported++
	}
	for _, line := range added {
		if reported == maxReportedManifestChanges {
			break
		}
		message.WriteString("\n+ ")
		message.WriteString(line)
		reported++
	}
	if remaining := len(removed) + len(added) - reported; remaining > 0 {
		fmt.Fprintf(&message, "\n... %d additional changes omitted", remaining)
	}
	return fmt.Errorf("%s", message.String())
}

func manifestAPISet(manifest Manifest) map[string]struct{} {
	total := 0
	for _, pkg := range manifest.Packages {
		total += len(pkg.Declarations) + 1
	}
	lines := make(map[string]struct{}, total)
	for _, pkg := range manifest.Packages {
		lines[renderPackageLine(pkg)] = struct{}{}
		for _, declaration := range pkg.Declarations {
			lines[pkg.Path+": "+declaration] = struct{}{}
		}
	}
	return lines
}

func sortManifestChanges(changes []string) {
	slices.Sort(changes)
}
