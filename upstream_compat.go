package robotgo

import (
	"fmt"
	"image"
)

// CmdV presses the platform paste shortcut.
func CmdV() error {
	return cmdVWith(CmdCtrl(), KeyTap)
}

func cmdVWith(modifier string, tap func(string, ...interface{}) error) error {
	return tap("v", modifier)
}

// Paste writes text to the clipboard and presses the platform paste shortcut.
func Paste(text string) error {
	return PasteStr(text)
}

// Type sends UTF-8 text. It is an upstream-compatible alias for TypeStr.
func Type(text string, args ...int) {
	TypeStr(text, args...)
}

// TypeDelay sends UTF-8 text and waits for delay milliseconds afterwards.
func TypeDelay(text string, delay int) {
	TypeStrDelay(text, delay)
}

// ClickV1 clicks a mouse button. It preserves the upstream compatibility name
// while the established RobotGo Click API remains source-compatible.
func ClickV1(args ...interface{}) {
	Click(args...)
}

// MultiClick performs count single clicks and stops at the first backend
// error. The optional compatibility argument is accepted for source parity
// with upstream; RobotGo uses the same checked path on every platform.
func MultiClick(button string, count int, compatibility ...bool) error {
	return multiClickWith(button, count, compatibility, ClickE)
}

func multiClickWith(
	button string,
	count int,
	compatibility []bool,
	click func(...interface{}) error,
) error {
	_ = compatibility
	for index := 0; index < count; index++ {
		if err := click(button, false); err != nil {
			return fmt.Errorf("robotgo: multi-click %d/%d: %w", index+1, count, err)
		}
	}
	return nil
}

// Capture1 captures a screen region. It preserves the compatibility name used
// by upstream's platform adapters; new code should use Capture.
func Capture1(args ...int) (*image.RGBA, error) {
	return Capture(args...)
}

// SaveCaptureGo captures and saves an image. It is an upstream-compatible
// alias for SaveCapture.
func SaveCaptureGo(path string, args ...int) error {
	return SaveCapture(path, args...)
}
