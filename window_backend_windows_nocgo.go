//go:build windows && !cgo

package robotgo

import "github.com/marang/robotgo/internal/windowswindow"

var pureGoWindowsWindowBackend = windowswindow.NewNative()

func platformPureGoWindowBackend() *windowswindow.Backend {
	return pureGoWindowsWindowBackend
}
