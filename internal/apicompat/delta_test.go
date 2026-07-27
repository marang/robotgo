package apicompat

import (
	"strings"
	"testing"
)

func TestManifestDeltaRoundTrip(t *testing.T) {
	t.Parallel()

	base := Manifest{Packages: []PackageAPI{
		{
			Path: "example.test/a",
			Declarations: []string{
				"func Changed(int)",
				"func Kept()",
			},
		},
	}}
	current := Manifest{Packages: []PackageAPI{
		{
			Path: "example.test/a",
			Declarations: []string{
				"func Changed(string)",
				"func Kept()",
			},
		},
		{
			Path:         "example.test/newpkg",
			Declarations: []string{"func New()"},
		},
	}}

	delta, err := ManifestDelta(base, current, "base.api")
	if err != nil {
		t.Fatalf("ManifestDelta: %v", err)
	}
	parsed, err := ParseDelta(delta.Render())
	if err != nil {
		t.Fatalf("ParseDelta: %v", err)
	}
	reconstructed, err := ApplyDelta(base, parsed)
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if err := Compare(current, reconstructed); err != nil {
		t.Fatalf("delta round trip: %v", err)
	}
	if parsed.Base != "base.api" {
		t.Fatalf("delta base = %q", parsed.Base)
	}
}

func TestApplyDeltaRejectsStaleEntries(t *testing.T) {
	t.Parallel()

	base := Manifest{Packages: []PackageAPI{{
		Path:         "example.test/a",
		Declarations: []string{"func Kept()"},
	}}}
	_, err := ApplyDelta(base, Delta{
		Base:    "base.api",
		Removed: []string{"example.test/a: func Missing()"},
	})
	if err == nil || !strings.Contains(err.Error(), "removes missing API") {
		t.Fatalf("stale removal result = %v", err)
	}
}

func TestParseDeltaRejectsAmbiguousChanges(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		deltaHeader,
		"base base.api",
		"- example.test/a: func Changed()",
		"+ example.test/a: func Changed()",
		"",
	}, "\n")
	if _, err := ParseDelta(body); err == nil {
		t.Fatal("remove/add overlap accepted")
	}
}
