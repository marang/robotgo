//go:build windows

package windowswindow

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/tailscale/win"
	"golang.org/x/sys/windows"
)

var (
	user32DLL           = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows     = user32DLL.NewProc("EnumWindows")
	procIsIconic        = user32DLL.NewProc("IsIconic")
	procIsWindow        = user32DLL.NewProc("IsWindow")
	procIsZoomed        = user32DLL.NewProc("IsZoomed")
	errNoMatchingWindow = errors.New("no top-level window belongs to the process")
)

type nativeSystem struct{}

// NewNative constructs a backend that calls User32 directly.
func NewNative() *Backend {
	return New(nativeSystem{})
}

func (nativeSystem) ForegroundWindow() Handle {
	return Handle(win.GetForegroundWindow())
}

func (nativeSystem) IsWindow(handle Handle) bool {
	result, _, _ := procIsWindow.Call(uintptr(handle))
	return result != 0
}

func (nativeSystem) FindWindowByPID(pid uint32) (Handle, error) {
	if pid == 0 {
		return 0, errNoMatchingWindow
	}

	var fallback, preferred Handle
	callback := syscall.NewCallback(func(rawHandle, _ uintptr) uintptr {
		handle := win.HWND(rawHandle)
		var ownerPID uint32
		win.GetWindowThreadProcessId(handle, &ownerPID)
		if ownerPID != pid {
			return 1
		}
		if fallback == 0 {
			fallback = Handle(rawHandle)
		}
		if preferred == 0 && win.IsWindowVisible(handle) &&
			win.GetWindow(handle, win.GW_OWNER) == 0 {
			preferred = Handle(rawHandle)
		}
		return 1
	})
	result, _, callErr := procEnumWindows.Call(callback, 0)
	if result == 0 {
		return 0, windowsCallError("EnumWindows", callErr)
	}
	if preferred != 0 {
		return preferred, nil
	}
	if fallback != 0 {
		return fallback, nil
	}
	return 0, errNoMatchingWindow
}

func (nativeSystem) WindowProcessID(handle Handle) (uint32, error) {
	var pid uint32
	if threadID := win.GetWindowThreadProcessId(win.HWND(handle), &pid); threadID == 0 || pid == 0 {
		return 0, errors.New("GetWindowThreadProcessId returned no process")
	}
	return pid, nil
}

func (nativeSystem) WindowText(handle Handle) (string, error) {
	hwnd := win.HWND(handle)
	length := win.GetWindowTextLength(hwnd)
	if length == 0 {
		return "", nil
	}
	buffer := make([]uint16, int(length)+1)
	written := win.GetWindowText(hwnd, &buffer[0], int32(len(buffer)))
	if written == 0 {
		return "", errors.New("GetWindowTextW returned no title")
	}
	return windows.UTF16ToString(buffer[:written]), nil
}

func (nativeSystem) WindowRect(handle Handle) (Rect, error) {
	var native win.RECT
	if !win.GetWindowRect(win.HWND(handle), &native) {
		return Rect{}, errors.New("GetWindowRect failed")
	}
	return rectFromNative(native, win.POINT{X: native.Left, Y: native.Top}), nil
}

func (nativeSystem) ClientRect(handle Handle) (Rect, error) {
	hwnd := win.HWND(handle)
	var native win.RECT
	if !win.GetClientRect(hwnd, &native) {
		return Rect{}, errors.New("GetClientRect failed")
	}
	origin := win.POINT{X: native.Left, Y: native.Top}
	if !win.ClientToScreen(hwnd, &origin) {
		return Rect{}, errors.New("ClientToScreen failed")
	}
	return rectFromNative(native, origin), nil
}

func (nativeSystem) SetForegroundWindow(handle Handle) error {
	if !win.SetForegroundWindow(win.HWND(handle)) {
		return errors.New("SetForegroundWindow was denied")
	}
	return nil
}

func (nativeSystem) SetWindowState(handle Handle, state State, enabled bool) error {
	var command int32
	switch state {
	case StateMinimized:
		if enabled {
			command = win.SW_MINIMIZE
		} else {
			command = win.SW_RESTORE
		}
	case StateMaximized:
		if enabled {
			command = win.SW_MAXIMIZE
		} else {
			command = win.SW_RESTORE
		}
	default:
		return fmt.Errorf("unknown window state %d", state)
	}
	// ShowWindow reports the previous visibility state, not operation success.
	win.ShowWindow(win.HWND(handle), command)
	actual, err := (nativeSystem{}).WindowState(handle, state)
	if err != nil {
		return err
	}
	if actual != enabled {
		return fmt.Errorf("ShowWindow did not apply state %d=%t", state, enabled)
	}
	return nil
}

func (nativeSystem) WindowState(handle Handle, state State) (bool, error) {
	var procedure *windows.LazyProc
	switch state {
	case StateMinimized:
		procedure = procIsIconic
	case StateMaximized:
		procedure = procIsZoomed
	default:
		return false, fmt.Errorf("unknown window state %d", state)
	}
	result, _, _ := procedure.Call(uintptr(handle))
	return result != 0, nil
}

func (nativeSystem) IsTopMost(handle Handle) (bool, error) {
	style := win.GetWindowLongPtr(win.HWND(handle), win.GWL_EXSTYLE)
	return style&uintptr(win.WS_EX_TOPMOST) != 0, nil
}

func (nativeSystem) SetTopMost(handle Handle, enabled bool) error {
	insertAfter := win.HWND_NOTOPMOST
	if enabled {
		insertAfter = win.HWND_TOPMOST
	}
	if !win.SetWindowPos(
		win.HWND(handle), insertAfter,
		0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE,
	) {
		return errors.New("SetWindowPos failed")
	}
	return nil
}

func (nativeSystem) CloseWindow(handle Handle) error {
	if win.PostMessage(win.HWND(handle), win.WM_CLOSE, 0, 0) == 0 {
		return errors.New("PostMessage(WM_CLOSE) failed")
	}
	return nil
}

func rectFromNative(native win.RECT, origin win.POINT) Rect {
	return Rect{
		X:      int(origin.X),
		Y:      int(origin.Y),
		Width:  int(native.Right - native.Left),
		Height: int(native.Bottom - native.Top),
	}
}

func windowsCallError(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s failed: %w", operation, callErr)
}
