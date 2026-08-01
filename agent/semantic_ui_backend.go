package agent

import (
	"context"
	"errors"

	robotgo "github.com/marang/robotgo"
	"github.com/marang/robotgo/internal/accessibility"
)

func inspectAccessibilityUI(
	ctx context.Context,
	target accessibility.Target,
	limits uiBackendLimits,
) (uiBackendSnapshot, error) {
	allowedRoles := make(map[string]bool, len(limits.AllowedRoles))
	for role := range limits.AllowedRoles {
		allowedRoles[string(role)] = true
	}
	snapshot, err := accessibility.Inspect(ctx, target, accessibility.Limits{
		MaxElements: limits.MaxElements, MaxDepth: limits.MaxDepth,
		MaxStringBytes:         limits.MaxStringBytes,
		MaxReferenceBytes:      limits.MaxReferenceBytes,
		MaxTotalReferenceBytes: limits.MaxTotalReferenceBytes,
		AllowedRoles:           allowedRoles,
		ReadName:               limits.ReadName,
		ReadDescription:        limits.ReadDescription,
		ReadValue:              limits.ReadValue,
		ReadStates:             limits.ReadStates,
		ReadBounds:             limits.ReadBounds,
		ReadFocus:              limits.ReadFocus,
		ReadActions:            limits.ReadActions,
	})
	if err != nil {
		return uiBackendSnapshot{}, agentAccessibilityError(err)
	}
	result := uiBackendSnapshot{
		Backend: snapshot.Backend, Truncated: snapshot.Truncated,
		Nodes: make([]uiBackendNode, 0, len(snapshot.Nodes)),
	}
	for index := range snapshot.Nodes {
		node := &snapshot.Nodes[index]
		converted := uiBackendNode{
			StableID: node.Reference, Parent: node.Parent, Depth: node.Depth,
			Role: UIRole(node.Role), Name: node.Name, Description: node.Description,
			Value: node.Value, Sensitive: node.Sensitive, Hidden: node.Hidden,
			Offscreen: node.Offscreen, Focused: node.Focused,
		}
		node.Reference = nil
		for _, state := range node.States {
			converted.States = append(converted.States, UIState(state))
		}
		for _, action := range node.Actions {
			converted.Actions = append(converted.Actions, UIAction(action))
		}
		if node.Bounds != nil {
			converted.Bounds = &UIBounds{
				X: node.Bounds.X, Y: node.Bounds.Y,
				Width: node.Bounds.Width, Height: node.Bounds.Height,
			}
		}
		result.Nodes = append(result.Nodes, converted)
	}
	return result, nil
}

func agentAccessibilityError(err error) error {
	switch {
	case errors.Is(err, accessibility.ErrStaleTarget):
		return ErrStaleTarget
	case errors.Is(err, accessibility.ErrPermissionDenied):
		return robotgo.ErrPermissionDenied
	case errors.Is(err, accessibility.ErrUnsupported):
		return robotgo.ErrNotSupported
	case errors.Is(err, accessibility.ErrInvalidTree):
		return uiError(ErrorBackendFailure, "accessibility backend returned an invalid tree", err)
	case errors.Is(err, accessibility.ErrUnavailable):
		return uiError(ErrorUnavailable, "semantic UI inspection backend is unavailable", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return uiError(ErrorBackendFailure, "native accessibility inspection failed", err)
	}
}
