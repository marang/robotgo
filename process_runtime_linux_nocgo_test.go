//go:build linux && !cgo

package robotgo

import (
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

type fakeLinuxPIDFDRuntime struct {
	openFD     int
	openErr    error
	sendErrs   []error
	closeErr   error
	opened     bool
	closeCalls int
	closedFD   int
	signals    []unix.Signal
}

func (runtime *fakeLinuxPIDFDRuntime) dependencies() linuxPIDFDRuntime {
	return linuxPIDFDRuntime{
		openPIDFD: func(int, int) (int, error) {
			runtime.opened = true
			return runtime.openFD, runtime.openErr
		},
		closeFD: func(fd int) error {
			runtime.closeCalls++
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
			if len(runtime.sendErrs) == 0 {
				return nil
			}
			err := runtime.sendErrs[0]
			runtime.sendErrs = runtime.sendErrs[1:]
			return err
		},
	}
}

func TestOpenCloseWindowProcessLinuxBindsBeforeUse(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openFD: 9}

	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if err != nil {
		t.Fatalf("openCloseWindowProcessLinux() error = %v", err)
	}
	if !runtime.opened {
		t.Fatal("pidfd was not opened")
	}

	running, err := reference.Running()
	if err != nil {
		t.Fatalf("Running() error = %v", err)
	}
	if !running {
		t.Fatal("Running() = false, want true")
	}
	if err := reference.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if got, want := runtime.signals, []unix.Signal{0, unix.SIGKILL}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pidfd signals = %v, want %v", got, want)
	}
	if err := reference.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if runtime.closedFD != 9 {
		t.Fatalf("closed fd = %d, want 9", runtime.closedFD)
	}
}

func TestCloseWindowProcessLinuxHandlesBoundProcessExit(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{
		openFD:   9,
		sendErrs: []error{unix.ESRCH, unix.ESRCH},
	}
	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if err != nil {
		t.Fatalf("openCloseWindowProcessLinux() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := reference.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	running, err := reference.Running()
	if err != nil {
		t.Fatalf("Running() error = %v", err)
	}
	if running {
		t.Fatal("Running() = true after bound process exit")
	}
	if err := reference.Kill(); err != nil {
		t.Fatalf("Kill() after exit error = %v", err)
	}
}

func TestOpenCloseWindowProcessLinuxReportsUnavailablePIDFD(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openErr: unix.ENOSYS}

	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if reference != nil {
		t.Fatalf("reference = %#v, want nil", reference)
	}
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("error = %v, want ErrNotSupported", err)
	}
	if runtime.closeCalls != 0 {
		t.Fatalf("close calls = %d after failed open", runtime.closeCalls)
	}
}

func TestOpenCloseWindowProcessLinuxDoesNotTreatPreBindExitAsSuccess(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openErr: unix.ESRCH}

	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if reference != nil {
		t.Fatalf("reference = %#v, want nil", reference)
	}
	if !errors.Is(err, unix.ESRCH) {
		t.Fatalf("error = %v, want ESRCH", err)
	}
}

func TestCloseWindowProcessLinuxReportsAndDoesNotRepeatCloseFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	runtime := &fakeLinuxPIDFDRuntime{openFD: 9, closeErr: closeErr}
	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if err != nil {
		t.Fatalf("openCloseWindowProcessLinux() error = %v", err)
	}

	if err := reference.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("first Close() error = %v, want close error", err)
	}
	runtime.closeErr = errors.New("unexpected repeated close")
	if err := reference.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", runtime.closeCalls)
	}
}

func TestCloseWindowProcessLinuxClosesFileDescriptorZero(t *testing.T) {
	runtime := &fakeLinuxPIDFDRuntime{openFD: 0}
	reference, err := openCloseWindowProcessLinux(42, runtime.dependencies())
	if err != nil {
		t.Fatalf("openCloseWindowProcessLinux() error = %v", err)
	}

	if err := reference.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if runtime.closeCalls != 1 || runtime.closedFD != 0 {
		t.Fatalf(
			"close result = calls %d, fd %d; want calls 1, fd 0",
			runtime.closeCalls,
			runtime.closedFD,
		)
	}
}
