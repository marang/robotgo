//go:build linux

package x11window

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/marang/robotgo/internal/windowbackend"
)

const (
	atomActiveWindow      = "_NET_ACTIVE_WINDOW"
	atomCloseWindow       = "_NET_CLOSE_WINDOW"
	atomFrameExtents      = "_NET_FRAME_EXTENTS"
	atomSupported         = "_NET_SUPPORTED"
	atomSupportingWMCheck = "_NET_SUPPORTING_WM_CHECK"
	atomWMName            = "_NET_WM_NAME"
	atomWMPID             = "_NET_WM_PID"
	atomWMState           = "_NET_WM_STATE"
	atomWMStateAbove      = "_NET_WM_STATE_ABOVE"
	atomWMStateHidden     = "_NET_WM_STATE_HIDDEN"
	atomWMStateMaxHorz    = "_NET_WM_STATE_MAXIMIZED_HORZ"
	atomWMStateMaxVert    = "_NET_WM_STATE_MAXIMIZED_VERT"
	atomWMChangeState     = "WM_CHANGE_STATE"
	atomLegacyWMName      = "WM_NAME"
	atomUTF8String        = "UTF8_STRING"
	icccmIconicState      = 3
	maxPropertyBytes      = 1 << 20
	maxFrameExtentPixels  = math.MaxUint16
	maxWindowTreeNodes    = 1 << 16
)

// ErrWindowManagerUnavailable reports that a state/control request has no
// consistent EWMH manager or is not advertised by that manager.
var ErrWindowManagerUnavailable = fmt.Errorf(
	"%w: EWMH window manager unavailable",
	windowbackend.ErrUnsupported,
)

// Config controls X11 connection selection.
type Config struct {
	ResolveDisplay func() (string, error)
}

// NewNative constructs a backend that opens and closes an X11 connection for
// each public operation.
func NewNative(config Config) *Backend {
	return New(&nativeSystem{resolveDisplay: config.ResolveDisplay})
}

type nativeSystem struct {
	resolveDisplay func() (string, error)
}

func (system *nativeSystem) open() (*xgbutil.XUtil, error) {
	if system.resolveDisplay == nil {
		return nil, errors.New("X11 display resolver is nil")
	}
	display, err := system.resolveDisplay()
	if err != nil {
		return nil, err
	}
	if display == "" {
		return nil, errors.New("X11 display resolver returned an empty display")
	}
	xu, err := xgbutil.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("connect to X11 display %q: %w", display, err)
	}
	return xu, nil
}

func (system *nativeSystem) ActiveWindow() (windowbackend.Handle, error) {
	xu, err := system.open()
	if err != nil {
		return 0, err
	}
	defer xu.Conn().Close()

	active, activeErr := ewmh.ActiveWindowGet(xu)
	if activeErr == nil && active != xproto.WindowNone {
		return windowbackend.Handle(active), nil
	}
	focus, focusErr := xproto.GetInputFocus(xu.Conn()).Reply()
	if focusErr != nil {
		return 0, errors.Join(activeErr, fmt.Errorf("query X11 input focus: %w", focusErr))
	}
	if focus == nil || focus.Focus == xproto.WindowNone || focus.Focus == xu.RootWin() {
		return 0, fmt.Errorf("%w: X11 has no active client window", ErrWindowNotFound)
	}
	return windowbackend.Handle(focus.Focus), nil
}

func (system *nativeSystem) WindowExists(handle windowbackend.Handle) (bool, error) {
	window, ok := x11Window(handle)
	if !ok {
		return false, nil
	}
	xu, err := system.open()
	if err != nil {
		return false, err
	}
	defer xu.Conn().Close()
	reply, err := xproto.GetWindowAttributes(xu.Conn(), window).Reply()
	if err != nil {
		var invalidWindow xproto.WindowError
		if errors.As(err, &invalidWindow) {
			return false, nil
		}
		return false, fmt.Errorf("query X11 window attributes: %w", err)
	}
	return reply != nil, nil
}

