//go:build !cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/marang/robotgo/clipboard"
	inputportal "github.com/marang/robotgo/input/portal"
)

const Version = "v1.00.0.1189, MT. Baker!"

var (
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	MouseSleep = 0
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	KeySleep = 10
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	DisplayID = -1
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	NotPid bool
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	Scale bool
)

type DisplayServer string

const (
	DisplayServerX11     DisplayServer = "x11"
	DisplayServerWayland DisplayServer = "wayland"
	DisplayServerUnknown DisplayServer = "unknown"
)

type FeatureCapability struct {
	Available bool
	Fallback  bool
	Backend   string
	Reason    string
	Notes     string
}

type LinuxCapabilities struct {
	DisplayServer  DisplayServer
	Compositor     string
	WaylandSession bool
	X11Session     bool
	Capture        FeatureCapability
	Bounds         FeatureCapability
	Keyboard       FeatureCapability
	Mouse          FeatureCapability
	RemoteDesktop  FeatureCapability
	Window         FeatureCapability
	Hook           FeatureCapability
	Events         FeatureCapability
}

type CaptureBackend string

const (
	BackendNone       CaptureBackend = ""
	BackendScreencopy CaptureBackend = "screencopy"
	BackendPortal     CaptureBackend = "portal"
	BackendScreenCast CaptureBackend = "screencast"
	BackendX11        CaptureBackend = "x11"
	BackendPureGo     CaptureBackend = "pure-go"
)

type WaylandBackend int

const (
	WaylandBackendAuto   WaylandBackend = -1
	WaylandBackendDmabuf WaylandBackend = 0
	WaylandBackendWlShm  WaylandBackend = 1
)

var (
	ErrWaylandDisplay   = errors.New("wayland connect failed")
	ErrNoScreencopy     = errors.New("screencopy manager not available")
	ErrNoOutputs        = errors.New("no outputs")
	ErrDmabufDevice     = errors.New("screencopy dmabuf device unsupported")
	ErrDmabufModifiers  = errors.New("screencopy dmabuf modifiers unsupported")
	ErrDmabufImport     = errors.New("screencopy dmabuf import failed")
	ErrDmabufMap        = errors.New("screencopy dmabuf map failed")
	ErrWaylandFailed    = errors.New("wayland capture failed")
	ErrPortalFailed     = errors.New("portal capture failed")
	ErrNotSupported     = errors.New("operation not supported on current platform/backend")
	ErrPermissionDenied = errors.New("permission denied by desktop security policy")
)

type (
	Map    map[string]interface{}
	CHex   uint32
	Handle uintptr
)

type Point struct{ X, Y int }
type Size struct{ W, H int }
type Rect struct {
	Point
	Size
}

type Bitmap struct {
	ImgBuf        *uint8
	Width, Height int
	Bytewidth     int
	BitsPixel     uint8
	BytesPerPixel uint8
	buf           []uint8
	trusted       bool
}

// CBitmap is an opaque compatibility handle in non-CGO builds.
type CBitmap = *Bitmap

func GetVersion() string { return Version }

func DetectDisplayServer() DisplayServer {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return DisplayServerWayland
	}
	if os.Getenv("DISPLAY") != "" {
		return DisplayServerX11
	}
	return DisplayServerUnknown
}

