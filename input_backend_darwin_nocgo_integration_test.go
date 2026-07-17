//go:build darwin && !cgo && darwinintegration

package robotgo

import (
	"os"
	"testing"
)

func TestPureGoDarwinPointerIntegration(t *testing.T) {
	if os.Getenv("ROBOTGO_REQUIRE_DARWIN_INPUT_INTEGRATION") != "1" {
		t.Skip("set ROBOTGO_REQUIRE_DARWIN_INPUT_INTEGRATION=1 after granting Accessibility access")
	}
	if err := MouseReady(); err != nil {
		t.Fatalf("MouseReady: %v", err)
	}
	originalX, originalY, err := LocationE()
	if err != nil {
		t.Fatalf("original LocationE: %v", err)
	}
	t.Cleanup(func() {
		if err := MoveE(originalX, originalY); err != nil {
			t.Errorf("restore pointer: %v", err)
		}
		if err := CloseMainDisplayE(); err != nil {
			t.Errorf("CloseMainDisplayE: %v", err)
		}
	})

	bounds := GetScreenRect()
	if bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("invalid main-display bounds: %+v", bounds)
	}
	targetX := bounds.X + bounds.W/2
	targetY := bounds.Y + bounds.H/2
	if err := MoveE(targetX, targetY); err != nil {
		t.Fatalf("MoveE(%d,%d): %v", targetX, targetY, err)
	}
	actualX, actualY, err := LocationE()
	if err != nil {
		t.Fatalf("moved LocationE: %v", err)
	}
	if actualX != targetX || actualY != targetY {
		t.Fatalf(
			"pointer location = (%d,%d), want (%d,%d)",
			actualX, actualY, targetX, targetY,
		)
	}
}