func (system *nativeSystem) FindWindowByPID(pid uint32) (windowbackend.Handle, error) {
	if pid == 0 {
		return 0, fmt.Errorf("%w: pid is zero", ErrWindowNotFound)
	}
	xu, err := system.open()
	if err != nil {
		return 0, err
	}
	defer xu.Conn().Close()

	windows, listErr := ewmh.ClientListGet(xu)
	if listErr == nil {
		if window := findWindowByPID(xu, windows, pid); window != xproto.WindowNone {
			return windowbackend.Handle(window), nil
		}
	}
	window, treeErr := findWindowByPIDInTree(xu, pid)
	if treeErr != nil {
		return 0, errors.Join(listErr, treeErr)
	}
	if window == xproto.WindowNone {
		return 0, fmt.Errorf("%w: no X11 client for pid %d", ErrWindowNotFound, pid)
	}
	return windowbackend.Handle(window), nil
}

func (system *nativeSystem) WindowProcessID(handle windowbackend.Handle) (uint32, error) {
	window, ok := x11Window(handle)
	if !ok {
		return 0, fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return 0, err
	}
	defer xu.Conn().Close()
	return windowPID(xu, window)
}

func (system *nativeSystem) WindowText(handle windowbackend.Handle) (string, error) {
	window, ok := x11Window(handle)
	if !ok {
		return "", fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return "", err
	}
	defer xu.Conn().Close()

	utf8Atom, utf8Err := internAtom(xu.Conn(), atomUTF8String, true)
	modernErr := utf8Err
	if utf8Err == nil && utf8Atom != xproto.AtomNone {
		value, propertyErr := stringProperty(
			xu.Conn(),
			window,
			atomWMName,
			utf8Atom,
			true,
		)
		switch {
		case propertyErr != nil:
			modernErr = propertyErr
		case value == "":
			modernErr = fmt.Errorf("X11 property %s is empty", atomWMName)
		case !utf8.ValidString(value):
			modernErr = fmt.Errorf("X11 property %s is not valid UTF-8", atomWMName)
		default:
			return value, nil
		}
	} else if utf8Err == nil {
		modernErr = fmt.Errorf("X11 atom %s is unavailable", atomUTF8String)
	}
	value, legacyErr := stringProperty(
		xu.Conn(),
		window,
		atomLegacyWMName,
		xproto.AtomString,
		true,
	)
	if legacyErr != nil {
		return "", errors.Join(modernErr, legacyErr)
	}
	if value == "" {
		return "", errors.Join(
			fmt.Errorf("%w: XID %#x has no title", ErrWindowNotFound, window),
			modernErr,
		)
	}
	return value, nil
}

func (system *nativeSystem) WindowRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	window, ok := x11Window(handle)
	if !ok {
		return windowbackend.Rect{}, fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return windowbackend.Rect{}, err
	}
	defer xu.Conn().Close()
	client, err := clientRect(xu, window)
	if err != nil {
		return windowbackend.Rect{}, err
	}
	extents, err := frameExtents(xu.Conn(), window)
	if err != nil {
		return client, nil
	}
	outer, err := addFrameExtents(client, extents)
	if err != nil {
		return client, nil
	}
	return outer, nil
}

func (system *nativeSystem) ClientRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	window, ok := x11Window(handle)
	if !ok {
		return windowbackend.Rect{}, fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return windowbackend.Rect{}, err
	}
	defer xu.Conn().Close()
	return clientRect(xu, window)
}

func (system *nativeSystem) ActivateWindow(handle windowbackend.Handle) error {
	return system.withWindowManager(
		handle,
		[]string{atomActiveWindow},
		func(xu *xgbutil.XUtil, window xproto.Window) error {
			return ewmh.ActiveWindowReq(xu, window)
		},
	)
}

