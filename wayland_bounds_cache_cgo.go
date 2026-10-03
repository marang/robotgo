//go:build cgo

package robotgo

import (
	"context"
	"os/exec"
	"sync"
	"time"

	commandpkg "github.com/marang/robotgo/internal/command"
)

var (
	waylandBoundsProbeMu sync.Mutex
	waylandBoundsMu      sync.Mutex
	waylandBoundsCached  Rect
	waylandPrimaryCached Rect
	waylandBoundsValid   bool
	waylandBoundsProbed  bool
	waylandBoundsAt      time.Time
	waylandBoundsNow     = time.Now
)

const (
	waylandBoundsSuccessTTL = 2 * time.Second
	waylandBoundsFailureTTL = 250 * time.Millisecond
)

// InvalidateScreenBoundsCache forces the next Wayland fallback bounds query to
// re-read compositor output geometry.
func InvalidateScreenBoundsCache() {
	waylandBoundsProbeMu.Lock()
	defer waylandBoundsProbeMu.Unlock()
	waylandBoundsMu.Lock()
	waylandBoundsCached = Rect{}
	waylandPrimaryCached = Rect{}
	waylandBoundsValid = false
	waylandBoundsProbed = false
	waylandBoundsAt = time.Time{}
	waylandBoundsMu.Unlock()
}

func waylandScreenBoundsFallback() (Rect, bool) {
	return waylandBoundsFallback(false)
}

func waylandPrimaryBoundsFallback() (Rect, bool) {
	return waylandBoundsFallback(true)
}

func waylandBoundsFallback(primary bool) (Rect, bool) {
	if !isWaylandSession() {
		return Rect{}, false
	}
	waylandBoundsProbeMu.Lock()
	defer waylandBoundsProbeMu.Unlock()

	waylandBoundsMu.Lock()
	if waylandBoundsProbed {
		ttl := waylandBoundsFailureTTL
		if waylandBoundsValid {
			ttl = waylandBoundsSuccessTTL
		}
		if waylandBoundsNow().Sub(waylandBoundsAt) < ttl {
			rect := waylandBoundsCached
			if primary {
				rect = waylandPrimaryCached
			}
			ok := waylandBoundsValid
			waylandBoundsMu.Unlock()
			return rect, ok
		}
	}
	waylandBoundsMu.Unlock()

	path, err := exec.LookPath(cmdWaylandInfo)
	if err != nil {
		waylandBoundsMu.Lock()
		waylandBoundsProbed = true
		waylandBoundsValid = false
		waylandBoundsAt = waylandBoundsNow()
		waylandBoundsMu.Unlock()
		return Rect{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := commandpkg.Output(ctx, path)
	if err != nil {
		waylandBoundsMu.Lock()
		waylandBoundsProbed = true
		waylandBoundsValid = false
		waylandBoundsAt = waylandBoundsNow()
		waylandBoundsMu.Unlock()
		return Rect{}, false
	}

	rect, primaryRect, ok := parseWaylandInfoGeometry(string(out))
	waylandBoundsMu.Lock()
	waylandBoundsProbed = true
	waylandBoundsValid = ok
	waylandBoundsAt = waylandBoundsNow()
	if ok {
		waylandBoundsCached = rect
		waylandPrimaryCached = primaryRect
	}
	waylandBoundsMu.Unlock()

	if primary {
		return primaryRect, ok
	}
	return rect, ok
}
