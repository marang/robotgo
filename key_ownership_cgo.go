//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	inputportal "github.com/marang/robotgo/input/portal"
)

type keyboardHoldID struct {
	keysym int32
	flags  uint32
	pid    int
}

type keyboardHold struct {
	backend          persistentInputBackend
	server           DisplayServer
	portalGeneration uint64
	portalMain       int32
	portalModifiers  []int32
}

type portalKeyboardRefID struct {
	generation uint64
	keysym     int32
}

var (
	keyboardHolds         = make(map[keyboardHoldID]keyboardHold)
	portalKeyboardKeyRefs = make(map[portalKeyboardRefID]uint)
)

func keyboardHoldIdentity(key string, flags uint32, pid int) (keyboardHoldID, error) {
	keysym, _, err := portalKeysymsPure(key, nil)
	if err != nil {
		return keyboardHoldID{}, err
	}
	return keyboardHoldID{keysym: keysym, flags: flags, pid: pid}, nil
}

func clearPortalKeyboardStateLocked() {
	clear(portalKeyboardKeyRefs)
	for id, hold := range keyboardHolds {
		if hold.backend == persistentInputBackendPortal {
			delete(keyboardHolds, id)
		}
	}
}

func clearNativeKeyboardStateLocked(server DisplayServer) {
	for id, hold := range keyboardHolds {
		matches := hold.server == server ||
			server == DisplayServerX11 && hold.server != DisplayServerWayland
		if hold.backend == persistentInputBackendNative && matches {
			delete(keyboardHolds, id)
		}
	}
}

func clearPortalKeyboardGenerationLocked(generation uint64) {
	for id := range portalKeyboardKeyRefs {
		if id.generation == generation {
			delete(portalKeyboardKeyRefs, id)
		}
	}
	for id, hold := range keyboardHolds {
		if hold.backend == persistentInputBackendPortal &&
			hold.portalGeneration == generation {
			delete(keyboardHolds, id)
		}
	}
}

func releasePortalKeyboardRef(
	session remoteDesktopInputSession, generation uint64, keysym int32,
) error {
	id := portalKeyboardRefID{generation: generation, keysym: keysym}
	owners := portalKeyboardKeyRefs[id]
	if owners == 0 {
		return ErrInputOwnership
	}
	if owners > 1 {
		portalKeyboardKeyRefs[id] = owners - 1
		return nil
	}
	if err := remoteDesktopEvent(func(ctx context.Context) error {
		return session.KeyboardKeysym(ctx, keysym, false)
	}); err != nil {
		return err
	}
	delete(portalKeyboardKeyRefs, id)
	return nil
}

func pressPortalKeyboardHold(
	session remoteDesktopInputSession,
	generation uint64,
	mainKey int32,
	modifiers []int32,
) (keyboardHold, error) {
	hold := keyboardHold{
		backend:          persistentInputBackendPortal,
		server:           DisplayServerWayland,
		portalGeneration: generation,
		portalMain:       mainKey,
		portalModifiers:  make([]int32, 0, len(modifiers)),
	}
	mainID := portalKeyboardRefID{generation: generation, keysym: mainKey}
	if portalKeyboardKeyRefs[mainID] != 0 {
		return keyboardHold{}, ErrInputOwnership
	}

	seen := make(map[int32]struct{}, len(modifiers))
	for _, modifier := range modifiers {
		if modifier == mainKey {
			continue
		}
		if _, duplicate := seen[modifier]; duplicate {
			continue
		}
		seen[modifier] = struct{}{}
		id := portalKeyboardRefID{generation: generation, keysym: modifier}
		if portalKeyboardKeyRefs[id] == 0 {
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, modifier, true)
			}); err != nil {
				var rollbackErr error
				for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
					rollbackErr = errors.Join(rollbackErr, releasePortalKeyboardRef(
						session, generation, hold.portalModifiers[index],
					))
				}
				return keyboardHold{}, errors.Join(err, rollbackErr)
			}
		}
		portalKeyboardKeyRefs[id]++
		hold.portalModifiers = append(hold.portalModifiers, modifier)
	}

	if err := remoteDesktopEvent(func(ctx context.Context) error {
		return session.KeyboardKeysym(ctx, mainKey, true)
	}); err != nil {
		var rollbackErr error
		for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, releasePortalKeyboardRef(
				session, generation, hold.portalModifiers[index],
			))
		}
		return keyboardHold{}, errors.Join(err, rollbackErr)
	}
	portalKeyboardKeyRefs[mainID] = 1
	return hold, nil
}

func releasePortalKeyboardHold(
	session remoteDesktopInputSession, hold keyboardHold,
) error {
	firstErr := releasePortalKeyboardRef(
		session, hold.portalGeneration, hold.portalMain,
	)
	for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
		firstErr = errors.Join(firstErr, releasePortalKeyboardRef(
			session, hold.portalGeneration, hold.portalModifiers[index],
		))
	}
	return firstErr
}

func portalKeyboardPayload(key string, modifiers []string, pid int) (int32, []int32, error) {
	if pid != 0 {
		return 0, nil, fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
	}
	return portalKeysymsPure(key, modifiers)
}

func tryPortalKeyTap(
	server DisplayServer, key string, modifiers []string, pid int,
) (bool, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, nil
	}
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			mainKey, portalModifiers, err := portalKeyboardPayload(key, modifiers, pid)
			if err != nil {
				return err
			}
			hold, err := pressPortalKeyboardHold(
				session, generation, mainKey, portalModifiers,
			)
			if err != nil {
				return err
			}
			return releasePortalKeyboardHold(session, hold)
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(generation)
		return true, errors.Join(err, closeErr)
	}
	return used, err
}

func tryPortalKeyDown(
	server DisplayServer, key string, modifiers []string, pid int,
) (bool, keyboardHold, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, keyboardHold{}, nil
	}
	var hold keyboardHold
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			mainKey, portalModifiers, err := portalKeyboardPayload(key, modifiers, pid)
			if err != nil {
				return err
			}
			hold, err = pressPortalKeyboardHold(
				session, generation, mainKey, portalModifiers,
			)
			return err
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(generation)
		return true, keyboardHold{}, errors.Join(err, closeErr)
	}
	return used, hold, err
}

func tryPortalKeyUp(hold keyboardHold) (bool, error) {
	expected := hold.portalGeneration
	used, currentGeneration, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, &expected,
		func(session remoteDesktopInputSession, _ uint64) error {
			return releasePortalKeyboardHold(session, hold)
		},
	)
	if errors.Is(err, ErrInputOwnership) || errors.Is(err, inputportal.ErrClosed) || !used {
		clearPortalKeyboardGenerationLocked(hold.portalGeneration)
		return used, errors.Join(ErrInputOwnership, err)
	}
	if err != nil {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(currentGeneration)
		return true, errors.Join(err, closeErr)
	}
	return true, nil
}
