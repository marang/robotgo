//go:build !cgo && !windows

package robotgo

import (
	"errors"
	"fmt"
)

func closeWindowProcessExists(pid int) (bool, error) {
	return PidExists(pid)
}

func closeWindowProcessKill(pid int, identity int64) error {
	currentIdentity, err := closeWindowProcessIdentity(pid)
	if err != nil {
		exists, existsErr := closeWindowProcessExists(pid)
		if existsErr != nil {
			return fmt.Errorf(
				"verify process %d before termination: %w",
				pid,
				errors.Join(err, existsErr),
			)
		}
		if !exists {
			return nil
		}
		return fmt.Errorf("verify process %d before termination: %w", pid, err)
	}
	if currentIdentity != identity {
		return nil
	}
	return Kill(pid)
}
