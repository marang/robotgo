//go:build windows && !cgo

package robotgo

import (
	"math"
	"strconv"
	"testing"
)

func TestWindowsCloseWindowProcessIDSupportsUint32Range(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("full uint32 PID range requires a 64-bit Go int")
	}
	const maximumWindowsPID = int(math.MaxUint32)
	got, err := windowsCloseWindowProcessID(maximumWindowsPID)
	if err != nil {
		t.Fatalf("windowsCloseWindowProcessID(MaxUint32): %v", err)
	}
	if got != math.MaxUint32 {
		t.Fatalf("native PID = %d, want %d", got, uint32(math.MaxUint32))
	}
	if _, err := windowsCloseWindowProcessID(maximumWindowsPID + 1); err == nil {
		t.Fatal("windowsCloseWindowProcessID(MaxUint32 + 1) succeeded")
	}
}
