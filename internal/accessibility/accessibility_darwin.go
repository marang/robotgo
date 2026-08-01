//go:build darwin

package accessibility

import (
	"context"
	"errors"

	"github.com/marang/robotgo/internal/darwinwindow"
)

func probe(ctx context.Context) Capability {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return darwinAccessibilityCapabilityError(err)
	}
	backend := darwinwindow.NewNative()
	err := backend.Ready()
	closeErr := backend.CloseSystem()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return darwinAccessibilityCapabilityError(err)
	}
	return Capability{
		Available: true,
		Backend:   BackendMacOSAccessibility,
		Reason:    "macOS Accessibility is available",
		Notes:     "semantic inspection is read-only, exact-window scoped, bounded, and never opens the Accessibility consent prompt",
	}
}

func inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target.ExpectedTitle == "" ||
		(target.ProcessID > 0) == (target.NativeWindowHandle > 0) {
		return Snapshot{}, ErrInvalidTree
	}
	allowedRoles := make(map[string]bool, len(limits.AllowedRoles))
	for role, allowed := range limits.AllowedRoles {
		allowedRoles[role] = allowed
	}
	native, err := darwinwindow.InspectAccessibility(ctx, darwinwindow.AccessibilityTarget{
		ProcessID: target.ProcessID, CGWindowID: target.NativeWindowHandle,
		ExpectedTitle: target.ExpectedTitle,
	}, darwinwindow.AccessibilityLimits{
		MaxElements: limits.MaxElements, MaxDepth: limits.MaxDepth,
		MaxStringBytes: limits.MaxStringBytes, MaxReferenceBytes: limits.MaxReferenceBytes,
		MaxTotalReferenceBytes: limits.MaxTotalReferenceBytes, AllowedRoles: allowedRoles,
		ReadName: limits.ReadName, ReadDescription: limits.ReadDescription,
		ReadValue: limits.ReadValue, ReadStates: limits.ReadStates,
		ReadBounds: limits.ReadBounds, ReadFocus: limits.ReadFocus,
		ReadActions: limits.ReadActions,
	})
	if err != nil {
		return Snapshot{}, normalizeDarwinAccessibilityError(err)
	}
	snapshot := Snapshot{
		Backend: BackendMacOSAccessibility,
		Nodes:   make([]Node, 0, len(native.Nodes)), Truncated: native.Truncated,
	}
	for index := range native.Nodes {
		node := &native.Nodes[index]
		converted := Node{
			Reference: node.Reference, Parent: node.Parent, Depth: node.Depth,
			Role: node.Role, Name: node.Name, Description: node.Description,
			Value: node.Value, Sensitive: node.Sensitive, Hidden: node.Hidden,
			Offscreen: node.Offscreen, States: node.States, Focused: node.Focused,
			Actions: node.Actions,
		}
		node.Reference = nil
		node.States = nil
		node.Actions = nil
		if node.Bounds != nil {
			converted.Bounds = &Bounds{
				X: node.Bounds.X, Y: node.Bounds.Y,
				Width: node.Bounds.Width, Height: node.Bounds.Height,
			}
		}
		snapshot.Nodes = append(snapshot.Nodes, converted)
	}
	return snapshot, nil
}
func darwinAccessibilityCapabilityError(err error) Capability {
	if errors.Is(err, darwinwindow.ErrPermission) {
		return Capability{
			Reason:           "macOS Accessibility permission is not granted",
			Notes:            "grant the calling application access in System Settings > Privacy & Security > Accessibility; the probe never opens that prompt",
			PermissionDenied: true,
		}
	}
	if errors.Is(err, darwinwindow.ErrUnsupported) {
		return Capability{
			Reason: "bounded macOS Accessibility is unsupported by this installation",
			Notes:  "ApplicationServices, CoreFoundation, and the AX messaging-timeout API are required",
		}
	}
	return Capability{
		Reason: "macOS Accessibility is unavailable",
		Notes:  "run RobotGo in an interactive Aqua desktop session",
	}
}

func normalizeDarwinAccessibilityError(err error) error {
	switch {
	case errors.Is(err, darwinwindow.ErrPermission):
		return ErrPermissionDenied
	case errors.Is(err, darwinwindow.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, darwinwindow.ErrAccessibilityStaleTarget):
		return ErrStaleTarget
	case errors.Is(err, darwinwindow.ErrAccessibilityInvalidTree):
		return ErrInvalidTree
	case errors.Is(err, darwinwindow.ErrAccessibilityUnavailable):
		return ErrUnavailable
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrUnavailable
	}
}
