//go:build !cgo && !windows && !linux

package robotgo

import (
	"errors"
	"fmt"
	"runtime"
)

type identityCloseWindowProcess struct {
	pid      int
	identity int64
}

func openCloseWindowProcess(pid int) (closeWindowProcess, error) {
	identity, err := closeWindowProcessIdentity(pid)
	if err != nil {
		return nil, err
	}
	return &identityCloseWindowProcess{pid: pid, identity: identity}, nil
}

func (process *identityCloseWindowProcess) Running() (bool, error) {
	exists, err := PidExists(process.pid)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	currentIdentity, err := closeWindowProcessIdentity(process.pid)
	if err != nil {
		stillExists, probeErr := PidExists(process.pid)
		if probeErr != nil {
			return false, errors.Join(err, probeErr)
		}
		if !stillExists {
			return false, nil
		}
		return false, err
	}
	return currentIdentity == process.identity, nil
}

func (process *identityCloseWindowProcess) Kill() error {
	return fmt.Errorf(
		"%w: Pure-Go %s cannot bind force-kill to process %d without a stable process handle",
		ErrNotSupported,
		runtime.GOOS,
		process.pid,
	)
}

func (*identityCloseWindowProcess) Close() error {
	return nil
}
