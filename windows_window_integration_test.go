//go:build windows && !cgo && windowsintegration

package robotgo

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/tailscale/win"
)

const envRequireWindowsWindowIntegration = "ROBOTGO_REQUIRE_WINDOWS_WINDOW_INTEGRATION"

func TestPureGoWindowsWindowRuntime(t *testing.T) {
	if os.Getenv(envRequireWindowsWindowIntegration) != "1" {
		t.Skip("set " + envRequireWindowsWindowIntegration + "=1 to exercise a real Windows desktop window")
	}

	handle, stopped := startWindowsIntegrationWindow(t)
	pid := os.Getpid()

	capability := GetRuntimeCapabilities().Window
	if !capability.Available || capability.Backend != featureBackendPureGoWindows {
		t.Fatalf("window capability = %+v", capability)
	}

	title, err := GetTitleE(int(handle), 1)
	if err != nil || title != "RobotGo Pure-Go window integration" {
		t.Fatalf("GetTitleE(handle) = %q, %v", title, err)
	}
	title, err = GetTitleE(pid)
	if err != nil || title != "RobotGo Pure-Go window integration" {
		t.Fatalf("GetTitleE(pid) = %q, %v", title, err)
	}
	if resolved := GetHWNDByPid(pid); resolved != int(handle) {
		t.Fatalf("GetHWNDByPid(%d) = %#x, want %#x", pid, resolved, handle)
	}
	if resolved := GetHandByPid(pid); resolved != Handle(handle) {
		t.Fatalf("GetHandByPid(%d) = %#x, want %#x", pid, resolved, handle)
	}

	x, y, width, height := GetBounds(int(handle), 1)
	if width <= 0 || height <= 0 {
		t.Fatalf("GetBounds(handle) = (%d, %d, %d, %d)", x, y, width, height)
	}
	clientX, clientY, clientWidth, clientHeight := GetClient(int(handle), 1)
	if clientWidth <= 0 || clientHeight <= 0 || clientWidth > width || clientHeight > height {
		t.Fatalf("GetClient(handle) = (%d, %d, %d, %d), window=(%d, %d, %d, %d)",
			clientX, clientY, clientWidth, clientHeight, x, y, width, height)
	}

	if err := MinWindowE(int(handle), true, 1); err != nil {
		t.Fatalf("MinWindowE(true): %v", err)
	}
	waitForWindowsCondition(t, "window to minimize", func() bool {
		return win.GetWindowLongPtr(handle, win.GWL_STYLE)&uintptr(win.WS_MINIMIZE) != 0
	})
	if err := MinWindowE(int(handle), false, 1); err != nil {
		t.Fatalf("MinWindowE(false): %v", err)
	}
	if err := MaxWindowE(int(handle), true, 1); err != nil {
		t.Fatalf("MaxWindowE(true): %v", err)
	}
	waitForWindowsCondition(t, "window to maximize", func() bool {
		return win.GetWindowLongPtr(handle, win.GWL_STYLE)&uintptr(win.WS_MAXIMIZE) != 0
	})
	if err := MaxWindowE(int(handle), false, 1); err != nil {
		t.Fatalf("MaxWindowE(false): %v", err)
	}

	if err := SetActiveE(Handle(handle)); err != nil {
		t.Fatalf("SetActiveE: %v", err)
	}
	waitForWindowsCondition(t, "window to become foreground", func() bool {
		return GetActive() == Handle(handle)
	})
	if got := GetPid(); got != pid {
		t.Fatalf("GetPid() = %d, want %d", got, pid)
	}
	if minimized, err := IsMinimizedE(); err != nil || minimized {
		t.Fatalf("IsMinimizedE() = %v, %v after restore", minimized, err)
	}
	if maximized, err := IsMaximizedE(); err != nil || maximized {
		t.Fatalf("IsMaximizedE() = %v, %v after restore", maximized, err)
	}
	if err := SetTopMostE(true); err != nil {
		t.Fatalf("SetTopMostE(true): %v", err)
	}
	topMost, err := IsTopMostE()
	if err != nil || !topMost {
		t.Fatalf("IsTopMostE() = %v, %v", topMost, err)
	}
	if err := SetTopMostE(false); err != nil {
		t.Fatalf("SetTopMostE(false): %v", err)
	}

	if err := CloseWindowE(int(handle), 1); err != nil {
		t.Fatalf("CloseWindowE(handle): %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("window did not process WM_CLOSE")
	}
}

func startWindowsIntegrationWindow(t *testing.T) (win.HWND, <-chan struct{}) {
	t.Helper()
	created := make(chan win.HWND, 1)
	failed := make(chan error, 1)
	stopped := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(stopped)

		instance := win.GetModuleHandle(nil)
		className, err := syscall.UTF16PtrFromString(fmt.Sprintf("RobotGoPureGoWindow%d", os.Getpid()))
		if err != nil {
			failed <- err
			return
		}
		title, err := syscall.UTF16PtrFromString("RobotGo Pure-Go window integration")
		if err != nil {
			failed <- err
			return
		}
		windowProc := syscall.NewCallback(func(handle uintptr, message uint32, wParam, lParam uintptr) uintptr {
			switch message {
			case win.WM_DESTROY:
				win.PostQuitMessage(0)
				return 0
			default:
				return win.DefWindowProc(win.HWND(handle), message, wParam, lParam)
			}
		})
		class := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   windowProc,
			HInstance:     instance,
			LpszClassName: className,
		}
		if atom := win.RegisterClassEx(&class); atom == 0 {
			failed <- errorsFromWindowsCall("RegisterClassEx")
			return
		}
		defer win.UnregisterClass(className)

		handle := win.CreateWindowEx(
			0, className, title, win.WS_OVERLAPPEDWINDOW,
			80, 90, 420, 300, 0, 0, instance, nil,
		)
		if handle == 0 {
			failed <- errorsFromWindowsCall("CreateWindowEx")
			return
		}
		win.ShowWindow(handle, win.SW_SHOW)
		created <- handle

		var message win.MSG
		for {
			result := win.GetMessage(&message, 0, 0, 0)
			if result == 0 {
				break
			}
			if int32(result) == -1 {
				failed <- errorsFromWindowsCall("GetMessage")
				return
			}
			win.TranslateMessage(&message)
			win.DispatchMessage(&message)
		}
		runtime.KeepAlive(windowProc)
	}()

	select {
	case handle := <-created:
		t.Cleanup(func() {
			if win.GetWindowThreadProcessId(handle, nil) != 0 {
				win.PostMessage(handle, win.WM_CLOSE, 0, 0)
			}
		})
		return handle, stopped
	case err := <-failed:
		t.Fatalf("create integration window: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out creating integration window")
	}
	return 0, stopped
}

func waitForWindowsCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + description)
}

func errorsFromWindowsCall(operation string) error {
	err := syscall.GetLastError()
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s failed: %w", operation, err)
}
