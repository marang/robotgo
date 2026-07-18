//go:build !cgo && !darwin && !linux

package robotgo

import "github.com/vcaesar/screenshot"

func platformDisplayCount() int {
	return screenshot.NumActiveDisplays()
}
