//go:build !darwin && !linux && !windows

package accessibility

import "context"

func probe(context.Context) Capability {
	return Capability{
		Reason: "native accessibility inspection is not implemented on this platform",
		Notes:  "use a platform adapter that preserves the bounded semantic observation contract",
	}
}

func inspect(context.Context, Target, Limits) (Snapshot, error) {
	return Snapshot{}, ErrUnsupported
}

func act(context.Context, ActionRequest) (ActionResult, error) {
	return ActionResult{}, ErrUnsupported
}
