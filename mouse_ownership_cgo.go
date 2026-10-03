//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

var waylandMouseMu sync.Mutex

type mouseHold struct {
	backend          persistentInputBackend
	server           DisplayServer
	portalGeneration uint64
	portalButton     int32
}

type portalMouseRefID struct {
	generation uint64
	button     int32
}

var (
	mouseHolds            = make(map[string]mouseHold)
	portalMouseButtonRefs = make(map[portalMouseRefID]uint)
)

func canonicalMouseHoldName(name string) string {
	switch name {
	case "", "left":
		return "left"
	case "middle":
		return "center"
	default:
		return name
	}
}

func clearPortalMouseStateLocked() {
	clear(portalMouseButtonRefs)
	for name, hold := range mouseHolds {
		if hold.backend == persistentInputBackendPortal {
			delete(mouseHolds, name)
		}
	}
}

func clearPortalMouseGenerationLocked(generation uint64) {
	for id := range portalMouseButtonRefs {
		if id.generation == generation {
			delete(portalMouseButtonRefs, id)
		}
	}
	for name, hold := range mouseHolds {
		if hold.backend == persistentInputBackendPortal &&
			hold.portalGeneration == generation {
			delete(mouseHolds, name)
		}
	}
}

func releaseNativeMouseHoldsLocked(server DisplayServer) error {
	var releaseErr error
	if runtime.GOOS != "linux" {
		for name, hold := range mouseHolds {
			if hold.backend != persistentInputBackendNative {
				continue
			}
			releaseErr = errors.Join(releaseErr, toggleTrackedNativeMouseHold(
				mouseHolds,
				name,
				false,
				func(down bool) error { return nativeTrackedMouseToggle(name, down) },
			))
		}
		return releaseErr
	}
	if server == DisplayServerX11 {
		releaseErr = nativeReleaseX11MouseHolds()
	}
	for name, hold := range mouseHolds {
		matches := hold.server == server ||
			server == DisplayServerX11 && hold.server != DisplayServerWayland
		if hold.backend != persistentInputBackendNative || !matches {
			continue
		}
		if server == DisplayServerWayland {
			releaseErr = errors.Join(releaseErr, nativeToggleMouse(name, false, "release owned Wayland mouse button"))
		}
		delete(mouseHolds, name)
	}
	return releaseErr
}

func tryPortalMouseDown(server DisplayServer, name string) (bool, mouseHold, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, mouseHold{}, nil
	}
	var hold mouseHold
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DevicePointer, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			button, err := portalPointerButton(name)
			if err != nil {
				return err
			}
			id := portalMouseRefID{generation: generation, button: button}
			if portalMouseButtonRefs[id] != 0 {
				return ErrInputOwnership
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, button, true)
			}); err != nil {
				return err
			}
			portalMouseButtonRefs[id] = 1
			hold = mouseHold{
				backend:          persistentInputBackendPortal,
				server:           DisplayServerWayland,
				portalGeneration: generation,
				portalButton:     button,
			}
			return nil
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalMouseGenerationLocked(generation)
		return true, mouseHold{}, errors.Join(err, closeErr)
	}
	return used, hold, err
}

func tryPortalMouseUp(hold mouseHold) (bool, error) {
	expected := hold.portalGeneration
	used, currentGeneration, err := withRemoteDesktopInputLease(
		inputportal.DevicePointer, &expected,
		func(session remoteDesktopInputSession, generation uint64) error {
			id := portalMouseRefID{
				generation: generation,
				button:     hold.portalButton,
			}
			if portalMouseButtonRefs[id] != 1 {
				return ErrInputOwnership
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, hold.portalButton, false)
			}); err != nil {
				return err
			}
			delete(portalMouseButtonRefs, id)
			return nil
		},
	)
	if errors.Is(err, ErrInputOwnership) || errors.Is(err, inputportal.ErrClosed) || !used {
		clearPortalMouseGenerationLocked(hold.portalGeneration)
		return used, errors.Join(ErrInputOwnership, err)
	}
	if err != nil {
		closeErr := CloseRemoteDesktopInput()
		clearPortalMouseGenerationLocked(currentGeneration)
		return true, errors.Join(err, closeErr)
	}
	return true, nil
}

func toggleTrackedNativeMouseHold(
	holds map[string]mouseHold,
	name string,
	down bool,
	toggle func(bool) error,
) error {
	if down {
		if _, held := holds[name]; held {
			return ErrInputOwnership
		}
		if err := toggle(true); err != nil {
			return err
		}
		holds[name] = mouseHold{backend: persistentInputBackendNative}
		return nil
	}

	hold, held := holds[name]
	if !held || hold.backend != persistentInputBackendNative {
		return ErrInputOwnership
	}
	err := toggle(false)
	if err == nil || errors.Is(err, ErrInputOwnership) {
		delete(holds, name)
	}
	return err
}

func clickTrackedNativeMouseHold(
	holds map[string]mouseHold,
	name string,
	double bool,
	toggle func(bool) error,
	sleep func(time.Duration),
) error {
	clicks := 1
	if double {
		clicks = 2
	}
	for click := 0; click < clicks; click++ {
		if err := toggleTrackedNativeMouseHold(holds, name, true, toggle); err != nil {
			return err
		}
		sleep(5 * time.Millisecond)
		if err := toggleTrackedNativeMouseHold(holds, name, false, toggle); err != nil {
			// An ambiguous release remains in holds so an explicit Up or
			// CloseMainDisplayE can retry it without touching foreign input.
			return errors.Join(ErrInputReleasePending, err)
		}
		if click+1 < clicks {
			sleep(200 * time.Millisecond)
		}
	}
	return nil
}