func GetLinuxCapabilities() LinuxCapabilities {
	ds := DetectDisplayServer()
	unsupported := FeatureCapability{
		Available: false,
		Reason:    ErrNotSupported.Error(),
		Notes:     "this build has no selected pure-Go display backend",
	}
	capabilities := LinuxCapabilities{
		DisplayServer:  ds,
		WaylandSession: ds == DisplayServerWayland,
		X11Session:     ds == DisplayServerX11,
		Capture:        unsupported,
		Bounds:         unsupported,
		Keyboard:       unsupported,
		Mouse:          unsupported,
		Window:         unsupported,
		Hook:           unsupported,
		Events:         unsupported,
	}
	overrideCapture, captureOverridden := pureGoCaptureOverrideCapability()
	if captureOverridden {
		capabilities.Capture = overrideCapture
	}
	if runtime.GOOS == "linux" && ds == DisplayServerWayland {
		if !captureOverridden {
			capabilities.Capture = pureGoPortalCaptureCapability(
				"Pure-Go Wayland capture uses the screenshot portal",
				"capture APIs may prompt for consent",
			)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		portalCapability, err := remoteDesktopStatusProbe(ctx)
		cancel()
		if err != nil && portalCapability.ScreenCastIssue == "" {
			capabilities.RemoteDesktop = FeatureCapability{
				Available: false,
				Backend:   "portal-remote-desktop",
				Reason:    err.Error(),
				Notes:     "the pure-Go RemoteDesktop client remains usable without CGO when a portal backend is available",
			}
		} else {
			notes := fmt.Sprintf(
				"interface version=%d device-mask=%d; screencast version=%d source-mask=%d cursor-mask=%d",
				portalCapability.Version, portalCapability.AvailableDevices,
				portalCapability.ScreenCastVersion, portalCapability.AvailableSources,
				portalCapability.AvailableCursorModes,
			)
			if portalCapability.ScreenCastIssue != "" {
				notes += "; ScreenCast capability degraded: " + portalCapability.ScreenCastIssue
			}
			capabilities.RemoteDesktop = FeatureCapability{
				Available: portalCapability.AvailableDevices != 0,
				Backend:   "portal-remote-desktop",
				Reason:    "RemoteDesktop portal capability probed without CGO",
				Notes:     notes,
			}
		}
		if err := RemoteDesktopInputReady(RemoteDesktopKeyboard); err == nil {
			capabilities.Keyboard = FeatureCapability{
				Available: true,
				Fallback:  true,
				Backend:   "portal-remote-desktop",
				Reason:    "active pure-Go RemoteDesktop session grants keyboard input",
				Notes:     "TypeStrE and UnicodeTypeE use the consent-aware portal session",
			}
		}
		if err := RemoteDesktopInputReady(RemoteDesktopPointer); err == nil {
			capabilities.Mouse = FeatureCapability{
				Available: true,
				Fallback:  true,
				Backend:   "portal-remote-desktop",
				Reason:    "active pure-Go RemoteDesktop session grants pointer input",
				Notes:     "relative movement, buttons, and scroll use the consent-aware portal session",
			}
		}
	}
	if runtime.GOOS == "linux" && ds == DisplayServerX11 && !captureOverridden {
		compiled := pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH)
		conflict := pureGoX11EnvironmentConflict()
		capabilities.Capture = FeatureCapability{
			Available: compiled && !conflict,
			Backend:   featureBackendPureGoX11,
			Reason:    "Pure-Go X11 screenshot backend is selected; runtime access is not probed",
			Notes:     "runtime X server access is validated when capture starts",
		}
		if !compiled {
			capabilities.Capture.Reason = fmt.Sprintf("Pure-Go X11 capture is not compiled for %s/%s", runtime.GOOS, runtime.GOARCH)
			capabilities.Capture.Notes = ErrNotSupported.Error()
		} else if conflict {
			capabilities.Capture.Reason = envXDGSessionType + " selects Wayland while DISPLAY selects X11"
			capabilities.Capture.Notes = "capture refuses the screenshot dependency's implicit portal fallback"
		}
		capabilities.Bounds = FeatureCapability{
			Available: compiled && !conflict,
			Backend:   featureBackendPureGoX11,
			Reason:    "Pure-Go X11 display enumeration is selected; runtime access is not probed",
			Notes:     "runtime X server access is validated when bounds are queried",
		}
		if !compiled {
			capabilities.Bounds.Reason = fmt.Sprintf("Pure-Go X11 bounds are not compiled for %s/%s", runtime.GOOS, runtime.GOARCH)
			capabilities.Bounds.Notes = ErrNotSupported.Error()
		} else if conflict {
			capabilities.Bounds.Reason = envXDGSessionType + " selects Wayland while DISPLAY selects X11"
			capabilities.Bounds.Notes = "bounds refuse the screenshot dependency's implicit portal fallback"
		}
	}
	return capabilities
}

func pureGoCaptureOverrideCapability() (FeatureCapability, bool) {
	if runtime.GOOS != "linux" {
		return FeatureCapability{}, false
	}
	override := pureGoWaylandBackendOverride()
	if override == waylandBackendScreenCast {
		return FeatureCapability{
			Backend: string(BackendScreenCast),
			Reason:  "persistent ScreenCast capture requires a CGO PipeWire backend",
			Notes:   ErrNotSupported.Error(),
		}, true
	}
	if !pureGoPortalForced(override) {
		return FeatureCapability{}, false
	}
	return pureGoPortalCaptureCapability(
		"screenshot portal capture is forced by runtime configuration",
		"capture APIs may prompt for consent",
	), true
}

