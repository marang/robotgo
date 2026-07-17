//go:build !windows && !cgo

package robotgo

import "github.com/marang/robotgo/internal/windowswindow"

func platformPureGoWindowBackend() *windowswindow.Backend {
	return nil
}
