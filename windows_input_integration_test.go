//go:build windows && !cgo && windowsintegration

package robotgo_test

import (
	"os"
	"testing"

	"github.com/marang/robotgo"
)

const requireWindowsInputIntegration = "ROBOTGO_REQUIRE_WINDOWS_INPUT_INTEGRATION"

func TestPureGoWindowsInputRuntime(t *testing.T) {
	if os.Getenv(requireWindowsInputIntegration) != "1" {
		t.Skip("set " + requireWindowsInputIntegration + "=1 to inject global Windows pointer input")
	}
	if err := robotgo.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady: %v", err)
	}
	if err := robotgo.MouseReady(); err != nil {
		t.Fatalf("MouseReady: %v", err)
	}
	startX, startY, err := robotgo.LocationE()
	if err != nil {
		t.Fatalf("initial LocationE: %v", err)
	}
	t.Cleanup(func() {
		if restoreErr := robotgo.MoveE(startX, startY); restoreErr != nil {
			t.Errorf("restore pointer: %v", restoreErr)
		}
	})

	for _, delta := range []int{1, -1} {
		if err := robotgo.MoveE(startX+delta, startY); err != nil {
			t.Fatalf("MoveE(%d,%d): %v", startX+delta, startY, err)
		}
		x, y, err := robotgo.LocationE()
		if err != nil {
			t.Fatalf("LocationE after move: %v", err)
		}
		if x == startX+delta && y == startY {
			return
		}
	}
	t.Fatal("Windows input desktop accepted no observable one-pixel pointer move")
}