func pureGoPortalCaptureCapability(reason, notes string) FeatureCapability {
	capability := FeatureCapability{
		Backend: string(BackendPortal),
		Reason:  reason,
		Notes:   notes,
	}
	if os.Getenv(envDisablePortal) != "" {
		capability.Reason = "screenshot portal disabled by " + envDisablePortal
		capability.Notes = "remove the override to enable consent-aware Pure-Go capture"
		return capability
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	available, err := pureGoPortalAvailable(ctx)
	cancel()
	capability.Available = available && err == nil
	if err != nil {
		capability.Reason = err.Error()
	} else if !available {
		capability.Reason = "screenshot portal service is not available"
	}
	return capability
}

func SetWaylandBackend(WaylandBackend) {}
func MilliSleep(tm int)                { time.Sleep(time.Duration(tm) * time.Millisecond) }
func Sleep(tm int)                     { time.Sleep(time.Duration(tm) * time.Second) }
func FreeBitmap(CBitmap)               {}

func alertArgs(args ...string) (string, string) {
	defaultButton := "Ok"
	cancelButton := ""
	if len(args) > 0 && args[0] != "" {
		defaultButton = args[0]
	}
	if len(args) > 1 {
		cancelButton = args[1]
	}
	return defaultButton, cancelButton
}

func SysScale(...int) float64 { return 1 }

func GetBounds(int, ...int) (int, int, int, int) { return 0, 0, 0, 0 }
func GetClient(int, ...int) (int, int, int, int) { return 0, 0, 0, 0 }
func GetTitle(...int) string                     { return "" }
func SetActive(Handle)                           {}
func SetActiveE(Handle) error                    { return ErrNotSupported }
func ActivePid(int, ...int) error                { return ErrNotSupported }
func ActiveName(string) error                    { return ErrNotSupported }

const (
	KeyA           = "a"
	KeyI           = "i"
	KeyGrave       = "`"
	KeyQuote       = "'"
	KeyDoubleQuote = "\""
	KeyQuoter      = KeyDoubleQuote
	Enter          = "enter"
	Alt            = "alt"
	Cmd            = "cmd"
)

// KeyTap taps a key through an explicitly authorized RemoteDesktop session.
func KeyTap(key string, args ...interface{}) error {
	pid, _, modifiers, err := parsePortalKeyArgs(args, false)
	if err != nil {
		return err
	}
	used, err := withRemoteDesktopInput(inputportal.DeviceKeyboard, func(session remoteDesktopInputSession) error {
		if pid != 0 {
			return fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
		}
		mainKey, modifierKeys, err := portalKeysymsPure(key, modifiers)
		if err != nil {
			return err
		}
		return portalModifiedKey(session, mainKey, modifierKeys, true, true)
	})
	if !used {
		return ErrNotSupported
	}
	if err == nil {
		MilliSleep(currentKeyDelay())
	}
	return err
}

func parsePortalKeyArgs(args []interface{}, toggle bool) (pid int, down bool, modifiers []string, err error) {
	down = true
	if len(args) == 0 {
		return pid, down, nil, nil
	}
	if values, ok := args[0].([]string); ok {
		modifiers = append(modifiers, values...)
		args = args[1:]
	} else if value, ok := args[0].(int); ok {
		pid = value
		args = args[1:]
	}
	for _, arg := range args {
		value, ok := arg.(string)
		if !ok {
			return 0, false, nil, fmt.Errorf("robotgo: key modifier must be a string, got %T", arg)
		}
		modifiers = append(modifiers, value)
	}
	if toggle && len(modifiers) > 0 && (modifiers[0] == "up" || modifiers[0] == "down") {
		down = modifiers[0] == "down"
		modifiers = modifiers[1:]
	}
	return pid, down, modifiers, nil
}

// KeyToggle changes a key state through an authorized RemoteDesktop session.
func KeyToggle(key string, args ...interface{}) error {
	pid, down, modifiers, err := parsePortalKeyArgs(args, true)
	if err != nil {
		return err
	}
	used, err := withRemoteDesktopInput(inputportal.DeviceKeyboard, func(session remoteDesktopInputSession) error {
		if pid != 0 {
			return fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
		}
		mainKey, modifierKeys, err := portalKeysymsPure(key, modifiers)
		if err != nil {
			return err
		}
		return portalModifiedKey(session, mainKey, modifierKeys, down, false)
	})
	if !used {
		return ErrNotSupported
	}
	if err == nil {
		MilliSleep(currentKeyDelay())
	}
	return err
}

// KeyDown presses a key through an authorized RemoteDesktop session.
func KeyDown(key string, args ...interface{}) error { return KeyToggle(key, args...) }

// KeyUp releases a key through an authorized RemoteDesktop session.
func KeyUp(key string, args ...interface{}) error {
	return KeyToggle(key, append([]interface{}{"up"}, args...)...)
}

// KeyPress presses and releases a key through an authorized RemoteDesktop session.
func KeyPress(key string, args ...interface{}) error {
	if err := KeyDown(key, args...); err != nil {
		return err
	}
	MilliSleep(2)
	return KeyUp(key, args...)
}
func KeyboardReady() error                  { return RemoteDesktopInputReady(RemoteDesktopKeyboard) }
func UnicodeType(value uint32, args ...int) { _ = UnicodeTypeE(value, args...) }
func UnicodeTypeE(value uint32, args ...int) error {
	used, err := tryRemoteDesktopUnicode(rune(value), args)
	if !used {
		return ErrNotSupported
	}
	return err
}
func TypeStr(text string, args ...int) { _ = TypeStrE(text, args...) }
func TypeStrE(text string, args ...int) error {
	used, err := tryRemoteDesktopText(text, args)
	if !used {
		return ErrNotSupported
	}
	return err
}
func TypeStrDelay(text string, delay int) {
	TypeStr(text)
	MilliSleep(delay)
}
func PasteStr(string) error      { return ErrNotSupported }
func ReadAll() (string, error)   { return clipboard.ReadAll() }
func WriteAll(text string) error { return clipboard.WriteAll(text) }
func CharCodeAt(s string, n int) rune {
	for i, r := range []rune(s) {
		if i == n {
			return r
		}
	}
	return 0
}

func Move(x, y int, displayID ...int) { _ = MoveE(x, y, displayID...) }
func MoveE(x, y int, displayID ...int) error {
	used, err := tryRemoteDesktopMoveAbsolute(x, y, displayID)
	if !used {
		return ErrNotSupported
	}
	return finishRemoteDesktopMouseEvent(err, 0)
}
func MoveRelative(x, y int) { _ = MoveRelativeE(x, y) }
func MoveRelativeE(x, y int) error {
	used, err := tryRemoteDesktopMoveRelative(x, y)
	if !used {
		return ErrNotSupported
	}
	return finishRemoteDesktopMouseEvent(err, 0)
}
func MoveSmooth(int, int, ...interface{}) bool    { return false }
func MoveSmoothRelative(int, int, ...interface{}) {}
func DragSmooth(int, int, ...interface{})         {}
func Click(args ...interface{})                   { _ = ClickE(args...) }
func ClickE(args ...interface{}) error {
	name, double, err := parseClickArguments(args)
	if err != nil {
		return err
	}
	used, err := tryRemoteDesktopClick(name, double)
	if !used {
		return ErrNotSupported
	}
	return finishRemoteDesktopMouseEvent(err, 0)
}
func Toggle(args ...interface{}) error {
	name, down, err := parseToggleArguments(args)
	if err != nil {
		return err
	}
	used, err := tryRemoteDesktopToggle(name, down)
	if !used {
		return ErrNotSupported
	}
	return err
}
func Scroll(x, y int, args ...int) { _ = ScrollE(x, y, args...) }
func ScrollE(x, y int, args ...int) error {
	msDelay := 10
	if len(args) > 0 {
		msDelay = args[0]
	}
	used, err := tryRemoteDesktopScroll(x, y)
	if !used {
		return ErrNotSupported
	}
	return finishRemoteDesktopMouseEvent(err, msDelay)
}
func ScrollDir(int, ...interface{}) {}
func Location() (int, int)          { return 0, 0 }
func LocationE() (int, int, error)  { return 0, 0, ErrNotSupported }
func MouseReady() error             { return RemoteDesktopInputReady(RemoteDesktopPointer) }
func CloseWaylandInput()            { _ = CloseRemoteDesktopInput() }
func GetScreenSize() (int, int) {
	displayID := currentDisplayID()
	if displayID < 0 {
		displayID = 0
	}
	_, _, width, height := GetDisplayBounds(displayID)
	return width, height
}
func GetScreenRect(displayID ...int) Rect {
	id := currentDisplayID()
	if id < 0 {
		id = 0
	}
	if len(displayID) > 0 {
		id = displayID[0]
	}
	return GetDisplayRect(id)
}
func GetScaleSize(...int) (int, int) { return GetScreenSize() }
func DisplaysNum() int               { return platformDisplayCount() }

// GetPixelColor returns the pixel color at (x, y) as a six-digit RGB string.
func GetPixelColor(x, y int, displayID ...int) (string, error) {
	value, err := GetPxColor(x, y, displayID...)
	if err != nil {
		return "", err
	}
	return PadHex(value), nil
}

// GetPxColor returns the pixel color at (x, y) through the active Pure-Go
// capture backend. The optional display index follows CaptureImg semantics.
func GetPxColor(x, y int, displayID ...int) (uint32, error) {
	args := []int{x, y, 1, 1}
	if len(displayID) > 0 {
		args = append(args, displayID[0])
	}
	img, err := CaptureImg(args...)
	if err != nil {
		return 0, err
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return 0, errors.New("Pure-Go pixel capture returned an empty image")
	}
	pixel := color.NRGBAModel.Convert(img.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA)
	return RgbToHex(pixel.R, pixel.G, pixel.B), nil
}
func ToBitmap(bit CBitmap) Bitmap {
	if bit == nil {
		return Bitmap{}
	}
	return *bit
}
func ToCBitmap(bit Bitmap) CBitmap {
	bitmap, _ := ToCBitmapE(bit)
	return bitmap
}
func ToCBitmapE(bit Bitmap) (CBitmap, error) {
	data, err := bitmapBytes(bit)
	if err != nil {
		return nil, err
	}
	result := bit
	result.buf = data
	result.ImgBuf = &result.buf[0]
	return &result, nil
}
func ToImage(bit CBitmap) image.Image {
	img, err := ToRGBAE(bit)
	if err != nil {
		return nil
	}
	return img
}
func ToRGBA(bit CBitmap) *image.RGBA {
	img, _ := ToRGBAE(bit)
	return img
}
func ToRGBAE(bit CBitmap) (*image.RGBA, error) {
	if bit == nil {
		return nil, errors.New("bitmap is nil")
	}
	return ToRGBAGoE(*bit)
}
func ImgToCBitmap(img image.Image) CBitmap {
	bitmap, _ := ImgToCBitmapE(img)
	return bitmap
}
func ImgToCBitmapE(img image.Image) (CBitmap, error) {
	bitmap, err := ImgToBitmapE(img)
	if err != nil {
		return nil, err
	}
	return ToCBitmapE(bitmap)
}
func ByteToCBitmap(data []byte) CBitmap {
	bitmap, _ := ByteToCBitmapE(data)
	return bitmap
}
func ByteToCBitmapE(data []byte) (CBitmap, error) {
	img, err := ByteToImg(data)
	if err != nil {
		return nil, err
	}
	return ImgToCBitmapE(img)
}
func PadHex(hex uint32) string   { return fmt.Sprintf("%06x", hex&0xffffff) }
func U32ToHex(hex uint32) uint32 { return hex }
func RgbToHex(r, g, b uint8) uint32 {
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}
func HexToRgb(hex uint32) *uint8 {
	rgb := &[3]uint8{uint8(hex >> 16), uint8(hex >> 8), uint8(hex)}
	return &rgb[0]
}
func U8ToHex(rgb *uint8) uint32 {
	if rgb == nil {
		return 0
	}
	return RgbToHex(
		*rgb,
		*(*uint8)(unsafe.Add(unsafe.Pointer(rgb), 1)),
		*(*uint8)(unsafe.Add(unsafe.Pointer(rgb), 2)),
	)
}
func GetActive() Handle             { return 0 }
func GetHandle() int                { return 0 }
func GetPid() int                   { return os.Getpid() }
func MinWindow(int, ...interface{}) {}
func MaxWindow(int, ...interface{}) {}
func CloseWindow(...int)            {}
func CloseWindowKill(...int) error  { return ErrNotSupported }