func (system *nativeSystem) SetWindowState(
	handle windowbackend.Handle,
	state windowbackend.State,
	enabled bool,
) error {
	var required []string
	switch state {
	case windowbackend.StateMinimized:
		if enabled {
			required = []string{atomWMState, atomWMStateHidden}
		} else {
			required = []string{atomActiveWindow}
		}
	case windowbackend.StateMaximized:
		required = []string{
			atomWMState,
			atomWMStateMaxHorz,
			atomWMStateMaxVert,
		}
	default:
		return fmt.Errorf("%w: unknown X11 window state %d", ErrOperation, state)
	}
	return system.withWindowManager(handle, required, func(xu *xgbutil.XUtil, window xproto.Window) error {
		switch state {
		case windowbackend.StateMinimized:
			if !enabled {
				return ewmh.ActiveWindowReq(xu, window)
			}
			return ewmh.ClientEvent(xu, window, atomWMChangeState, icccmIconicState)
		case windowbackend.StateMaximized:
			action := ewmh.StateRemove
			if enabled {
				action = ewmh.StateAdd
			}
			return ewmh.WmStateReqExtra(
				xu,
				window,
				action,
				atomWMStateMaxHorz,
				atomWMStateMaxVert,
				2,
			)
		}
		return fmt.Errorf("%w: unknown X11 window state %d", ErrOperation, state)
	})
}

func (system *nativeSystem) WindowState(
	handle windowbackend.Handle,
	state windowbackend.State,
) (bool, error) {
	window, ok := x11Window(handle)
	if !ok {
		return false, fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return false, err
	}
	defer xu.Conn().Close()
	switch state {
	case windowbackend.StateMinimized:
		if err := requireWindowManager(xu, atomWMState, atomWMStateHidden); err != nil {
			return false, err
		}
		atoms, err := windowStateAtoms(xu.Conn(), window)
		if err != nil {
			return false, err
		}
		return hasAtom(xu.Conn(), atoms, atomWMStateHidden)
	case windowbackend.StateMaximized:
		if err := requireWindowManager(
			xu,
			atomWMState,
			atomWMStateMaxHorz,
			atomWMStateMaxVert,
		); err != nil {
			return false, err
		}
		atoms, err := windowStateAtoms(xu.Conn(), window)
		if err != nil {
			return false, err
		}
		horizontal, err := hasAtom(xu.Conn(), atoms, atomWMStateMaxHorz)
		if err != nil || !horizontal {
			return false, err
		}
		return hasAtom(xu.Conn(), atoms, atomWMStateMaxVert)
	default:
		return false, fmt.Errorf("%w: unknown X11 window state %d", ErrOperation, state)
	}
}

func (system *nativeSystem) IsTopMost(handle windowbackend.Handle) (bool, error) {
	window, ok := x11Window(handle)
	if !ok {
		return false, fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return false, err
	}
	defer xu.Conn().Close()
	if err := requireWindowManager(xu, atomWMState, atomWMStateAbove); err != nil {
		return false, err
	}
	atoms, err := windowStateAtoms(xu.Conn(), window)
	if err != nil {
		return false, err
	}
	return hasAtom(xu.Conn(), atoms, atomWMStateAbove)
}

func (system *nativeSystem) SetTopMost(handle windowbackend.Handle, enabled bool) error {
	return system.withWindowManager(
		handle,
		[]string{atomWMState, atomWMStateAbove},
		func(xu *xgbutil.XUtil, window xproto.Window) error {
			action := ewmh.StateRemove
			if enabled {
				action = ewmh.StateAdd
			}
			return ewmh.WmStateReq(xu, window, action, atomWMStateAbove)
		},
	)
}

func (system *nativeSystem) CloseWindow(handle windowbackend.Handle) error {
	return system.withWindowManager(
		handle,
		[]string{atomCloseWindow},
		func(xu *xgbutil.XUtil, window xproto.Window) error {
			return ewmh.CloseWindow(xu, window)
		},
	)
}

