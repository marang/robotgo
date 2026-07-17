//go:build !darwin

package darwininput

import "fmt"

func openNativeSystem() (inputSystem, error) {
	return nil, fmt.Errorf("%w: Quartz input is available only on macOS", ErrUnsupported)
}
