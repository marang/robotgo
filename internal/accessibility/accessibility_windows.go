//go:build windows

package accessibility

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/marang/robotgo/internal/windowswindow"
)

func probe(ctx context.Context) Capability {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := boundedWindowsUIAContext(ctx, 3*time.Second)
	defer cancel()
	err := runOnWindowsUIAThread(probeCtx, func() error {
		client, err := newUIAClient(probeCtx)
		if err != nil {
			return err
		}
		client.close()
		return nil
	})
	if err != nil {
		return windowsUIACapabilityError(err)
	}
	return Capability{
		Available: true,
		Backend:   BackendWindowsAutomation,
		Reason:    "Windows UI Automation is available",
		Notes:     "semantic inspection is read-only, process-scoped, bounded, and does not open a consent dialog",
	}
}

func boundedWindowsUIAContext(ctx context.Context, maximum time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= maximum {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, maximum)
}

func inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target.ExpectedTitle == "" || !validUIALimits(limits) ||
		(target.ProcessID > 0) == (target.NativeWindowHandle > 0) {
		return Snapshot{}, ErrInvalidTree
	}
	return runOnWindowsUIAThreadValue(ctx, func() (Snapshot, error) {
		return inspectWindowsUIA(ctx, target, limits)
	})
}

func inspectWindowsUIA(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	windowBackend := windowswindow.NewNative()
	isHandle := target.NativeWindowHandle > 0
	windowTarget := target.ProcessID
	if isHandle {
		windowTarget = target.NativeWindowHandle
	}
	handle, err := windowBackend.Resolve(windowTarget, isHandle)
	if err != nil {
		return Snapshot{}, ErrStaleTarget
	}
	processID, err := windowBackend.PID(handle)
	if err != nil || processID <= 0 {
		return Snapshot{}, ErrStaleTarget
	}
	if target.ProcessID > 0 && processID != target.ProcessID {
		return Snapshot{}, ErrStaleTarget
	}
	title, err := windowBackend.Title(handle)
	if err != nil || title != target.ExpectedTitle {
		return Snapshot{}, ErrStaleTarget
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	client, err := newUIAClient(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer client.close()
	root, err := client.elementFromHandle(ctx, uintptr(handle))
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := buildUIATree(ctx, client.query, root, int32(processID), limits)
	if err != nil {
		return Snapshot{}, err
	}

	// Re-resolve both immutable policy fields after traversal. UIA runtime IDs
	// are opaque and recyclable, so HWND/PID/title validation remains the
	// authoritative observation boundary.
	liveProcessID, pidErr := windowBackend.PID(handle)
	liveTitle, titleErr := windowBackend.Title(handle)
	if pidErr != nil || titleErr != nil || liveProcessID != processID || liveTitle != target.ExpectedTitle {
		clearSnapshot(&snapshot)
		return Snapshot{}, ErrStaleTarget
	}
	return snapshot, nil
}

func windowsUIACapabilityError(err error) Capability {
	if errors.Is(err, ErrPermissionDenied) {
		return Capability{
			Reason:           "Windows denied access to UI Automation",
			Notes:            "run in the interactive user's desktop session with permission to inspect the target application",
			PermissionDenied: true,
		}
	}
	if errors.Is(err, ErrUnsupported) {
		return Capability{
			Reason: "bounded Windows UI Automation is unsupported by this Windows installation",
			Notes:  "Windows UI Automation 2 with configurable connection and transaction timeouts is required",
		}
	}
	return Capability{
		Reason: "Windows UI Automation is unavailable",
		Notes:  "run RobotGo in an interactive Windows desktop session",
	}
}

func runOnWindowsUIAThread(ctx context.Context, operation func() error) error {
	_, err := runOnWindowsUIAThreadValue(ctx, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

func runOnWindowsUIAThreadValue[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := initializeUIAThread(); err != nil {
			var zero T
			completed <- result{value: zero, err: err}
			return
		}
		defer uninitializeUIAThread()
		value, err := operation()
		completed <- result{value: value, err: err}
	}()
	// Do not abandon the worker on cancellation: UIA2 applies a strict native
	// transaction timeout, and waiting here guarantees COM and BSTR/SAFEARRAY
	// cleanup before the API returns.
	outcome := <-completed
	return outcome.value, outcome.err
}
