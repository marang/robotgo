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

func captureCloseWindowProcessIdentity(
	pid int,
) (closeWindowProcessFingerprint, error) {
	identity, err := closeWindowProcessIdentity(pid)
	if err != nil {
		return closeWindowProcessFingerprint{}, err
	}
	return closeWindowProcessFingerprint{primary: uint64(identity)}, nil
}

func openCloseWindowProcess(
	pid int,
	expected closeWindowProcessFingerprint,
) (closeWindowProcess, error) {
	current, err := captureCloseWindowProcessIdentity(pid)
	if err != nil {
		return nil, err
	}
	if current != expected {
		return nil, fmt.Errorf("process %d identity changed during binding", pid)
	}
	return &identityCloseWindowProcess{
		pid:      pid,
		identity: int64(expected.primary),
	}, nil
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
