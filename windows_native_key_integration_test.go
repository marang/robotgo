//go:build windows && cgo

package robotgo

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/tailscale/win"
)

const (
	nativeWindowsKeyMessageTimeout   = 3 * time.Second
	nativeWindowsNoKeyMessageTimeout = 100 * time.Millisecond
)

type nativeWindowsKeyMessage struct {
	message uint32
	key     uintptr
}

func TestNativeWindowsProcessTargetedKeyInputFailsClosed(t *testing.T) {
	messages, _ := startNativeWindowsKeyTarget(t)
	pid := os.Getpid()

	if err := KeyToggleImmediate("delete", "down", "ctrl", pid); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("process-targeted extended key down error = %v", err)
	}
	assertNoNativeWindowsKeyMessage(t, messages)

	if err := KeyToggleImmediate("delete", "up", "ctrl", pid); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("process-targeted extended key up error = %v", err)
	}
	assertNoNativeWindowsKeyMessage(t, messages)

	if err := KeyTap("v", "ctrl", pid); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("process-targeted key tap error = %v", err)
	}
	assertNoNativeWindowsKeyMessage(t, messages)
}

func assertNoNativeWindowsKeyMessage(
	t *testing.T,
	messages <-chan nativeWindowsKeyMessage,
) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf(
			"ownership rejection emitted key message {message:%#x key:%#x}",
			message.message,
			message.key,
		)
	case <-time.After(nativeWindowsNoKeyMessageTimeout):
	}
}

func startNativeWindowsKeyTarget(t *testing.T) (<-chan nativeWindowsKeyMessage, func()) {
	t.Helper()
	type createdWindow struct {
		handle win.HWND
	}
	created := make(chan createdWindow, 1)
	failed := make(chan error, 1)
	stopped := make(chan error, 1)
	messages := make(chan nativeWindowsKeyMessage, 4)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var stopErr error
		defer func() {
			stopped <- stopErr
			close(stopped)
		}()

		instance := win.GetModuleHandle(nil)
		className, err := syscall.UTF16PtrFromString(
			fmt.Sprintf("RobotGoNativeKeyTarget%d", os.Getpid()),
		)
		if err != nil {
			failed <- err
			return
		}
		windowProc := syscall.NewCallback(func(
			handle uintptr,
			message uint32,
			wParam, lParam uintptr,
		) uintptr {
			switch message {
			case win.WM_KEYDOWN, win.WM_KEYUP:
				messages <- nativeWindowsKeyMessage{message: message, key: wParam}
				return 0
			case win.WM_DESTROY:
				win.PostQuitMessage(0)
				return 0
			}
			return win.DefWindowProc(win.HWND(handle), message, wParam, lParam)
		})
		class := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   windowProc,
			HInstance:     instance,
			LpszClassName: className,
		}
		if atom := win.RegisterClassEx(&class); atom == 0 {
			failed <- nativeWindowsCallError("RegisterClassEx")
			return
		}
		defer func() {
			unregistered, _, callErr := syscall.NewLazyDLL("user32.dll").
				NewProc("UnregisterClassW").
				Call(uintptr(unsafe.Pointer(className)), uintptr(instance))
			runtime.KeepAlive(className)
			if unregistered == 0 {
				stopErr = errors.Join(
					stopErr,
					nativeWindowsCallResultError("UnregisterClassW", callErr),
				)
			}
		}()

		title, err := syscall.UTF16PtrFromString("RobotGo native key target")
		if err != nil {
			failed <- err
			return
		}
		handle := win.CreateWindowEx(
			0,
			className,
			title,
			win.WS_OVERLAPPED,
			0, 0, 100, 100,
			0, 0, instance, nil,
		)
		if handle == 0 {
			failed <- nativeWindowsCallError("CreateWindowEx")
			return
		}
		created <- createdWindow{handle: handle}

		var message win.MSG
		for {
			result := win.GetMessage(&message, 0, 0, 0)
			if result == 0 {
				break
			}
			if int32(result) == -1 {
				stopErr = nativeWindowsCallError("GetMessage")
				return
			}
			win.TranslateMessage(&message)
			win.DispatchMessage(&message)
		}
		runtime.KeepAlive(windowProc)
	}()

	var window createdWindow
	select {
	case window = <-created:
	case err := <-failed:
		t.Fatalf("create native Windows key target: %v", err)
	case <-time.After(nativeWindowsKeyMessageTimeout):
		t.Fatal("timed out creating native Windows key target")
	}

	var stopOnce sync.Once
	stop := func() {
		t.Helper()
		stopOnce.Do(func() {
			if posted := win.PostMessage(window.handle, win.WM_CLOSE, 0, 0); posted == 0 {
				t.Errorf("close native Windows key target: %v", nativeWindowsCallError("PostMessage"))
				return
			}
			select {
			case stopErr := <-stopped:
				if stopErr != nil {
					t.Errorf("stop native Windows key target: %v", stopErr)
				}
			case <-time.After(nativeWindowsKeyMessageTimeout):
				t.Error("timed out stopping native Windows key target")
			}
		})
	}
	t.Cleanup(stop)
	return messages, stop
}

func nativeWindowsCallError(operation string) error {
	return nativeWindowsCallResultError(operation, syscall.GetLastError())
}

func nativeWindowsCallResultError(operation string, callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return fmt.Errorf("%s failed without Win32 error information", operation)
	}
	return fmt.Errorf("%s failed: %w", operation, callErr)
}
