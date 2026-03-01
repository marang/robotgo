//go:build linux

package base

import "testing"

func TestDetectDisplayServer(t *testing.T) {

	tests := []struct {
		name           string
		waylandDisplay string
		display        string
		want           DisplayServer
	}{
		{name: "Wayland", waylandDisplay: "wayland-0", display: "", want: Wayland},
		{name: "X11", waylandDisplay: "", display: ":0", want: X11},
		{name: "Unknown", waylandDisplay: "", display: "", want: Unknown},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", tt.waylandDisplay)
			t.Setenv("DISPLAY", tt.display)
			if got := DetectDisplayServer(); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
