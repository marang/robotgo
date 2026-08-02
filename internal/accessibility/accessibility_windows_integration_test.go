//go:build windows && windowsintegration

package accessibility

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsUIAIntegrationEnv = "ROBOTGO_REQUIRE_WINDOWS_ACCESSIBILITY_INTEGRATION"
	windowsFixtureTitle      = "RobotGo UIA self-owned fixture"
	windowsFixtureVisible    = "fixture-visible-value"
	windowsFixtureUpdated    = "fixture-updated-value"
	windowsFixtureSecret     = "fixture-password-secret"

	windowStyleOverlapped = 0x00cf0000
	windowStyleChild      = 0x40000000
	windowStyleVisible    = 0x10000000
	editStylePassword     = 0x20
	showWindowNormal      = 1
	messageClose          = 0x0010
	messageQuit           = 0x0012
	useDefaultPosition    = 0x80000000
)

var (
	windowsFixtureUser32          = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW           = windowsFixtureUser32.NewProc("CreateWindowExW")
	procDestroyWindow             = windowsFixtureUser32.NewProc("DestroyWindow")
	procShowWindow                = windowsFixtureUser32.NewProc("ShowWindow")
	procUpdateWindow              = windowsFixtureUser32.NewProc("UpdateWindow")
	procGetMessageW               = windowsFixtureUser32.NewProc("GetMessageW")
	procTranslateMessage          = windowsFixtureUser32.NewProc("TranslateMessage")
	procDispatchMessageW          = windowsFixtureUser32.NewProc("DispatchMessageW")
	procPostMessageW              = windowsFixtureUser32.NewProc("PostMessageW")
	procPostThreadMessageW        = windowsFixtureUser32.NewProc("PostThreadMessageW")
	windowsFixtureKernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procFixtureGetCurrentThreadID = windowsFixtureKernel32.NewProc("GetCurrentThreadId")
)

type windowsFixtureMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   struct{ X, Y int32 }
	Private uint32
}

type windowsFixture struct {
	handle   uintptr
	threadID uint32
	done     chan error
}

