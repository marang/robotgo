//go:build linux && !cgo

package robotgo

import (
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

type fakeLinuxPIDFDRuntime struct {
	openFD      int
	openErr     error
	identity    int64
	identityErr error
	sendErr     error
	closeErr    error
	closedFD    int
	signals     []unix.Signal
}

func (runtime *fakeLinuxPIDFDRuntime) dependencies() linuxPIDFDRuntime {
	return linuxPIDFDRuntime{
		openPIDFD: func(int, int) (int, error) {
			return runtime.openFD, runtime.openErr
		},
		closeFD: func(fd int) error {
			runtime.closedFD = fd
			return runtime.closeErr
		},
		sendSignal: func(
			_ int,
			signal unix.Signal,
			_ *unix.Siginfo,
			_ int,
		) error {
			runtime.signals = append(runtime.signals, signal)
			return runtime.sendErr
		},
		processIdentity: func(int) (int64, error) {
			return runtime.identity, runtime.identityErr
		},
	}
}

func TestCloseWindowProcessKillLinuxUsesBoundPIDFD(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openFD: 9, identity: 100}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if err != nil {
		t.Fatalf("closeWindowProcessKillLinux() error = %v", err)
	}
	if got, want := runtime.signals, []unix.Signal{unix.SIGKILL}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pidfd signals = %v, want %v", got, want)
	}
	if runtime.closedFD != 9 {
		t.Fatalf("closed fd = %d, want 9", runtime.closedFD)
	}
}

func TestCloseWindowProcessKillLinuxRejectsReusedPID(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openFD: 9, identity: 200}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if err != nil {
		t.Fatalf("closeWindowProcessKillLinux() error = %v", err)
	}
	if len(runtime.signals) != 0 {
		t.Fatalf("signals for reused PID = %v, want none", runtime.signals)
	}
}

func TestCloseWindowProcessKillLinuxHandlesExitDuringIdentityProbe(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{
		openFD:      9,
		identityErr: errors.New("process exited"),
		sendErr:     unix.ESRCH,
	}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if err != nil {
		t.Fatalf("closeWindowProcessKillLinux() error = %v", err)
	}
	if got, want := runtime.signals, []unix.Signal{linuxPIDFDProcessExistenceSignal}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pidfd signals = %v, want %v", got, want)
	}
}

func TestCloseWindowProcessKillLinuxReportsUnavailablePIDFD(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openErr: unix.ENOSYS}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("error = %v, want ErrNotSupported", err)
	}
	if runtime.closedFD != 0 {
		t.Fatalf("closed fd = %d after failed open", runtime.closedFD)
	}
}

func TestCloseWindowProcessKillLinuxHandlesExitBeforePIDFDOpen(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openErr: unix.ESRCH}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if err != nil {
		t.Fatalf("closeWindowProcessKillLinux() error = %v", err)
	}
	if runtime.closedFD != 0 {
		t.Fatalf("closed fd = %d after failed open", runtime.closedFD)
	}
}

func TestCloseWindowProcessKillLinuxReportsCloseFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	runtime := &fakeLinuxPIDFDRuntime{
		openFD:   9,
		identity: 100,
		closeErr: closeErr,
	}

	err := closeWindowProcessKillLinux(42, 100, runtime.dependencies())
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want close error", err)
	}
}
