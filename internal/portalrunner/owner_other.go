//go:build !linux

package portalrunner

import (
	"errors"
	"os"
)

func validateCurrentOwner(os.FileInfo) error {
	return errors.New("portal runner state is supported only on Linux")
}
