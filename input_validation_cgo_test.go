//go:build cgo

package robotgo

import (
	"runtime"
	"testing"
	"time"
)

func TestInvalidMouseArgumentsDoNotWaitForWaylandBackendLock(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Wayland mouse serialization is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-validation-test")
	t.Setenv("DISPLAY", "")

	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "click", call: func() error { return ClickE("invalid") }},
		{name: "toggle", call: func() error { return Toggle("invalid") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			waylandMouseMu.Lock()
			done := make(chan error, 1)
			go func() { done <- test.call() }()
			select {
			case err := <-done:
				waylandMouseMu.Unlock()
				if err == nil {
					t.Fatal("invalid arguments unexpectedly succeeded")
				}
			case <-time.After(250 * time.Millisecond):
				waylandMouseMu.Unlock()
				<-done
				t.Fatal("invalid arguments waited for the Wayland backend lock")
			}
		})
	}
}
