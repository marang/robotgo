//go:build darwin

package robotgo

import (
	"fmt"

	"github.com/ebitengine/purego"
)

const coreGraphicsFramework = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"

func darwinScreenCapturePreflight() (granted, supported bool, err error) {
	handle, err := purego.Dlopen(coreGraphicsFramework, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return false, false, fmt.Errorf("load CoreGraphics: %w", err)
	}
	defer func() {
		if closeErr := purego.Dlclose(handle); err == nil && closeErr != nil {
			err = fmt.Errorf("close CoreGraphics: %w", closeErr)
		}
	}()
	symbol, err := purego.Dlsym(handle, "CGPreflightScreenCaptureAccess")
	if err != nil {
		return false, false, nil
	}
	var preflight func() bool
	purego.RegisterFunc(&preflight, symbol)
	return preflight(), true, nil
}
