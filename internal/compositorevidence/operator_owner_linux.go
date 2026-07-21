//go:build linux

package compositorevidence

import (
	"errors"
	"os"
	"syscall"
)

func validateOperatorReadyOwner(info os.FileInfo) error {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != 0 {
		return errors.New("operator readiness attestation is not owned by root")
	}
	return nil
}
