//go:build linux

package portalrunner

import (
	"errors"
	"os"
	"syscall"
)

func validateCurrentOwner(info os.FileInfo) error {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != uint32(os.Getuid()) {
		return errors.New("portal runner state is not owned by the current user")
	}
	return nil
}
