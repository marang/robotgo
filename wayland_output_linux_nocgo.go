//go:build linux && !cgo

package robotgo

import (
	"context"
	"fmt"
	"image"
	"time"

	"github.com/marang/robotgo/internal/waylandoutput"
)

const (
	pureGoWaylandOutputTimeout = 1500 * time.Millisecond
	pureGoWaylandProbeTimeout  = 500 * time.Millisecond
)

var pureGoWaylandOutputEnumerate = waylandoutput.Enumerate

func pureGoWaylandOutputSnapshot(ctx context.Context) (waylandoutput.Snapshot, error) {
	snapshot, err := pureGoWaylandOutputEnumerate(ctx)
	if err != nil {
		return waylandoutput.Snapshot{}, fmt.Errorf(
			"%w: Pure-Go Wayland output enumeration failed: %v",
			ErrNotSupported,
			err,
		)
	}
	return snapshot, nil
}

func pureGoWaylandOutputs() (waylandoutput.Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pureGoWaylandOutputTimeout)
	defer cancel()
	return pureGoWaylandOutputSnapshot(ctx)
}

func pureGoWaylandDisplayBounds(displayIndex int) (image.Rectangle, error) {
	snapshot, err := pureGoWaylandOutputs()
	if err != nil {
		return image.Rectangle{}, err
	}
	if displayIndex == -1 {
		bounds := make([]waylandOutputBounds, 0, len(snapshot.Outputs))
		for _, output := range snapshot.Outputs {
			bounds = append(bounds, waylandOutputBounds{
				x: output.X,
				y: output.Y,
				w: output.Width,
				h: output.Height,
			})
		}
		aggregate, ok := aggregateWaylandOutputBounds(bounds)
		if !ok {
			return image.Rectangle{}, fmt.Errorf(
				"%w: Pure-Go Wayland output bounds cannot be aggregated safely",
				ErrNotSupported,
			)
		}
		return image.Rect(
			aggregate.X,
			aggregate.Y,
			aggregate.X+aggregate.W,
			aggregate.Y+aggregate.H,
		), nil
	}
	if displayIndex < 0 {
		return image.Rectangle{}, invalidDisplayIndexError(displayIndex)
	}
	if displayIndex >= len(snapshot.Outputs) {
		return image.Rectangle{}, fmt.Errorf(
			"robotgo: display index %d is outside active Wayland output count %d",
			displayIndex,
			len(snapshot.Outputs),
		)
	}
	output := snapshot.Outputs[displayIndex]
	return image.Rect(
		output.X,
		output.Y,
		output.X+output.Width,
		output.Y+output.Height,
	), nil
}

func pureGoWaylandDisplayCount() (int, error) {
	snapshot, err := pureGoWaylandOutputs()
	if err != nil {
		return 0, err
	}
	return len(snapshot.Outputs), nil
}

func pureGoWaylandScreenSizeE() (int, int, error, bool) {
	if selectedDisplayServer() != DisplayServerWayland {
		return 0, 0, nil, false
	}
	displayID := currentDisplayID()
	if displayID < 0 {
		displayID = 0
	}
	bounds, err := pureGoWaylandDisplayBounds(displayID)
	if err != nil {
		return 0, 0, err, true
	}
	return bounds.Dx(), bounds.Dy(), nil, true
}

func pureGoWaylandScreenRectE(displayID ...int) (Rect, error, bool) {
	if selectedDisplayServer() != DisplayServerWayland {
		return Rect{}, nil, false
	}
	id := currentDisplayID()
	if len(displayID) > 0 {
		id = displayID[0]
	}
	bounds, err := pureGoWaylandDisplayBounds(id)
	if err != nil {
		return Rect{}, err, true
	}
	return Rect{
		Point: Point{X: bounds.Min.X, Y: bounds.Min.Y},
		Size:  Size{W: bounds.Dx(), H: bounds.Dy()},
	}, nil, true
}

func pureGoWaylandDisplaysNumE() (int, error, bool) {
	if selectedDisplayServer() != DisplayServerWayland {
		return 0, nil, false
	}
	count, err := pureGoWaylandDisplayCount()
	return count, err, true
}

func pureGoWaylandBoundsCapability() FeatureCapability {
	ctx, cancel := context.WithTimeout(context.Background(), pureGoWaylandProbeTimeout)
	defer cancel()
	snapshot, err := pureGoWaylandOutputSnapshot(ctx)
	if err != nil {
		return FeatureCapability{
			Backend: featureBackendPureGoWaylandOutput,
			Reason:  err.Error(),
			Notes:   "the read-only Wayland probe is bounded and never opens a consent dialog",
		}
	}
	return FeatureCapability{
		Available: true,
		Backend:   featureBackendPureGoWaylandOutput,
		Reason:    "native Pure-Go Wayland output enumeration returned valid logical bounds",
		Notes: fmt.Sprintf(
			"outputs=%d wl_output=%d xdg-output=%d",
			len(snapshot.Outputs),
			snapshot.OutputVersion,
			snapshot.XDGOutputVersion,
		),
	}
}
