//go:build !cgo

package robotgo

import (
	"math"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/marang/robotgo/internal/windowswindow"
)

// The functions in this file preserve the portable public surface for builds
// without CGO. Native operations return ErrNotSupported instead of disappearing
// from the package API.

func ActivePidC(target int, args ...int) error { return ActivePid(target, args...) }
func CloseWindowE(args ...int) error {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return err
	}
	var handle windowswindow.Handle
	if len(args) == 0 {
		handle, err = backend.Active()
	} else {
		handle, err = backend.Resolve(args[0], len(args) > 1 || currentTreatAsHandle())
	}
	if err != nil {
		return err
	}
	return backend.Close(handle)
}

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
func GetBHandle() int {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return 0
	}
	if selected := backend.Selected(); selected != 0 {
		return int(selected)
	}
	return GetHandle()
}
func GetHWNDByPid(pid int) int {
	handle, err := pureGoWindowResolve(pid, false)
	if err != nil {
		return 0
	}
	return int(handle)
}
func GetLocationColor(displayID ...int) (string, error) {
	return getLocationColorWith(displayID, LocationE, GetPixelColor)
}

func getLocationColorWith(
	displayID []int,
	location func() (int, int, error),
	pixelColor func(int, int, ...int) (string, error),
) (string, error) {
	x, y, err := location()
	if err != nil {
		return "", err
	}
	return pixelColor(x, y, displayID...)
}

func GetMousePos() (int, int)               { return Location() }
func GetTitleE(args ...int) (string, error) { return pureGoWindowTitle(args...) }
func GetXDisplayName() string               { return "" }
func SetXDisplayName(string) error          { return ErrNotSupported }
func Is64Bit() bool                         { return strconv.IntSize == 64 }
func IsMain(displayID int) bool             { return displayID == GetMainId() }
func IsValid() bool {
	_, err := pureGoWindowActive()
	return err == nil
}
func IsTopMost() bool {
	state, _ := IsTopMostE()
	return state
}
func IsTopMostE() (bool, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return false, err
	}
	handle, err := backend.Active()
	if err != nil {
		return false, err
	}
	return backend.TopMost(handle)
}
func IsMinimized() bool {
	state, _ := IsMinimizedE()
	return state
}
func IsMinimizedE() (bool, error) {
	return pureGoActiveWindowState(windowswindow.StateMinimized)
}
func IsMaximized() bool {
	state, _ := IsMaximizedE()
	return state
}
func IsMaximizedE() (bool, error) {
	return pureGoActiveWindowState(windowswindow.StateMaximized)
}
func SetTopMost(state bool) { _ = SetTopMostE(state) }
func SetTopMostE(state bool) error {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return err
	}
	handle, err := backend.Active()
	if err != nil {
		return err
	}
	return backend.SetTopMost(handle, state)
}
func SetHandle(handle int) {
	backend, err := pureGoWindowBackend()
	if err == nil {
		_ = backend.Select(handle, true)
	}
}
func SetHandlePid(target int, args ...int) {
	backend, err := pureGoWindowBackend()
	if err == nil {
		_ = backend.Select(target, len(args) > 0 || currentTreatAsHandle())
	}
}
func GetHandById(id int, args ...int) Handle {
	isHandle := true
	if len(args) > 0 {
		isHandle = args[0] != 0
	}
	handle, err := pureGoWindowResolve(id, isHandle)
	if err != nil {
		return 0
	}
	return Handle(handle)
}
func GetHandByPid(target int, args ...int) Handle {
	handle, err := pureGoWindowResolve(target, len(args) > 0 || currentTreatAsHandle())
	if err != nil {
		return 0
	}
	return Handle(handle)
}
func GetHandPid(target int, args ...int) Handle { return GetHandByPid(target, args...) }
func MicroSleep(tm float64)                     { time.Sleep(time.Duration(tm * float64(time.Millisecond))) }
func PadHexs(hex CHex) string                   { return PadHex(uint32(hex)) }
func UintToHex(value uint32) CHex               { return CHex(value) }
func Scaled(x int, displayID ...int) int        { return Scaled0(x, ScaleF(displayID...)) }
func Scaled0(x int, factor float64) int         { return int(float64(x) * factor) }
func Scaled1(x int, factor float64) int         { return int(float64(x) / factor) }
func ScaleX() int {
	if runtime.GOOS != "windows" {
		return 0
	}
	return int(math.Round(96 * SysScale(-2)))
}
func Scale0() int { return int(float64(ScaleX()) / 0.96) }
func Scale1() int {
	return int(math.Round(float64(ScaleX()) * 100 / 96))
}
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

func MinWindowE(target int, args ...interface{}) error {
	state, err := parseWindowStateArguments(args)
	if err != nil {
		return err
	}
	return pureGoSetWindowState(target, state, len(args) > 1 || currentTreatAsHandle(), windowswindow.StateMinimized)
}
func MaxWindowE(target int, args ...interface{}) error {
	state, err := parseWindowStateArguments(args)
	if err != nil {
		return err
	}
	return pureGoSetWindowState(target, state, len(args) > 1 || currentTreatAsHandle(), windowswindow.StateMaximized)
}

func pureGoActiveWindowState(state windowswindow.State) (bool, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return false, err
	}
	handle, err := backend.Active()
	if err != nil {
		return false, err
	}
	return backend.State(handle, state)
}

func pureGoSetWindowState(target int, enabled, isHandle bool, state windowswindow.State) error {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return err
	}
	handle, err := backend.Resolve(target, isHandle)
	if err != nil {
		return err
	}
	return backend.SetState(handle, state, enabled)
}