func TestWindowsUIAInspectsAndActsOnlyOnSelfOwnedBoundedFixture(t *testing.T) {
	if os.Getenv(windowsUIAIntegrationEnv) != "1" {
		t.Skip("set " + windowsUIAIntegrationEnv + "=1 on a disposable Windows desktop")
	}
	fixture := startWindowsUIAFixture(t)
	t.Cleanup(func() { fixture.close(t) })
	if fixture.handle > uintptr(^uint(0)>>1) {
		t.Fatal("fixture HWND does not fit RobotGo's public integer target")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	snapshot := waitForWindowsUIAFixtureSnapshot(t, ctx, fixture.handle)
	defer clearSnapshot(&snapshot)

	var foundButton, foundInput, foundPassword bool
	var inputNode *Node
	for _, node := range snapshot.Nodes {
		if node.Name == "Save" && node.Role == "button" {
			foundButton = true
		}
		if node.Role == "textbox" && node.Value == windowsFixtureVisible {
			foundInput = true
			copy := node
			inputNode = &copy
		}
		if node.Role == "password" {
			foundPassword = true
			if !node.Sensitive || node.Name != "" || node.Description != "" || node.Value != "" {
				t.Fatalf("password node was not fully redacted: %+v", node)
			}
		}
		if node.Name == windowsFixtureSecret || node.Description == windowsFixtureSecret || node.Value == windowsFixtureSecret {
			t.Fatal("password fixture text crossed the semantic boundary")
		}
	}
	if !foundButton || !foundInput || !foundPassword {
		t.Fatalf("fixture semantics missing: button=%t input=%t password=%t nodes=%+v",
			foundButton, foundInput, foundPassword, snapshot.Nodes)
	}
	if inputNode == nil || inputNode.Bounds == nil {
		t.Fatal("editable fixture node lacks an actionable semantic identity")
	}
	action, err := Act(ctx, ActionRequest{
		Target:    Target{NativeWindowHandle: int(fixture.handle), ExpectedTitle: windowsFixtureTitle},
		Reference: inputNode.Reference, Action: "set-value", Value: windowsFixtureUpdated,
		Expected: ElementExpectation{
			Role: inputNode.Role, Name: inputNode.Name, Sensitive: inputNode.Sensitive,
			States: inputNode.States, Bounds: inputNode.Bounds, Actions: inputNode.Actions,
		},
	})
	if err != nil || !action.Dispatched {
		t.Fatalf("self-owned UIA set-value = %+v, %v", action, err)
	}
	updated, err := Inspect(ctx, Target{
		NativeWindowHandle: int(fixture.handle), ExpectedTitle: windowsFixtureTitle,
	}, Limits{
		MaxElements: 64, MaxDepth: 8, MaxStringBytes: 4096,
		MaxReferenceBytes: 256, MaxTotalReferenceBytes: 16 * 1024,
		AllowedRoles: map[string]bool{"textbox": true}, ReadName: true, ReadValue: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clearSnapshot(&updated)
	foundUpdated := false
	for _, node := range updated.Nodes {
		foundUpdated = foundUpdated || node.Role == "textbox" && node.Value == windowsFixtureUpdated
	}
	if !foundUpdated {
		t.Fatalf("UIA set-value was not observable: %+v", updated.Nodes)
	}
}

func waitForWindowsUIAFixtureSnapshot(t *testing.T, ctx context.Context, handle uintptr) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := Inspect(ctx, Target{
			NativeWindowHandle: int(handle),
			ExpectedTitle:      windowsFixtureTitle,
		}, Limits{
			MaxElements: 64, MaxDepth: 8, MaxStringBytes: 4096,
			MaxReferenceBytes: 256, MaxTotalReferenceBytes: 16 * 1024,
			AllowedRoles: map[string]bool{
				"window": true, "group": true, "generic": true, "label": true,
				"button": true, "textbox": true, "password": true,
			},
			ReadName: true, ReadDescription: true, ReadValue: true,
			ReadStates: true, ReadBounds: true, ReadFocus: true, ReadActions: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range snapshot.Nodes {
			if node.Role == "textbox" && node.Value == windowsFixtureVisible && node.Bounds != nil &&
				slices.Contains(node.States, "enabled") && slices.Contains(node.Actions, "set-value") {
				return snapshot
			}
		}
		clearSnapshot(&snapshot)
		if time.Now().After(deadline) {
			t.Fatal("editable fixture did not become actionable before the bounded deadline")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func startWindowsUIAFixture(t *testing.T) windowsFixture {
	t.Helper()
	type readyResult struct {
		fixture windowsFixture
		err     error
	}
	ready := make(chan readyResult, 1)
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		threadID, _, _ := procFixtureGetCurrentThreadID.Call()
		handle, err := createFixtureWindow()
		if err != nil {
			ready <- readyResult{err: err}
			return
		}
		ready <- readyResult{fixture: windowsFixture{handle: handle, threadID: uint32(threadID), done: done}}
		var message windowsFixtureMessage
		for {
			result, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(result) == -1 {
				done <- windowsFixtureCallError("GetMessageW", callErr)
				return
			}
			if result == 0 {
				done <- nil
				return
			}
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	result := <-ready
	if result.err != nil {
		t.Fatal(result.err)
	}
	return result.fixture
}

func createFixtureWindow() (uintptr, error) {
	staticClass, _ := windows.UTF16PtrFromString("STATIC")
	editClass, _ := windows.UTF16PtrFromString("EDIT")
	buttonClass, _ := windows.UTF16PtrFromString("BUTTON")
	title, _ := windows.UTF16PtrFromString(windowsFixtureTitle)
	handle, _, callErr := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(title)),
		windowStyleOverlapped|windowStyleVisible, useDefaultPosition, useDefaultPosition, 420, 240,
		0, 0, 0, 0,
	)
	if handle == 0 {
		return 0, windowsFixtureCallError("CreateWindowExW(top-level)", callErr)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_, _, _ = procDestroyWindow.Call(handle)
		}
	}()
	for _, child := range []struct {
		class *uint16
		text  string
		style uintptr
		x, y  uintptr
		w, h  uintptr
	}{
		{editClass, windowsFixtureVisible, windowStyleChild | windowStyleVisible, 20, 20, 240, 28},
		{editClass, windowsFixtureSecret, windowStyleChild | windowStyleVisible | editStylePassword, 20, 60, 240, 28},
		{buttonClass, "Save", windowStyleChild | windowStyleVisible, 20, 110, 100, 32},
	} {
		text, _ := windows.UTF16PtrFromString(child.text)
		childHandle, _, childErr := procCreateWindowExW.Call(
			0, uintptr(unsafe.Pointer(child.class)), uintptr(unsafe.Pointer(text)), child.style,
			child.x, child.y, child.w, child.h, handle, 0, 0, 0,
		)
		if childHandle == 0 {
			return 0, windowsFixtureCallError("CreateWindowExW(child)", childErr)
		}
	}
	_, _, _ = procShowWindow.Call(handle, showWindowNormal)
	updated, _, updateErr := procUpdateWindow.Call(handle)
	if updated == 0 {
		return 0, windowsFixtureCallError("UpdateWindow", updateErr)
	}
	cleanup = false
	return handle, nil
}

func (fixture windowsFixture) close(t *testing.T) {
	t.Helper()
	if fixture.handle == 0 || fixture.threadID == 0 {
		return
	}
	if posted, _, callErr := procPostMessageW.Call(fixture.handle, messageClose, 0, 0); posted == 0 {
		t.Errorf("close fixture: %v", windowsFixtureCallError("PostMessageW", callErr))
	}
	if posted, _, callErr := procPostThreadMessageW.Call(uintptr(fixture.threadID), messageQuit, 0, 0); posted == 0 {
		t.Errorf("stop fixture loop: %v", windowsFixtureCallError("PostThreadMessageW", callErr))
	}
	select {
	case err := <-fixture.done:
		if err != nil {
			t.Errorf("fixture loop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("fixture message loop did not stop")
	}
}

func windowsFixtureCallError(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s failed: %w", operation, callErr)
}
