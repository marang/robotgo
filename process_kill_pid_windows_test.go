//go:build windows

package robotgo

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestValidateProcessIDForKillUsesUint32WindowsRange(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("full uint32 PID range requires a 64-bit Go int")
	}
	if err := validateProcessIDForKill(int(math.MaxUint32)); err != nil {
		t.Fatalf("validateProcessIDForKill(MaxUint32): %v", err)
	}
	err := validateProcessIDForKill(int(math.MaxUint32) + 1)
	if !errors.Is(err, ErrInvalidPID) {
		t.Fatalf("validateProcessIDForKill(MaxUint32 + 1) = %v, want ErrInvalidPID", err)
	}
}