func (system *nativeSystem) withWindowManager(
	handle windowbackend.Handle,
	required []string,
	operation func(*xgbutil.XUtil, xproto.Window) error,
) error {
	window, ok := x11Window(handle)
	if !ok {
		return fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	xu, err := system.open()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()
	if err := requireWindowManager(xu, required...); err != nil {
		return err
	}
	return operation(xu, window)
}

func x11Window(handle windowbackend.Handle) (xproto.Window, bool) {
	if handle == 0 || uint64(handle) > math.MaxUint32 {
		return xproto.WindowNone, false
	}
	return xproto.Window(handle), true
}

func findWindowByPID(
	xu *xgbutil.XUtil,
	windows []xproto.Window,
	pid uint32,
) xproto.Window {
	for _, window := range windows {
		windowPID, err := windowPID(xu, window)
		if err == nil && windowPID == pid {
			return window
		}
	}
	return xproto.WindowNone
}

func findWindowByPIDInTree(xu *xgbutil.XUtil, pid uint32) (xproto.Window, error) {
	queue := []xproto.Window{xu.RootWin()}
	seen := make(map[xproto.Window]struct{}, 128)
	for len(queue) > 0 {
		if len(seen) >= maxWindowTreeNodes {
			return xproto.WindowNone, fmt.Errorf(
				"%w: X11 window tree exceeds %d nodes",
				ErrOperation,
				maxWindowTreeNodes,
			)
		}
		parent := queue[0]
		queue = queue[1:]
		if _, exists := seen[parent]; exists {
			continue
		}
		seen[parent] = struct{}{}
		reply, err := xproto.QueryTree(xu.Conn(), parent).Reply()
		if err != nil || reply == nil {
			continue
		}
		if window := findWindowByPID(xu, reply.Children, pid); window != xproto.WindowNone {
			return window, nil
		}
		queue = append(queue, reply.Children...)
	}
	return xproto.WindowNone, nil
}

func windowPID(xu *xgbutil.XUtil, window xproto.Window) (uint32, error) {
	values, err := cardinalProperty(xu.Conn(), window, atomWMPID, 1)
	if err != nil {
		return 0, err
	}
	if values[0] == 0 {
		return 0, fmt.Errorf("%w: XID %#x has a zero _NET_WM_PID", ErrWindowNotFound, window)
	}
	return values[0], nil
}

func clientRect(xu *xgbutil.XUtil, window xproto.Window) (windowbackend.Rect, error) {
	geometry, err := xproto.GetGeometry(xu.Conn(), xproto.Drawable(window)).Reply()
	if err != nil {
		return windowbackend.Rect{}, fmt.Errorf("query X11 geometry: %w", err)
	}
	if geometry == nil || geometry.Width == 0 || geometry.Height == 0 {
		return windowbackend.Rect{}, fmt.Errorf("%w: XID %#x returned empty geometry", ErrOperation, window)
	}
	translated, err := xproto.TranslateCoordinates(
		xu.Conn(),
		window,
		xu.RootWin(),
		0,
		0,
	).Reply()
	if err != nil {
		return windowbackend.Rect{}, fmt.Errorf("translate X11 client coordinates: %w", err)
	}
	if translated == nil {
		return windowbackend.Rect{}, errors.New("X11 returned no translated coordinates")
	}
	return windowbackend.Rect{
		X:      int(translated.DstX),
		Y:      int(translated.DstY),
		Width:  int(geometry.Width),
		Height: int(geometry.Height),
	}, nil
}

type x11FrameExtents struct {
	left   uint32
	right  uint32
	top    uint32
	bottom uint32
}

func frameExtents(conn *xgb.Conn, window xproto.Window) (x11FrameExtents, error) {
	values, err := cardinalProperty(conn, window, atomFrameExtents, 4)
	if err != nil {
		return x11FrameExtents{}, err
	}
	return x11FrameExtents{
		left:   values[0],
		right:  values[1],
		top:    values[2],
		bottom: values[3],
	}, nil
}

func addFrameExtents(
	client windowbackend.Rect,
	extents x11FrameExtents,
) (windowbackend.Rect, error) {
	if extents.left > maxFrameExtentPixels ||
		extents.right > maxFrameExtentPixels ||
		extents.top > maxFrameExtentPixels ||
		extents.bottom > maxFrameExtentPixels {
		return windowbackend.Rect{}, fmt.Errorf("%w: X11 frame extents exceed protocol geometry", ErrOperation)
	}
	x := int64(client.X) - int64(extents.left)
	y := int64(client.Y) - int64(extents.top)
	width := int64(client.Width) + int64(extents.left) + int64(extents.right)
	height := int64(client.Height) + int64(extents.top) + int64(extents.bottom)
	if x < int64(minInt()) || x > int64(maxInt()) ||
		y < int64(minInt()) || y > int64(maxInt()) ||
		width <= 0 || width > int64(maxInt()) ||
		height <= 0 || height > int64(maxInt()) {
		return windowbackend.Rect{}, fmt.Errorf("%w: X11 frame extents overflow", ErrOperation)
	}
	return windowbackend.Rect{
		X:      int(x),
		Y:      int(y),
		Width:  int(width),
		Height: int(height),
	}, nil
}

func requireWindowManager(xu *xgbutil.XUtil, required ...string) error {
	manager, err := windowProperty(
		xu.Conn(),
		xu.RootWin(),
		atomSupportingWMCheck,
	)
	if err != nil || manager == xproto.WindowNone {
		return fmt.Errorf("%w: root check: %v", ErrWindowManagerUnavailable, err)
	}
	self, err := windowProperty(xu.Conn(), manager, atomSupportingWMCheck)
	if err != nil || self != manager {
		return fmt.Errorf(
			"%w: manager check window %#x is inconsistent: self=%#x err=%v",
			ErrWindowManagerUnavailable,
			manager,
			self,
			err,
		)
	}
	if reply, err := xproto.GetWindowAttributes(xu.Conn(), manager).Reply(); err != nil || reply == nil {
		return fmt.Errorf(
			"%w: manager check window %#x is invalid: %v",
			ErrWindowManagerUnavailable,
			manager,
			err,
		)
	}
	if len(required) == 0 {
		return nil
	}
	supported, err := supportedAtoms(xu.Conn(), xu.RootWin())
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrWindowManagerUnavailable, atomSupported, err)
	}
	for _, name := range required {
		atom, err := internAtom(xu.Conn(), name, true)
		if err != nil || atom == xproto.AtomNone || !containsAtom(supported, atom) {
			return fmt.Errorf(
				"%w: manager does not advertise %s: atom=%#x err=%v",
				ErrWindowManagerUnavailable,
				name,
				atom,
				err,
			)
		}
	}
	return nil
}

