package screen

import (
	"os"
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureScreen(t *testing.T) {
	t.Parallel()
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X11 display")
	}
	bit, err := robotgo.CaptureScreen()
	if err != nil {
		t.Fatalf("CaptureScreen error: %v", err)
	}
	robotgo.FreeBitmap(bit)
}
