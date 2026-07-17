//go:build !cgo

package robotgo

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The functions in this file preserve the portable public surface for builds
// without CGO. Native operations return ErrNotSupported instead of disappearing
// from the package API.

func ActivePidC(int, ...int) error { return ErrNotSupported }
func CloseWindowE(...int) error    { return ErrNotSupported }

func CmdCtrl() string {
	if runtime.GOOS == "darwin" {
		return "cmd"
	}
	return "ctrl"
}

// CloseMainDisplay closes persistent Pure-Go display/input connections and
// ignores cleanup errors for compatibility. Prefer CloseMainDisplayE when
// server-global X11 scratch mappings or owned input state may need restoration.
func CloseMainDisplay() { _ = CloseMainDisplayE() }

// CloseMainDisplayE closes persistent Pure-Go display/input connections and
// reports owned-input release or X11 scratch-mapping restoration failures. A
// later operation reconnects lazily. On Pure-Go X11, key taps and text may use
// server-global scratch mappings, so call it only after targets have processed
// all prior keyboard input. Abnormal process termination can leave mappings
// behind. If cleanup reports a pressed-key or modifier-map conflict, remove
// that conflict and retry CloseMainDisplayE. On Pure-Go Windows it releases
// RobotGo-owned persistent key and button holds; process termination cannot run
// this in-process cleanup.
func CloseMainDisplayE() error { return closePureGoPlatformInput() }

func InvalidateScreenBoundsCache() {}
func Drag(int, int, ...string)     {}
func FreeBitmapArr(bits ...CBitmap) {
	for _, bit := range bits {
		FreeBitmap(bit)
	}
}
func GetBHandle() int                                 { return GetHandle() }
func GetHWNDByPid(int) int                            { return 0 }
func GetLocationColor(...int) (string, error)         { return "", ErrNotSupported }
func GetMousePos() (int, int)                         { return Location() }
func GetTitleE(...int) (string, error)                { return "", ErrNotSupported }
func GetXDisplayName() string                         { return "" }
func SetXDisplayName(string) error                    { return ErrNotSupported }
func Is64Bit() bool                                   { return strconv.IntSize == 64 }
func IsMain(displayID int) bool                       { return displayID == GetMainId() }
func IsValid() bool                                   { return false }
func IsTopMost() bool                                 { return false }
func IsTopMostE() (bool, error)                       { return false, ErrNotSupported }
func IsMinimized() bool                               { return false }
func IsMinimizedE() (bool, error)                     { return false, ErrNotSupported }
func IsMaximized() bool                               { return false }
func IsMaximizedE() (bool, error)                     { return false, ErrNotSupported }
func SetTopMost(bool)                                 {}
func SetTopMostE(bool) error                          { return ErrNotSupported }
func SetHandle(int)                                   {}
func SetHandlePid(int, ...int)                        {}
func GetHandById(int, ...int) Handle                  { return 0 }
func GetHandByPid(int, ...int) Handle                 { return 0 }
func GetHandPid(int, ...int) Handle                   { return 0 }
func MicroSleep(tm float64)                           { time.Sleep(time.Duration(tm * float64(time.Millisecond))) }
func PadHexs(hex CHex) string                         { return PadHex(uint32(hex)) }
func UintToHex(value uint32) CHex                     { return CHex(value) }
func Scaled(x int, displayID ...int) int              { return Scaled0(x, ScaleF(displayID...)) }
func Scaled0(x int, factor float64) int               { return int(float64(x) * factor) }
func Scaled1(x int, factor float64) int               { return int(float64(x) / factor) }
func ScaleX() int                                     { return 96 }
func Scale0() int                                     { return int(float64(ScaleX()) / 0.96) }
func Scale1() int                                     { return 100 }
func Mul(x int) int                                   { return x * Scale1() / 100 }
func MoveScale(x, y int, _ ...int) (int, int)         { return x, y }
func MoveArgs(x, y int) (int, int)                    { mx, my := Location(); return mx + x, my + y }
func MoveClick(x, y int, args ...interface{})         { Move(x, y); Click(args...) }
func MovesClick(x, y int, args ...interface{})        { MoveSmooth(x, y); Click(args...) }
func MoveMouse(x, y int)                              { Move(x, y) }
func MoveMouseSmooth(x, y int, a ...interface{}) bool { return MoveSmooth(x, y, a...) }
func DragMouse(x, y int, args ...interface{})         { DragSmooth(x, y, args...) }
func MouseClick(args ...interface{})                  { Click(args...) }
func MouseDown(args ...interface{}) error             { return Toggle(args...) }
func MouseUp(args ...interface{}) error {
	if len(args) == 0 {
		args = []interface{}{"left"}
	}
	toggleArgs := append([]interface{}(nil), args...)
	return Toggle(append(toggleArgs, "up")...)
}
func ScrollRelative(x, y int, args ...int) { Scroll(x, y, args...) }
func ScrollSmooth(to int, args ...int) {
	num, delay, horizontal := 5, 100, 0
	if len(args) > 0 {
		num = args[0]
	}
	if len(args) > 1 {
		delay = args[1]
	}
	if len(args) > 2 {
		horizontal = args[2]
	}
	for i := 0; i < num; i++ {
		Scroll(horizontal, to)
		MilliSleep(delay)
	}
}
func SetDelay(delay ...int) {
	value := 10
	if len(delay) > 0 {
		value = delay[0]
	}
	_ = setInputDelays(value, value)
}
func ToInterfaces(fields []string) []interface{} {
	result := make([]interface{}, len(fields))
	for i := range fields {
		result[i] = fields[i]
	}
	return result
}
func ToStrings(fields []interface{}) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		value, ok := field.(string)
		if !ok {
			return nil
		}
		result = append(result, value)
	}
	return result
}
func ToUC(text string) []string {
	result := make([]string, 0, len([]rune(text)))
	for _, value := range text {
		quoted := strconv.QuoteToASCII(string(value))
		encoded := strings.ReplaceAll(quoted[1:len(quoted)-1], `\u`, "U")
		if encoded == `\\` {
			encoded = `\`
		}
		if encoded == `\"` {
			encoded = `"`
		}
		result = append(result, encoded)
	}
	return result
}
func Try(fn func(), handler func(interface{})) {
	defer func() {
		if recovered := recover(); recovered != nil && handler != nil {
			handler(recovered)
		}
	}()
	fn()
}
func TypeStringDelayed(text string, delay int) { TypeStrDelay(text, delay) }

func MinWindowE(_ int, args ...interface{}) error {
	if _, err := parseWindowStateArguments(args); err != nil {
		return err
	}
	return ErrNotSupported
}
func MaxWindowE(_ int, args ...interface{}) error {
	if _, err := parseWindowStateArguments(args); err != nil {
		return err
	}
	return ErrNotSupported
}
