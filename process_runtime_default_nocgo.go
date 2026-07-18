//go:build !cgo && !windows && !linux

package robotgo

import (
	"fmt"
	"runtime"
)

func closeWindowProcessExists(pid int) (bool, error) {
	return PidExists(pid)
}

func closeWindowProcessKill(pid int, _ int64) error {
	return fmt.Errorf(
		"%w: Pure-Go %s cannot bind force-kill to process %d without a stable process handle",
		ErrNotSupported,
		runtime.GOOS,
		pid,
	)
}
