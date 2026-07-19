package robotgo

import "testing"

func TestReleaseVersion(t *testing.T) {
	const expected = "v1.0.0-beta.1"
	if Version != expected {
		t.Fatalf("Version = %q, want %q", Version, expected)
	}
	if got := GetVersion(); got != expected {
		t.Fatalf("GetVersion() = %q, want %q", got, expected)
	}
}
