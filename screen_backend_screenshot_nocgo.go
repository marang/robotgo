//go:build !cgo && !darwin

package robotgo

import "github.com/vcaesar/screenshot"

func platformDisplayCount() int {
	return screenshot.NumActiveDisplays()
}
