//go:build windows && !cgo && windowsintegration

package robotgo

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/tailscale/win"
)

const envRequireWindowsWindowIntegration = "ROBOTGO_REQUIRE_WINDOWS_WINDOW_INTEGRATION"

const (
	windowsIntegrationFocusEditMessage      = win.WM_USER + 1
	windowsIntegrationQueryStatusMessage    = win.WM_USER + 2
	windowsIntegrationQueryFocusLossMessage = win.WM_USER + 3

	windowsIntegrationConditionTimeout = 3 * time.Second
	windowsIntegrationMessageTimeout   = 3 * time.Second
	windowsIntegrationAbortIfHung      = 0x0002
)

var windowsIntegrationSendMessageTimeout = syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW")

func TestPureGoWindowsWindowRuntime(t *testing.T) {
	if os.Getenv(envRequireWindowsWindowIntegration) != "1" {
		t.Skip("set " + envRequireWindowsWindowIntegration + "=1 to exercise a real Windows desktop window and the text clipboard")
	}

	handle, editHandle, stopped := startWindowsIntegrationWindow(t)
	pid := os.Getpid()

	capability := GetRuntimeCapabilities().Window
	if !capability.Available || capability.Backend != featureBackendPureGoWindows {
		t.Fatalf("window capability = %+v", capability)
	}

	dpi := GetDPI(handle)
	wantScale := float64(dpi) / 96
	if wantScale == 0 {
		wantScale = 1
	}
	if scale := SysScale(int(handle)); math.Abs(scale-wantScale) > 1e-9 {
		t.Fatalf("SysScale(handle) = %v, want %v from %d DPI", scale, wantScale, dpi)
	}
	if scale := SysScale(-2); !(scale > 0) {
		t.Fatalf("SysScale(desktop) = %v, want positive factor", scale)
	}
	if dpi := ScaleX(); dpi <= 0 {
		t.Fatalf("ScaleX() = %d, want positive Windows DPI", dpi)
	}
	screenWidth, screenHeight := GetScreenSize()
	scaleWidth, scaleHeight := GetScaleSize()
	if scaleWidth != screenWidth || scaleHeight != screenHeight {
		t.Fatalf(
			"GetScaleSize() = %dx%d, GetScreenSize() = %dx%d; Pure-Go bounds are already physical pixels",
			scaleWidth, scaleHeight, screenWidth, screenHeight,
		)
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

	previousClipboard, clipboardErr := ReadAll()
	hadReadableClipboard := clipboardErr == nil
	if clipboardErr != nil {
		previousClipboard = ""
	}
	t.Cleanup(func() {
		if err := WriteAll(previousClipboard); err != nil {
			t.Errorf("restore text clipboard: %v", err)
			return
		}
		if !hadReadableClipboard {
			return
		}
		restoredClipboard, err := ReadAll()
		if err != nil {
			t.Errorf("verify restored text clipboard: %v", err)
			return
		}
		if restoredClipboard != previousClipboard {
			t.Error("restored text clipboard does not match its previous readable value")
		}
	})
	const pasteText = "RobotGo Pure-Go paste ✓"
	exerciseWindowsPaste(t, handle, editHandle, pasteText)

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

func startWindowsIntegrationWindow(t *testing.T) (win.HWND, win.HWND, <-chan struct{}) {
	t.Helper()
	type createdWindows struct {
		window win.HWND
		edit   win.HWND
	}
	created := make(chan createdWindows, 1)
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
		var editHandle win.HWND
		var focusLossEvents uintptr
		windowProc := syscall.NewCallback(func(handle uintptr, message uint32, wParam, lParam uintptr) uintptr {
			switch message {
			case windowsIntegrationFocusEditMessage:
				win.SetFocus(editHandle)
				return uintptr(win.GetFocus())
			case windowsIntegrationQueryStatusMessage:
				var status uintptr
				if win.GetForegroundWindow() == win.HWND(handle) {
					status |= windowsIntegrationStatusForeground
				}
				if win.GetFocus() == editHandle {
					status |= windowsIntegrationStatusEditFocused
				}
				return status
			case windowsIntegrationQueryFocusLossMessage:
				return focusLossEvents
			case win.WM_ACTIVATE:
				if win.LOWORD(uint32(wParam)) == uint16(win.WA_INACTIVE) {
					focusLossEvents++
				}
			case win.WM_COMMAND:
				if win.HWND(lParam) == editHandle &&
					win.HIWORD(uint32(wParam)) == uint16(win.EN_KILLFOCUS) {
					focusLossEvents++
				}
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
		editClass, err := syscall.UTF16PtrFromString("EDIT")
		if err != nil {
			failed <- err
			return
		}
		editHandle = win.CreateWindowEx(
			win.WS_EX_CLIENTEDGE, editClass, nil,
			win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.ES_AUTOHSCROLL,
			20, 20, 360, 32, handle, 0, instance, nil,
		)
		if editHandle == 0 {
			failed <- errorsFromWindowsCall("CreateWindowEx EDIT")
			return
		}
		win.ShowWindow(handle, win.SW_SHOW)
		created <- createdWindows{window: handle, edit: editHandle}

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
	case windows := <-created:
		t.Cleanup(func() {
			if win.GetWindowThreadProcessId(windows.window, nil) != 0 {
				win.PostMessage(windows.window, win.WM_CLOSE, 0, 0)
			}
		})
		return windows.window, windows.edit, stopped
	case err := <-failed:
		t.Fatalf("create integration window: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out creating integration window")
	}
	return 0, 0, stopped
}

func waitForWindowsCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	if waitForWindowsConditionFor(windowsIntegrationConditionTimeout, condition) {
		return
	}
	t.Fatal("timed out waiting for " + description)
}

func waitForWindowsConditionFor(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return condition()
		}
		delay := 20 * time.Millisecond
		if remaining < delay {
			delay = remaining
		}
		time.Sleep(delay)
	}
}

func exerciseWindowsPaste(t *testing.T, handle, editHandle win.HWND, text string) {
	t.Helper()
	first, err := runWindowsPasteAttempt(t, handle, editHandle, text)
	if err != nil {
		t.Fatalf("initial PasteStr attempt: %v", err)
	}
	if first.delivered {
		return
	}
	if !first.clipboardPublished {
		t.Fatal("PasteStr returned success but its text was not present on the clipboard")
	}
	if !windowsPasteFocusLossObserved(first.status, first.focusLossEvents, first.focusLossBaseline) {
		t.Fatal("PasteStr published the clipboard text and retained foreground/edit focus, but Ctrl+V was not delivered")
	}

	t.Logf(
		"retrying PasteStr once after observed focus loss: foreground=%t edit_focused=%t focus_loss_events=%d",
		first.status&windowsIntegrationStatusForeground != 0,
		first.status&windowsIntegrationStatusEditFocused != 0,
		windowsPasteFocusLossDelta(first.focusLossEvents, first.focusLossBaseline),
	)
	empty, err := syscall.UTF16PtrFromString("")
	if err != nil {
		t.Fatalf("encode empty edit text: %v", err)
	}
	if err := win.SetWindowText(editHandle, empty); err != nil {
		t.Fatalf("clear integration edit before focus-loss recovery: %v", err)
	}

	retry, err := runWindowsPasteAttempt(t, handle, editHandle, text)
	if err != nil {
		t.Fatalf("PasteStr after focus-loss recovery: %v", err)
	}
	if retry.delivered {
		return
	}

	t.Fatalf(
		"PasteStr delivery failed after one focus-loss recovery: clipboard_published=%t foreground=%t edit_focused=%t focus_loss_events=%d",
		retry.clipboardPublished,
		retry.status&windowsIntegrationStatusForeground != 0,
		retry.status&windowsIntegrationStatusEditFocused != 0,
		windowsPasteFocusLossDelta(retry.focusLossEvents, retry.focusLossBaseline),
	)
}

type windowsPasteAttemptResult struct {
	delivered          bool
	clipboardPublished bool
	status             uintptr
	focusLossEvents    uintptr
	focusLossBaseline  uintptr
}

func runWindowsPasteAttempt(
	t *testing.T,
	handle, editHandle win.HWND,
	text string,
) (windowsPasteAttemptResult, error) {
	t.Helper()
	result := windowsPasteAttemptResult{
		focusLossBaseline: prepareWindowsPasteTarget(t, handle, editHandle),
	}
	if err := PasteStr(text); err != nil {
		return result, err
	}
	result.delivered = waitForWindowsConditionFor(windowsIntegrationConditionTimeout, func() bool {
		return windowsWindowText(editHandle) == text
	})
	if result.delivered {
		return result, nil
	}
	var err error
	result.clipboardPublished, err = windowsClipboardContains(text)
	if err != nil {
		return result, fmt.Errorf("verify clipboard publication: %w", err)
	}
	result.status = queryWindowsIntegrationStatus(t, handle)
	result.focusLossEvents = queryWindowsIntegrationFocusLosses(t, handle)
	return result, nil
}

func prepareWindowsPasteTarget(t *testing.T, handle, editHandle win.HWND) uintptr {
	t.Helper()
	if GetActive() != Handle(handle) {
		if err := SetActiveE(Handle(handle)); err != nil {
			t.Fatalf("activate integration window for paste: %v", err)
		}
	}
	waitForWindowsCondition(t, "integration window to become foreground for paste", func() bool {
		return GetActive() == Handle(handle)
	})
	focused := sendWindowsIntegrationMessage(t, handle, windowsIntegrationFocusEditMessage)
	if focused != uintptr(editHandle) {
		t.Fatalf("focus integration edit for paste returned %#x, want %#x", focused, editHandle)
	}
	if status := queryWindowsIntegrationStatus(t, handle); status != windowsIntegrationStatusReady {
		t.Fatalf(
			"integration paste target is not ready: foreground=%t edit_focused=%t",
			status&windowsIntegrationStatusForeground != 0,
			status&windowsIntegrationStatusEditFocused != 0,
		)
	}
	return queryWindowsIntegrationFocusLosses(t, handle)
}

func queryWindowsIntegrationStatus(t *testing.T, handle win.HWND) uintptr {
	t.Helper()
	return sendWindowsIntegrationMessage(t, handle, windowsIntegrationQueryStatusMessage)
}

func queryWindowsIntegrationFocusLosses(t *testing.T, handle win.HWND) uintptr {
	t.Helper()
	return sendWindowsIntegrationMessage(t, handle, windowsIntegrationQueryFocusLossMessage)
}

func sendWindowsIntegrationMessage(t *testing.T, handle win.HWND, message uint32) uintptr {
	t.Helper()
	var result uintptr
	sent, _, callErr := windowsIntegrationSendMessageTimeout.Call(
		uintptr(handle),
		uintptr(message),
		0,
		0,
		windowsIntegrationAbortIfHung,
		uintptr(windowsIntegrationMessageTimeout/time.Millisecond),
		uintptr(unsafe.Pointer(&result)),
	)
	runtime.KeepAlive(&result)
	if sent != 0 {
		return result
	}
	if callErr == nil || callErr == syscall.Errno(0) {
		t.Fatalf("SendMessageTimeoutW(%#x) failed or timed out without Win32 error information", message)
	}
	t.Fatalf("SendMessageTimeoutW(%#x) failed or timed out: %v", message, callErr)
	return 0
}

func windowsClipboardContains(want string) (bool, error) {
	got, err := ReadAll()
	if err != nil {
		return false, err
	}
	return got == want, nil
}

func errorsFromWindowsCall(operation string) error {
	err := syscall.GetLastError()
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s failed: %w", operation, err)
}

func windowsWindowText(handle win.HWND) string {
	length := win.GetWindowTextLength(handle)
	buffer := make([]uint16, length+1)
	if len(buffer) == 0 {
		return ""
	}
	read := win.GetWindowText(handle, &buffer[0], int32(len(buffer)))
	if read <= 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:read])
}