func supportedAtoms(conn *xgb.Conn, root xproto.Window) ([]xproto.Atom, error) {
	property, err := internAtom(conn, atomSupported, true)
	if err != nil {
		return nil, err
	}
	if property == xproto.AtomNone {
		return nil, fmt.Errorf("X11 property %s is unavailable", atomSupported)
	}
	reply, err := xproto.GetProperty(
		conn,
		false,
		root,
		property,
		xproto.AtomAtom,
		0,
		maxPropertyBytes/4,
	).Reply()
	if err != nil {
		return nil, err
	}
	if reply == nil || reply.Type != xproto.AtomAtom || reply.Format != 32 ||
		uint64(reply.ValueLen)*4 != uint64(len(reply.Value)) || reply.BytesAfter != 0 {
		return nil, fmt.Errorf("X11 property %s is malformed", atomSupported)
	}
	atoms := make([]xproto.Atom, reply.ValueLen)
	for index := range atoms {
		atoms[index] = xproto.Atom(xgb.Get32(reply.Value[index*4:]))
	}
	return atoms, nil
}

func containsAtom(atoms []xproto.Atom, target xproto.Atom) bool {
	for _, atom := range atoms {
		if atom == target {
			return true
		}
	}
	return false
}

func windowProperty(conn *xgb.Conn, window xproto.Window, name string) (xproto.Window, error) {
	property, err := internAtom(conn, name, true)
	if err != nil {
		return xproto.WindowNone, fmt.Errorf("resolve X11 property %s: %w", name, err)
	}
	if property == xproto.AtomNone {
		return xproto.WindowNone, fmt.Errorf("X11 property %s is unavailable", name)
	}
	reply, err := xproto.GetProperty(
		conn,
		false,
		window,
		property,
		xproto.AtomWindow,
		0,
		1,
	).Reply()
	if err != nil {
		return xproto.WindowNone, err
	}
	if reply == nil || reply.Type != xproto.AtomWindow || reply.Format != 32 ||
		reply.ValueLen != 1 || len(reply.Value) != 4 || reply.BytesAfter != 0 {
		return xproto.WindowNone, fmt.Errorf("X11 property %s is malformed", name)
	}
	return xproto.Window(xgb.Get32(reply.Value)), nil
}

