//go:build linux

package portalrunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// StateLock prevents concurrent image mutation, probing, and protected runner
// execution within one private state root.
type StateLock struct {
	file *os.File
}

// AcquireStateLock obtains a non-blocking exclusive lock without following a
// replacement symlink.
func AcquireStateLock(stateRoot string) (*StateLock, error) {
	lockPath := filepath.Join(stateRoot, ".lock")
	descriptor, err := syscall.Open(
		lockPath,
		syscall.O_CLOEXEC|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_RDWR,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open portal runner state lock: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), lockPath)
	if file == nil {
		_ = syscall.Close(descriptor)
		return nil, errors.New("create portal runner state lock handle")
	}
	closeOnError := func(returned error) (*StateLock, error) {
		_ = file.Close()
		return nil, returned
	}
	info, err := file.Stat()
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Mode().Perm()&0o077 != 0 {
		return closeOnError(errors.New("portal runner state lock is unsafe"))
	}
	if err := validateCurrentOwner(info); err != nil {
		return closeOnError(err)
	}
	if err := syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return closeOnError(errors.New("portal runner state is already in use"))
		}
		return closeOnError(fmt.Errorf("lock portal runner state: %w", err))
	}
	return &StateLock{file: file}, nil
}

// Close releases the state lock.
func (lock *StateLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	descriptor := int(lock.file.Fd())
	unlockError := syscall.Flock(descriptor, syscall.LOCK_UN)
	closeError := lock.file.Close()
	lock.file = nil
	return errors.Join(unlockError, closeError)
}
