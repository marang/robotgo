//go:build linux && !waylandint

package key

import (
	"testing"

	"go.izoone.com/robotgo"
)

func TestCurrentSpecialTableWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	if s := robotgo.CurrentSpecialTable()["+"]; s != "=" {
		t.Fatalf("expected =, got %s", s)
	}
}