func cardinalProperty(
	conn *xgb.Conn,
	window xproto.Window,
	name string,
	count uint32,
) ([]uint32, error) {
	property, err := internAtom(conn, name, true)
	if err != nil {
		return nil, fmt.Errorf("resolve X11 property %s: %w", name, err)
	}
	if property == xproto.AtomNone {
		return nil, fmt.Errorf("X11 property %s is unavailable", name)
	}
	reply, err := xproto.GetProperty(
		conn,
		false,
		window,
		property,
		xproto.AtomCardinal,
		0,
		count,
	).Reply()
	if err != nil {
		return nil, err
	}
	if reply == nil || reply.Type != xproto.AtomCardinal || reply.Format != 32 ||
		reply.ValueLen != count || uint32(len(reply.Value)) != count*4 ||
		reply.BytesAfter != 0 {
		return nil, fmt.Errorf("X11 property %s is malformed", name)
	}
	values := make([]uint32, count)
	for index := range values {
		values[index] = xgb.Get32(reply.Value[index*4:])
	}
	return values, nil
}

func stringProperty(
	conn *xgb.Conn,
	window xproto.Window,
	name string,
	propertyType xproto.Atom,
	onlyIfExists bool,
) (string, error) {
	property, err := internAtom(conn, name, onlyIfExists)
	if err != nil {
		return "", fmt.Errorf("resolve X11 property %s: %w", name, err)
	}
	if property == xproto.AtomNone {
		return "", fmt.Errorf("X11 property %s is unavailable", name)
	}
	reply, err := xproto.GetProperty(
		conn,
		false,
		window,
		property,
		propertyType,
		0,
		maxPropertyBytes/4,
	).Reply()
	if err != nil {
		return "", err
	}
	if reply == nil || reply.Type != propertyType || reply.Format != 8 ||
		reply.ValueLen != uint32(len(reply.Value)) || reply.BytesAfter != 0 {
		return "", fmt.Errorf("X11 property %s is malformed", name)
	}
	return strings.TrimRight(string(reply.Value), "\x00"), nil
}

func windowStateAtoms(conn *xgb.Conn, window xproto.Window) ([]xproto.Atom, error) {
	property, err := internAtom(conn, atomWMState, true)
	if err != nil {
		return nil, fmt.Errorf("resolve X11 property %s: %w", atomWMState, err)
	}
	if property == xproto.AtomNone {
		return nil, nil
	}
	reply, err := xproto.GetProperty(
		conn,
		false,
		window,
		property,
		xproto.AtomAtom,
		0,
		maxPropertyBytes/4,
	).Reply()
	if err != nil {
		return nil, err
	}
	if reply != nil && reply.Type == xproto.AtomNone && reply.Format == 0 &&
		reply.ValueLen == 0 && len(reply.Value) == 0 && reply.BytesAfter == 0 {
		return nil, nil
	}
	if reply == nil || reply.Type != xproto.AtomAtom || reply.Format != 32 ||
		uint64(reply.ValueLen)*4 != uint64(len(reply.Value)) || reply.BytesAfter != 0 {
		return nil, fmt.Errorf("X11 property %s is malformed", atomWMState)
	}
	atoms := make([]xproto.Atom, reply.ValueLen)
	for index := range atoms {
		atoms[index] = xproto.Atom(xgb.Get32(reply.Value[index*4:]))
	}
	return atoms, nil
}

func hasAtom(conn *xgb.Conn, atoms []xproto.Atom, name string) (bool, error) {
	target, err := internAtom(conn, name, true)
	if err != nil {
		return false, err
	}
	if target == xproto.AtomNone {
		return false, nil
	}
	for _, atom := range atoms {
		if atom == target {
			return true, nil
		}
	}
	return false, nil
}

func internAtom(conn *xgb.Conn, name string, onlyIfExists bool) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(
		conn,
		onlyIfExists,
		uint16(len(name)),
		name,
	).Reply()
	if err != nil {
		return xproto.AtomNone, err
	}
	if reply == nil {
		return xproto.AtomNone, fmt.Errorf("X11 returned no atom for %s", name)
	}
	return reply.Atom, nil
}

func minInt() int {
	return -maxInt() - 1
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
