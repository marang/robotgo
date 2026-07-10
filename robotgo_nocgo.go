//go:build !cgo

package robotgo

import (
	"errors"
	"fmt"
	"image"
	"os"
	"time"
	"unsafe"

	"github.com/marang/robotgo/clipboard"
)

const Version = "v1.00.0.1189, MT. Baker!"

var (
	MouseSleep = 0
	KeySleep   = 10
	DisplayID  = -1
	NotPid     bool
	Scale      bool
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
	Window         FeatureCapability
}

type CaptureBackend string

const (
	BackendNone       CaptureBackend = ""
	BackendScreencopy CaptureBackend = "screencopy"
	BackendPortal     CaptureBackend = "portal"
	BackendX11        CaptureBackend = "x11"
)

type WaylandBackend int

const (
	WaylandBackendAuto   WaylandBackend = -1
	WaylandBackendDmabuf WaylandBackend = 0
	WaylandBackendWlShm  WaylandBackend = 1
)

var (
	ErrWaylandDisplay  = errors.New("wayland connect failed")
	ErrNoScreencopy    = errors.New("screencopy manager not available")
	ErrNoOutputs       = errors.New("no outputs")
	ErrDmabufDevice    = errors.New("screencopy dmabuf device unsupported")
	ErrDmabufModifiers = errors.New("screencopy dmabuf modifiers unsupported")
	ErrDmabufImport    = errors.New("screencopy dmabuf import failed")
	ErrDmabufMap       = errors.New("screencopy dmabuf map failed")
	ErrWaylandFailed   = errors.New("wayland capture failed")
	ErrPortalFailed    = errors.New("portal capture failed")
	ErrNotSupported    = errors.New("operation not supported on current platform/backend")
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
	return LinuxCapabilities{
		DisplayServer:  ds,
		WaylandSession: ds == DisplayServerWayland,
		X11Session:     ds == DisplayServerX11,
		Capture:        unsupported,
		Bounds:         unsupported,
		Keyboard:       unsupported,
		Mouse:          unsupported,
		Window:         unsupported,
	}
}

func LastBackend() CaptureBackend            { return BackendNone }
func SetWaylandBackend(WaylandBackend)       {}
func MilliSleep(tm int)                      { time.Sleep(time.Duration(tm) * time.Millisecond) }
func Sleep(tm int)                           { time.Sleep(time.Duration(tm) * time.Second) }
func FreeBitmap(CBitmap)                     {}
func CaptureScreen(...int) (CBitmap, error)  { return nil, ErrNotSupported }
func CaptureImg(...int) (image.Image, error) { return nil, ErrNotSupported }

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

func KeyTap(string, ...interface{}) error    { return ErrNotSupported }
func KeyToggle(string, ...interface{}) error { return ErrNotSupported }
func KeyboardReady() error                   { return ErrNotSupported }
func UnicodeType(uint32, ...int)             {}
func TypeStr(string, ...int)                 {}
func TypeStrDelay(string, int)               {}
func PasteStr(string) error                  { return ErrNotSupported }
func ReadAll() (string, error)               { return clipboard.ReadAll() }
func WriteAll(text string) error             { return clipboard.WriteAll(text) }
func CharCodeAt(s string, n int) rune {
	for i, r := range []rune(s) {
		if i == n {
			return r
		}
	}
	return 0
}

func Move(int, int, ...int)                       {}
func MoveE(int, int, ...int) error                { return ErrNotSupported }
func MoveRelative(int, int)                       {}
func MoveRelativeE(int, int) error                { return ErrNotSupported }
func MoveSmooth(int, int, ...interface{}) bool    { return false }
func MoveSmoothRelative(int, int, ...interface{}) {}
func DragSmooth(int, int, ...interface{})         {}
func Click(...interface{})                        {}
func ClickE(...interface{}) error                 { return ErrNotSupported }
func Toggle(...interface{}) error                 { return ErrNotSupported }
func Scroll(int, int, ...int)                     {}
func ScrollE(int, int, ...int) error              { return ErrNotSupported }
func ScrollDir(int, ...interface{})               {}
func Location() (int, int)                        { return 0, 0 }
func LocationE() (int, int, error)                { return 0, 0, ErrNotSupported }
func MouseReady() error                           { return ErrNotSupported }
func CloseWaylandInput()                          {}
func GetScreenSize() (int, int)                   { return 0, 0 }
func GetScreenRect(...int) Rect                   { return Rect{} }
func GetScaleSize(...int) (int, int)              { return 0, 0 }
func DisplaysNum() int                            { return 0 }
func GetPixelColor(int, int, ...int) (string, error) {
	return "", ErrNotSupported
}
func GetPxColor(int, int, ...int) (uint32, error) { return 0, ErrNotSupported }
func CaptureGo(...int) (Bitmap, error)            { return Bitmap{}, ErrNotSupported }
func ToBitmap(bit CBitmap) Bitmap {
	if bit == nil {
		return Bitmap{}
	}
	return *bit
}
func ToImage(CBitmap) image.Image { return image.NewRGBA(image.Rect(0, 0, 0, 0)) }
func PadHex(hex uint32) string    { return fmt.Sprintf("%06x", hex&0xffffff) }
func U32ToHex(hex uint32) uint32  { return hex }
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
