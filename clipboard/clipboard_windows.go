// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package clipboard

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodetext = 13
	gmemMoveable  = 0x0002
	// HWND_MESSAGE is the documented pseudo-handle (HWND)-3.
	clipboardMessageWindow = ^uintptr(2)
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	getClipboardData = user32.NewProc("GetClipboardData")
	setClipboardData = user32.NewProc("SetClipboardData")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")

	kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalFree   = kernel32.NewProc("GlobalFree")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalSize   = kernel32.NewProc("GlobalSize")
	setLastError = kernel32.NewProc("SetLastError")
)

type windowsClipboardCall func(...uintptr) (uintptr, uintptr, error)

func windowsClipboardError(operation string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("clipboard: %s failed without an error code", operation)
	}
	return fmt.Errorf("clipboard: %s: %w", operation, err)
}

func windowsClipboardBool(operation string, call windowsClipboardCall, args ...uintptr) error {
	if r, _, err := call(args...); r == 0 {
		return windowsClipboardError(operation, err)
	}
	return nil
}

// The caller pins the OS thread, including this last-error pair. Zero means
// successful final unlock only when GetLastError is also zero.
func unlockWindowsClipboard(h uintptr, clear, unlock windowsClipboardCall) error {
	_, _, _ = clear(0) // SetLastError has no return value to interpret.
	r, _, err := unlock(h)
	if r == 0 && err != nil && err != syscall.Errno(0) {
		return windowsClipboardError("GlobalUnlock", err)
	}
	return nil
}

func nativeWindowsClipboardOps() windowsClipboardOps {
	return windowsClipboardOps{
		open:  func(owner uintptr) error { return windowsClipboardBool("OpenClipboard", openClipboard.Call, owner) },
		close: func() error { return windowsClipboardBool("CloseClipboard", closeClipboard.Call) },
		data: func() (uintptr, error) {
			h, _, err := getClipboardData.Call(cfUnicodetext)
			if h == 0 {
				// Keep missing text an error, distinct from a valid empty string.
				return 0, windowsClipboardError("GetClipboardData(CF_UNICODETEXT)", err)
			}
			return h, nil
		},
		size: func(h uintptr) (uintptr, error) {
			n, _, err := globalSize.Call(h)
			if n == 0 {
				return 0, windowsClipboardError("GlobalSize", err)
			}
			return n, nil
		},
		lock: func(h uintptr) (unsafe.Pointer, error) {
			addr, _, err := globalLock.Call(h)
			if addr == 0 {
				return nil, windowsClipboardError("GlobalLock", err)
			}
			// GlobalLock returns non-Go storage. Keep conversion at the native
			// boundary rather than doing arithmetic on a Go heap uintptr.
			return *(*unsafe.Pointer)(unsafe.Pointer(&addr)), nil
		},
		unlock: func(h uintptr) error { return unlockWindowsClipboard(h, setLastError.Call, globalUnlock.Call) },
		alloc: func(n uintptr) (uintptr, error) {
			h, _, err := globalAlloc.Call(gmemMoveable, n)
			if h == 0 {
				return 0, windowsClipboardError("GlobalAlloc", err)
			}
			return h, nil
		},
		free: func(h uintptr) error {
			if r, _, err := globalFree.Call(h); r != 0 {
				return windowsClipboardError("GlobalFree", err)
			}
			return nil
		},
		empty: func() error { return windowsClipboardBool("EmptyClipboard", emptyClipboard.Call) },
		set: func(h uintptr) error {
			return windowsClipboardBool("SetClipboardData", setClipboardData.Call, cfUnicodetext, h)
		},
		createOwner: func() (uintptr, error) {
			// A predefined STATIC class needs no custom callback, persistent
			// message loop or delayed rendering for eagerly supplied text.
			class := [...]uint16{'S', 'T', 'A', 'T', 'I', 'C', 0}
			h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(&class[0])), 0,
				0, 0, 0, 0, 0, clipboardMessageWindow, 0, 0, 0)
			if h == 0 {
				return 0, windowsClipboardError("CreateWindowExW", err)
			}
			return h, nil
		},
		destroyOwner: func(h uintptr) error { return windowsClipboardBool("DestroyWindow", destroyWindow.Call, h) },
	}
}

func readAll() (string, error) {
	return readAllContext(context.Background(), SelectionClipboard)
}

func readAllContext(ctx context.Context, selection Selection) (string, error) {
	if selection != SelectionClipboard {
		return "", fmt.Errorf("clipboard: primary selection is unsupported on Windows")
	}
	return readWindowsClipboard(ctx, nativeWindowsClipboardOps())
}

func writeAll(text string) error {
	return writeAllContext(context.Background(), text, SelectionClipboard)
}

func writeAllContext(ctx context.Context, text string, selection Selection) error {
	if selection != SelectionClipboard {
		return fmt.Errorf("clipboard: primary selection is unsupported on Windows")
	}
	return writeWindowsClipboard(ctx, text, nativeWindowsClipboardOps())
}
