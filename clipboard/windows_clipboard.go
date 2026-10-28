package clipboard

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"
)

const windowsClipboardOpenTimeout = time.Second

// windowsClipboardOps keeps the Win32 boundary private. Production and
// hermetic fixtures exercise the same transaction and ownership logic.
type windowsClipboardOps struct {
	open         func(uintptr) error
	close        func() error
	data         func() (uintptr, error)
	size         func(uintptr) (uintptr, error)
	lock         func(uintptr) (unsafe.Pointer, error)
	unlock       func(uintptr) error
	alloc        func(uintptr) (uintptr, error)
	free         func(uintptr) error
	empty        func() error
	set          func(uintptr) error
	createOwner  func() (uintptr, error)
	destroyOwner func(uintptr) error
}

func openWindowsClipboard(ctx context.Context, ops windowsClipboardOps, owner uintptr) error {
	limit := time.Now().Add(windowsClipboardOpenTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := ops.open(owner)
		if err == nil || !time.Now().Before(limit) {
			return err
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func windowsUTF16Units(size uintptr) (int, error) {
	// The byte size, not only the unit count, must fit addressable Go memory.
	if size < 2 || size%2 != 0 || size > uintptr(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("clipboard: invalid UTF-16 allocation size %d", size)
	}
	return int(size / 2), nil
}

func readWindowsClipboard(ctx context.Context, ops windowsClipboardOps) (text string, err error) {
	// Both Open/Close and the SetLastError/GlobalUnlock pair are thread-affine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = openWindowsClipboard(ctx, ops, 0); err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, ops.close())
		if err != nil {
			text = ""
		}
	}()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	h, err := ops.data()
	if err != nil {
		return "", err
	}
	size, err := ops.size(h)
	if err != nil {
		return "", err
	}
	units, err := windowsUTF16Units(size)
	if err != nil {
		return "", err
	}
	p, err := ops.lock(h)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, ops.unlock(h)) }()
	buf := unsafe.Slice((*uint16)(p), units)
	for i, unit := range buf {
		if unit == 0 {
			if err = ctx.Err(); err != nil {
				return "", err
			}
			return string(utf16.Decode(buf[:i])), nil
		}
	}
	return "", errors.New("clipboard: unterminated UTF-16 allocation")
}

func writeWindowsClipboard(ctx context.Context, text string, ops windowsClipboardOps) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if strings.IndexByte(text, 0) >= 0 {
		return errors.New("clipboard: text contains NUL")
	}
	data := append(utf16.Encode([]rune(text)), 0)
	if len(data) > int(^uint(0)>>1)/2 {
		return errors.New("clipboard: UTF-16 text is too large")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Finish fallible conversion/allocation/copy/unlock before destroying old data.
	h, err := ops.alloc(uintptr(len(data)) * 2)
	if err != nil {
		return err
	}
	defer func() {
		if h != 0 {
			err = errors.Join(err, ops.free(h))
		}
	}()
	p, err := ops.lock(h)
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*uint16)(p), len(data)), data)
	if err = ops.unlock(h); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// A NULL owner is valid for reads, but not the documented Empty/Set contract.
	owner, err := ops.createOwner()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, ops.destroyOwner(owner)) }()
	if err = openWindowsClipboard(ctx, ops, owner); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, ops.close()) }()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = ops.empty(); err != nil {
		return err
	}
	// Never retry publication after destructive side effects.
	if err = ops.set(h); err != nil {
		return err
	}
	h = 0 // successful SetClipboardData transfers ownership to the system
	return nil
}
