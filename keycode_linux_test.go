//go:build linux
// +build linux

package robotgo

import (
	"os"
	"testing"

	"github.com/vcaesar/tt"
)

func TestCurrentSpecialTableX11(t *testing.T) {
	os.Setenv("DISPLAY", ":0")
	os.Unsetenv("WAYLAND_DISPLAY")
	s := CurrentSpecialTable()["+"]
	tt.Equal(t, "=", s)
}

func TestCurrentSpecialTableWayland(t *testing.T) {
	os.Unsetenv("DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	s := CurrentSpecialTable()["+"]
	tt.Equal(t, "=", s)
	os.Unsetenv("WAYLAND_DISPLAY")
}
