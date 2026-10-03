//go:build cgo

package robotgo

import (
	"runtime"

	"github.com/marang/robotgo/clipboard"
)

// ReadAll read string from clipboard
func ReadAll() (string, error) {
	return clipboard.ReadAll()
}

// WriteAll write string to clipboard
func WriteAll(text string) error {
	return clipboard.WriteAll(text)
}

// PasteStr paste a string (support UTF-8),
// write the string to clipboard and tap `cmd + v`
func PasteStr(str string) error {
	err := clipboard.WriteAll(str)
	if err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		return KeyTap("v", "command")
	}

	return KeyTap("v", "control")
}
