//go:build windows

package clipboard

import (
	"context"
	"errors"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsClipboardNativeErrorConventions(t *testing.T) {
	for _, absent := range []error{nil, syscall.Errno(0)} {
		if err := windowsClipboardError("fixture", absent); err == nil {
			t.Fatal("failed call with no last error must still fail")
		}
	}
	denied := syscall.Errno(5)
	if err := windowsClipboardError("fixture", denied); !errors.Is(err, denied) {
		t.Fatalf("last error identity lost: %v", err)
	}
	for _, result := range []struct {
		value uintptr
		err   error
		fail  bool
	}{{0, nil, true}, {0, syscall.Errno(0), true}, {0, denied, true}, {1, denied, false}} {
		call := func(...uintptr) (uintptr, uintptr, error) { return result.value, 0, result.err }
		if err := windowsClipboardBool("fixture", call); (err != nil) != result.fail {
			t.Fatalf("BOOL(%d, %v): %v", result.value, result.err, err)
		}
	}
}

func TestWindowsClipboardUnlockClearsStaleError(t *testing.T) {
	for _, result := range []struct {
		value uintptr
		err   error
		fail  bool
	}{{0, nil, false}, {0, syscall.Errno(0), false}, {0, syscall.Errno(5), true}, {1, syscall.Errno(5), false}} {
		cleared := false
		clear := func(args ...uintptr) (uintptr, uintptr, error) {
			if len(args) != 1 || args[0] != 0 {
				t.Fatal("SetLastError must clear to zero")
			}
			cleared = true
			return 0, 0, syscall.Errno(5) // SetLastError has no meaningful return value
		}
		unlock := func(args ...uintptr) (uintptr, uintptr, error) {
			if !cleared || len(args) != 1 || args[0] != 42 {
				t.Fatal("unlock must immediately follow clear and use its handle")
			}
			return result.value, 0, result.err
		}
		if err := unlockWindowsClipboard(42, clear, unlock); (err != nil) != result.fail {
			t.Fatalf("unlock(%d, %v): %v", result.value, result.err, err)
		}
	}
}

// Only actual thread IDs are observed. All clipboard and storage operations
// remain injected; the system clipboard is never opened by this test.
func TestWindowsClipboardTransactionsStayOnOneThread(t *testing.T) {
	for _, write := range []bool{false, true} {
		var thread uint32
		observe := func() {
			current := windows.GetCurrentThreadId()
			if thread == 0 {
				thread = current
			}
			if current != thread {
				t.Fatalf("transaction migrated from thread %d to %d", thread, current)
			}
			runtime.Gosched()
		}
		data := []uint16{'o', 'k', 0}
		ops := windowsClipboardOps{
			open:         func(uintptr) error { observe(); return nil },
			close:        func() error { observe(); return nil },
			data:         func() (uintptr, error) { observe(); return 1, nil },
			size:         func(uintptr) (uintptr, error) { observe(); return uintptr(len(data) * 2), nil },
			lock:         func(uintptr) (unsafe.Pointer, error) { observe(); return unsafe.Pointer(&data[0]), nil },
			unlock:       func(uintptr) error { observe(); return nil },
			alloc:        func(uintptr) (uintptr, error) { observe(); return 1, nil },
			free:         func(uintptr) error { t.Fatal("unexpected free after transfer"); return nil },
			empty:        func() error { observe(); return nil },
			set:          func(uintptr) error { observe(); return nil },
			createOwner:  func() (uintptr, error) { observe(); return 2, nil },
			destroyOwner: func(uintptr) error { observe(); return nil },
		}
		if write {
			if err := writeWindowsClipboard(context.Background(), "ok", ops); err != nil {
				t.Fatal(err)
			}
		} else if text, err := readWindowsClipboard(context.Background(), ops); err != nil || text != "ok" {
			t.Fatalf("read = %q, %v", text, err)
		}
	}
}

func TestWindowsClipboardPrivateOwnerWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ops := nativeWindowsClipboardOps()
	owner, err := ops.createOwner()
	if err != nil {
		t.Fatal(err)
	}
	// Creating/destroying a private message-only window neither reads nor
	// changes the clipboard. No Open/Empty/Set calls occur here.
	if owner == 0 {
		t.Fatal("owner must be nonzero")
	}
	if err := ops.destroyOwner(owner); err != nil {
		t.Fatal(err)
	}
}
