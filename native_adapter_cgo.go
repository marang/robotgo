//go:build cgo

package robotgo

/*
#cgo darwin CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework Cocoa -framework CoreFoundation -framework IOKit
#cgo darwin LDFLAGS: -framework Carbon -framework OpenGL
//
#if __ENVIRONMENT_MAC_OS_X_VERSION_MIN_REQUIRED__ > 140400
#cgo darwin LDFLAGS: -framework ScreenCaptureKit
#endif

#cgo linux CFLAGS: -I/usr/src
#cgo linux,!wayland LDFLAGS: -L/usr/src -lm -lX11 -lXtst
#cgo linux,wayland LDFLAGS: -L/usr/src -lm
#cgo windows LDFLAGS: -lgdi32 -luser32
//
#include "screen/goScreen.h"
#include "mouse/mouse_c.h"
#ifdef DISPLAY_SERVER_WAYLAND
#include "window/goWindow_wayland_stub.h"
#else
#include "window/goWindow.h"
#endif
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/vcaesar/tt"
)

func nativeScreencopyProtocolVersion() uint32 {
	return uint32(C.robotgo_wayland_screencopy_version())
}

func nativeMouseProtocolVersion() uint32 {
	return uint32(C.robotgo_wayland_mouse_protocol_version())
}

func waylandErr(code int32) error {
	switch code {
	case int32(C.ScreengrabErrDisplay):
		return ErrWaylandDisplay
	case int32(C.ScreengrabErrNoManager):
		return ErrNoScreencopy
	case int32(C.ScreengrabErrNoOutputs):
		return ErrNoOutputs
	case int32(C.ScreengrabErrDmabufDevice):
		return ErrDmabufDevice
	case int32(C.ScreengrabErrDmabufModifiers):
		return ErrDmabufModifiers
	case int32(C.ScreengrabErrDmabufImport):
		return ErrDmabufImport
	case int32(C.ScreengrabErrDmabufMap):
		return ErrDmabufMap
	case int32(C.ScreengrabErrPixelFormat):
		return ErrWaylandPixelFormat
	default:
		return ErrWaylandFailed
	}
}

func captureViaPortalStub(x, y, w, h int32, displayId int, isPid int) (CBitmap, error) {
	var perr C.int32_t
	pbit := C.capture_screen_portal(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), C.int32_t(displayId), C.int8_t(isPid), &perr)
	if pbit == nil {
		return nil, fmt.Errorf("portal capture failed: %d", int(perr))
	}
	setLastBackend(BackendPortal)
	return CBitmap(pbit), nil
}

type (
	// Map a map[string]interface{}
	Map map[string]interface{}
	// CHex define CHex as c rgb Hex type (C.MMRGBHex)
	CHex C.MMRGBHex
	// CBitmap define CBitmap as C.MMBitmapRef type
	CBitmap = C.MMBitmapRef
	// Handle define window Handle as C.MData type
	Handle C.MData
)

// Deprecated: use the MilliSleep(),
//
// MicroSleep time C.microsleep(tm)
func MicroSleep(tm float64) {
	C.microsleep(C.double(tm))
}

// GoString trans C.char to string
func GoString(char *C.char) string {
	return C.GoString(char)
}

// ToMMRGBHex trans CHex to C.MMRGBHex
func ToMMRGBHex(hex CHex) C.MMRGBHex {
	return C.MMRGBHex(hex)
}

// UintToHex trans uint32 to robotgo.CHex
func UintToHex(u uint32) CHex {
	hex := U32ToHex(C.uint32_t(u))
	return CHex(hex)
}

// U32ToHex trans C.uint32_t to C.MMRGBHex
func U32ToHex(hex C.uint32_t) C.MMRGBHex {
	return C.MMRGBHex(hex)
}

// U8ToHex trans *C.uint8_t to C.MMRGBHex
func U8ToHex(hex *C.uint8_t) C.MMRGBHex {
	return C.MMRGBHex(*hex)
}

// PadHex trans C.MMRGBHex to string
func PadHex(hex C.MMRGBHex) string {
	color := C.pad_hex(hex)
	gcolor := C.GoString(color)
	C.free(unsafe.Pointer(color))

	return gcolor
}

// PadHexs trans CHex to string
func PadHexs(hex CHex) string {
	return PadHex(C.MMRGBHex(hex))
}

// HexToRgb trans hex to rgb
func HexToRgb(hex uint32) *C.uint8_t {
	return C.color_hex_to_rgb(C.uint32_t(hex))
}

// RgbToHex trans rgb to hex
func RgbToHex(r, g, b uint8) C.uint32_t {
	return C.color_rgb_to_hex(C.uint8_t(r), C.uint8_t(g), C.uint8_t(b))
}

// GetPxColor returns the pixel color at (x,y). On Linux it captures a 1x1
// region through the selected capture backend so invalid coordinates and
// backend failures are returned as errors instead of looking like black.
func GetPxColor(x, y int, displayId ...int) (C.MMRGBHex, error) {
	display := displayIdx(displayId...)

	if runtime.GOOS == "linux" {
		bit, err := CaptureScreen(x, y, 1, 1, display)
		if err != nil {
			return 0, err
		}
		defer FreeBitmap(bit)
		return C.mmrgb_hex_at(C.MMBitmapRef(bit), 0, 0), nil
	}

	color := C.get_px_color(C.int32_t(x), C.int32_t(y), C.int32_t(display))
	return color, nil
}

// GetHWNDByPid get the hwnd by pid
func GetHWNDByPid(pid int) int {
	return int(C.get_hwnd_by_pid(C.uintptr(pid)))
}

func sysScaleLocked(display int) float64 {
	return float64(C.sys_scale(C.int32_t(display)))
}

// FreeBitmap free and dealloc the C bitmap
func FreeBitmap(bitmap CBitmap) {
	if bitmap == nil {
		return
	}
	// C.destroyMMBitmap(bitmap)
	C.bitmap_dealloc(C.MMBitmapRef(bitmap))
}

// ToMMBitmapRef trans CBitmap to C.MMBitmapRef
func ToMMBitmapRef(bit CBitmap) C.MMBitmapRef {
	return C.MMBitmapRef(bit)
}

// ToBitmap trans C.MMBitmapRef to Bitmap
func ToBitmap(bit CBitmap) Bitmap {
	if bit == nil {
		return Bitmap{}
	}
	bitmap := Bitmap{
		ImgBuf:        (*uint8)(bit.imageBuffer),
		Width:         int(bit.width),
		Height:        int(bit.height),
		Bytewidth:     int(bit.bytewidth),
		BitsPixel:     uint8(bit.bitsPerPixel),
		BytesPerPixel: uint8(bit.bytesPerPixel),
		buf:           nil,
		trusted:       true,
	}

	return bitmap
}

// ToCBitmapE validates and copies a Go Bitmap into C-owned memory.
func ToCBitmapE(bit Bitmap) (CBitmap, error) {
	data, err := bitmapBytes(bit)
	if err != nil {
		return nil, err
	}
	cptr := C.CBytes(data)
	if cptr == nil {
		return nil, errors.New("allocate C bitmap buffer")
	}
	cbitmap := C.createMMBitmap_c(
		(*C.uint8_t)(cptr),
		C.int32_t(bit.Width),
		C.int32_t(bit.Height),
		C.int32_t(bit.Bytewidth),
		C.uint8_t(bit.BitsPixel),
		C.uint8_t(bit.BytesPerPixel),
	)
	if cbitmap == nil {
		C.free(cptr)
		return nil, errors.New("create C bitmap")
	}
	return CBitmap(cbitmap), nil
}

// GetXDisplayName get XDisplay name (Linux)
func GetXDisplayName() string {
	unlock := lockNativeX11Display()
	defer unlock()
	name := C.get_XDisplay_name()
	if name == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(name))
	return C.GoString(name)
}

func getXDisplayNameLocked() string {
	name := C.get_XDisplay_name_borrowed()
	if name == nil {
		return ""
	}
	return C.GoString(name)
}

// Deprecated: use the ScaledF(),
//
// ScaleX get the primary display horizontal DPI scale factor, drop
func ScaleX() int {
	return int(C.scaleX())
}

// CheckMouse check the mouse button
func CheckMouse(btn string) C.MMMouseButton {
	// button = args[0].(C.MMMouseButton)
	m1 := map[string]C.MMMouseButton{
		"left":       C.LEFT_BUTTON,
		"center":     C.CENTER_BUTTON,
		"middle":     C.CENTER_BUTTON,
		"right":      C.RIGHT_BUTTON,
		"wheelDown":  C.WheelDown,
		"wheelUp":    C.WheelUp,
		"wheelLeft":  C.WheelLeft,
		"wheelRight": C.WheelRight,
	}
	if v, ok := m1[btn]; ok {
		return v
	}

	return C.LEFT_BUTTON
}

func nativeMouseStatusError(status C.int, operation string) error {
	switch status {
	case C.ROBOTGO_MOUSE_OK:
		return nil
	case C.ROBOTGO_MOUSE_NO_DISPLAY:
		return fmt.Errorf("%w: %s: native mouse display is unavailable", ErrNotSupported, operation)
	case C.ROBOTGO_MOUSE_UNSUPPORTED:
		return fmt.Errorf("%w: %s", ErrNotSupported, operation)
	case C.ROBOTGO_MOUSE_INJECTION_FAILED:
		return fmt.Errorf("robotgo: %s: native mouse injection failed", operation)
	case C.ROBOTGO_MOUSE_OWNERSHIP_CONFLICT:
		return fmt.Errorf("%w: %s", ErrInputOwnership, operation)
	case C.ROBOTGO_MOUSE_INVALID:
		return fmt.Errorf("robotgo: %s: invalid native mouse input", operation)
	case C.ROBOTGO_MOUSE_DELIVERY_PENDING:
		return fmt.Errorf("robotgo: %s: native mouse input remains queued", operation)
	default:
		return fmt.Errorf("robotgo: %s: unknown native mouse status %d", operation, int(status))
	}
}

func nativeWaylandMouseButtonCodeForTest(name string) (uint32, error) {
	var code C.uint32_t
	var index C.uint
	status := C.robotgo_wayland_mouse_button_code(
		CheckMouse(canonicalMouseHoldName(name)), &code, &index,
	)
	return uint32(code), nativeMouseStatusError(status, "map Wayland mouse button")
}

func nativeWaylandMouseBackendSelectedForTest() bool {
	return bool(C.robotgo_wayland_mouse_backend_selected())
}

func nativeTrackedMouseToggle(name string, down bool) error {
	err := nativeMouseStatusError(
		C.toggleMouse(C.bool(down), CheckMouse(name)),
		"toggle mouse button",
	)
	if down && runtime.GOOS == "windows" && err != nil {
		return errors.Join(ErrInputNotApplied, err)
	}
	return err
}

// IsValid valid the window
func IsValid() bool {
	unlock := lockNativeX11Display()
	defer unlock()
	abool := C.is_valid()
	gbool := bool(abool)
	return gbool
}

func nativeIsTopMost() bool {
	return bool(C.IsTopMost())
}

func nativeIsMinimized() bool {
	return bool(C.IsMinimized())
}

func nativeIsMaximized() bool {
	return bool(C.IsMaximized())
}

func nativeSetTopMost(state bool) {
	C.SetTopMost(C.bool(state))
}

// SetActiveC set the window active
func SetActiveC(win C.MData) {
	unlock := lockNativeX11Display()
	defer unlock()
	_ = C.set_active(win)
}

func nativeSetActive(win Handle) bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.set_active(C.MData(win)))
}

func nativeMinWindow(pid int, state bool, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.min_window(C.uintptr(pid), C.bool(state), C.int8_t(flag)))
}

func nativeMaxWindow(pid int, state bool, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.max_window(C.uintptr(pid), C.bool(state), C.int8_t(flag)))
}

func nativeCloseMainWindow() bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.close_main_window())
}

func nativeCloseWindowByPid(pid int, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.close_window_by_PId(C.uintptr(pid), C.int8_t(flag)))
}

func nativeGetMainTitle() string {
	unlock := lockNativeX11Display()
	defer unlock()
	title := C.get_main_title()
	if title == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(title))
	return C.GoString(title)
}

// GetActiveC get the active window
func GetActiveC() C.MData {
	handle, _ := GetActiveE()
	return C.MData(handle)
}

func nativeGetActiveC() C.MData {
	unlock := lockNativeX11Display()
	defer unlock()
	return C.get_active()
}

// SetHandle set the window handle
func SetHandle(hwnd int) {
	chwnd := C.uintptr(hwnd)
	unlock := lockNativeX11Display()
	defer unlock()
	C.setHandle(chwnd)
}

// SetHandlePid set the window handle by pid
func SetHandlePid(pid int, args ...int) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	unlock := lockNativeX11Display()
	defer unlock()
	C.set_handle_pid_mData(C.uintptr(pid), C.int8_t(isPid))
}

// GetHandByPidC get handle mdata by pid
func GetHandByPidC(pid int, args ...int) C.MData {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	unlock := lockNativeX11Display()
	defer unlock()
	return C.set_handle_pid(C.uintptr(pid), C.int8_t(isPid))
}

// GetHandle get the window handle
func GetHandle() int {
	unlock := lockNativeX11Display()
	defer unlock()
	hwnd := C.get_handle()
	ghwnd := int(hwnd)
	// fmt.Println("gethwnd---", ghwnd)
	return ghwnd
}

// Deprecated: use the GetHandle(),
//
// # GetBHandle get the window handle, Wno-deprecated
//
// This function will be removed in version v1.0.0
func GetBHandle() int {
	tt.Drop("GetBHandle", "GetHandle")
	unlock := lockNativeX11Display()
	defer unlock()
	hwnd := C.b_get_handle()
	ghwnd := int(hwnd)
	//fmt.Println("gethwnd---", ghwnd)
	return ghwnd
}

func cgetTitleLocked(pid, isPid int) string {
	title := C.get_title_by_pid(C.uintptr(pid), C.int8_t(isPid))
	if title == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(title))
	return C.GoString(title)
}

func nativeGetActivePID() int {
	unlock := lockNativeX11Display()
	defer unlock()
	return int(C.get_PID())
}

func internalGetBoundsLocked(pid, isPid int) (int, int, int, int) {
	bounds := C.get_bounds(C.uintptr(pid), C.int8_t(isPid))
	return int(bounds.X), int(bounds.Y), int(bounds.W), int(bounds.H)
}

func internalGetClientLocked(pid, isPid int) (int, int, int, int) {
	bounds := C.get_client(C.uintptr(pid), C.int8_t(isPid))
	return int(bounds.X), int(bounds.Y), int(bounds.W), int(bounds.H)
}

// Is64Bit determine whether the sys is 64bit
func Is64Bit() bool {
	b := C.Is64Bit()
	return bool(b)
}

func internalActiveLocked(pid, isPid int) bool {
	return bool(C.active_PID(C.uintptr(pid), C.int8_t(isPid)))
}

// Native bridges below share the existing owning C translation unit. They
// perform no backend selection and acquire no Go device/display leases; policy
// callers retain the original transaction and cleanup order.
func nativeMainDisplaySize() Size {
	size := C.getMainDisplaySize()
	return Size{W: int(size.w), H: int(size.h)}
}

func nativeScreenRect(display int) Rect {
	rect := C.getScreenRect(C.int32_t(display))
	return Rect{Point{int(rect.origin.x), int(rect.origin.y)}, Size{int(rect.size.w), int(rect.size.h)}}
}

func nativeScreencopyReady() bool { return int(C.robotgo_wayland_screencopy_ready()) != 0 }

func nativeCaptureScreen(x, y, w, h int32, display, isPID int) CBitmap {
	return C.capture_screen(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), C.int32_t(display), C.int8_t(isPID))
}

func nativeCaptureWayland(x, y, w, h int32, display, isPID int, backend WaylandBackend) (CBitmap, int32) {
	var code C.int32_t
	bit := C.capture_screen_wayland(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), C.int32_t(display), C.int8_t(isPID), C.int32_t(backend), &code)
	return bit, int32(code)
}

func nativeCaptureWaylandTimeout(x, y, w, h int32, display, isPID int, backend WaylandBackend, timeoutMillis int64) (CBitmap, int32) {
	var code C.int32_t
	bit := C.capture_screen_wayland_timeout(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), C.int32_t(display), C.int8_t(isPID), C.int32_t(backend), C.int32_t(timeoutMillis), &code)
	return bit, int32(code)
}

func nativeCaptureTimedOut(code int32) bool { return code == int32(C.ScreengrabErrTimeout) }

func nativeReleaseX11MouseHolds() error {
	return nativeMouseStatusError(C.robotgo_x11_release_owned_buttons(), "release owned X11 mouse buttons")
}

func nativeToggleMouse(name string, down bool, operation string) error {
	return nativeMouseStatusError(C.toggleMouse(C.bool(down), CheckMouse(name)), operation)
}

func nativeWaylandMouseCompiled() bool { return int(C.robotgo_wayland_mouse_backend_enabled()) != 0 }

func nativeWaylandMouseProbe() bool { return int(C.robotgo_wayland_mouse_ready()) != 0 }

func nativeCloseWaylandMouse() { C.robotgo_wayland_mouse_close() }

func nativeMoveMouse(x, y int) error {
	return nativeMouseStatusError(C.moveMouseChecked(C.MMPointInt32Make(C.int32_t(x), C.int32_t(y))), "move mouse")
}

func nativeDragMouse(x, y int, button string) {
	C.dragMouse(C.MMPointInt32Make(C.int32_t(x), C.int32_t(y)), CheckMouse(button))
}

func nativeMoveMouseSmooth(x, y int, lowDelay, highDelay float64) bool {
	return bool(C.smoothlyMoveMouse(C.MMPointInt32Make(C.int32_t(x), C.int32_t(y)), C.double(lowDelay), C.double(highDelay)))
}

func nativeMoveMouseRelative(x, y int) { C.moveMouseRelative(C.int32_t(x), C.int32_t(y)) }

func nativeMouseLocation() (int, int) {
	position := C.location()
	return int(position.x), int(position.y)
}

func nativeClickMouse(name string, double bool) error {
	button := CheckMouse(name)
	if !double {
		return nativeMouseStatusError(C.clickMouse(button), "click mouse button")
	}
	return nativeMouseStatusError(C.doubleClick(button), "double-click mouse button")
}

func nativeScrollMouse(x, y int) { C.scrollMouseXY(C.int(x), C.int(y)) }

func nativeSetXDisplayName(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return toErr(C.set_XDisplay_name(cname))
}

func nativeCloseMainDisplay() { C.close_main_display() }
