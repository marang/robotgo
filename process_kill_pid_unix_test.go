//go:build !windows

package robotgo

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestValidateProcessIDForKillUsesSignedUnixRange(t *testing.T) {
	if err := validateProcessIDForKill(math.MaxInt32); err != nil {
		t.Fatalf("validateProcessIDForKill(MaxInt32): %v", err)
	}
	if strconv.IntSize == 64 {
		err := validateProcessIDForKill(int(math.MaxInt32) + 1)
		if !errors.Is(err, ErrInvalidPID) {
			t.Fatalf("validateProcessIDForKill(MaxInt32 + 1) = %v, want ErrInvalidPID", err)
		}
	}
}
