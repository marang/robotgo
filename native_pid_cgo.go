//go:build cgo

package robotgo

import (
	"fmt"
	"runtime"
)

// Darwin pid_t is signed 32-bit even when Go int and the native legacy uintptr
// are wider. Validate the original Go value: on arm64 C.uintptr is only 32-bit,
// so a C-side check would already have lost the caller's target identity.
func nativeDarwinPIDValid(pid int, allowGlobal bool) bool {
	const maximumDarwinPID = 1<<31 - 1
	return (pid > 0 || allowGlobal && pid == 0) && uint64(pid) <= maximumDarwinPID
}

func validateNativeInputPID(pid int) error {
	if runtime.GOOS == "darwin" && !nativeDarwinPIDValid(pid, true) {
		return fmt.Errorf("%w: macOS keyboard target %d is outside the signed 32-bit PID range", ErrInvalidPID, pid)
	}
	return nil
}

// SetHandle accepts CGWindowID, not pid_t. Preserve the full unsigned 32-bit
// window-ID range and its zero reset sentinel rather than applying PID rules.
func nativeDarwinWindowIDValid(id int) bool {
	const maximumDarwinWindowID = 1<<32 - 1
	return id >= 0 && uint64(id) <= maximumDarwinWindowID
}
