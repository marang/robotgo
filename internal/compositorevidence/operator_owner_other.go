//go:build !linux

package compositorevidence

import (
	"errors"
	"os"
)

func validateOperatorReadyOwner(os.FileInfo) error {
	return errors.New("operator readiness attestation is supported only on Linux")
}
