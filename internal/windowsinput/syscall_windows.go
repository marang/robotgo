//go:build windows

package windowsinput

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyEventExtended = 0x0001
	keyEventKeyUp    = 0x0002
	keyEventUnicode  = 0x0004

	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800
	mouseEventHWheel     = 0x1000

	wheelDelta = 120

	mapVirtualKeyToScanCode = 0

	shiftStateShift   = 1
	shiftStateControl = 2
	shiftStateAlt     = 4
)

const (
	vkLButton        = 0x01
	vkRButton        = 0x02
	vkMButton        = 0x04
	vkBack           = 0x08
	vkTab            = 0x09
	vkReturn         = 0x0d
	vkShift          = 0x10
	vkControl        = 0x11
	vkMenu           = 0x12
	vkPause          = 0x13
	vkCapital        = 0x14
	vkEscape         = 0x1b
	vkSpace          = 0x20
	vkPrior          = 0x21
	vkNext           = 0x22
	vkEnd            = 0x23
	vkHome           = 0x24
	vkLeft           = 0x25
	vkUp             = 0x26
	vkRight          = 0x27
	vkDown           = 0x28
	vkSnapshot       = 0x2c
	vkInsert         = 0x2d
	vkDelete         = 0x2e
	vkLWin           = 0x5b
	vkRWin           = 0x5c
	vkApps           = 0x5d
	vkNumpad0        = 0x60
	vkNumpad1        = 0x61
	vkNumpad2        = 0x62
	vkNumpad3        = 0x63
	vkNumpad4        = 0x64
	vkNumpad5        = 0x65
	vkNumpad6        = 0x66
	vkNumpad7        = 0x67
	vkNumpad8        = 0x68
	vkNumpad9        = 0x69
	vkMultiply       = 0x6a
	vkAdd            = 0x6b
	vkSubtract       = 0x6d
	vkDecimal        = 0x6e
	vkDivide         = 0x6f
	vkF1             = 0x70
	vkNumLock        = 0x90
	vkScroll         = 0x91
	vkLShift         = 0xa0
	vkRShift         = 0xa1
	vkLControl       = 0xa2
	vkRControl       = 0xa3
	vkLMenu          = 0xa4
	vkRMenu          = 0xa5
	vkVolumeMute     = 0xad
	vkVolumeDown     = 0xae
	vkVolumeUp       = 0xaf
	vkMediaNextTrack = 0xb0
	vkMediaPrevTrack = 0xb1
	vkMediaStop      = 0xb2
	vkMediaPlayPause = 0xb3
	vkOEMPlus        = 0xbb
)

type mouseInput struct {
	X         int32
	Y         int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keyboardInput struct {
	VirtualKey uint16
	ScanCode   uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

// inputRecord uses MOUSEINPUT as the union backing because it is the largest
// INPUT payload on both 32-bit and 64-bit Windows. Go's alignment then matches
// INPUT: 28 bytes on 32-bit and 40 bytes on 64-bit.
type inputRecord struct {
	Type    uint32
	Payload mouseInput
}

type trackedKind uint8

const (
	trackedNone trackedKind = iota
	trackedKey
	trackedButton
	trackedUnicode
)

type trackedInput struct {
	record   inputRecord
	kind     trackedKind
	code     uint32
	down     bool
	extended bool
}

func keyboardRecord(virtualKey, scanCode uint16, flags uint32) inputRecord {
	record := inputRecord{Type: inputKeyboard}
	keyboard := (*keyboardInput)(unsafe.Pointer(&record.Payload))
	keyboard.VirtualKey = virtualKey
	keyboard.ScanCode = scanCode
	keyboard.Flags = flags
	return record
}

func trackedKeyInput(key uint16, down bool) trackedInput {
	return trackedKeyInputExtended(key, down, extendedVirtualKey(key))
}

func trackedKeyInputExtended(key uint16, down, extended bool) trackedInput {
	flags := uint32(0)
	if extended {
		flags |= keyEventExtended
	}
	if !down {
		flags |= keyEventKeyUp
	}
	return trackedInput{
		record: keyboardRecord(key, 0, flags),
		kind:   trackedKey, code: uint32(key), down: down,
		extended: extended,
	}
}

func trackedUnicodeInput(unit uint16, down bool) trackedInput {
	flags := uint32(keyEventUnicode)
	if !down {
		flags |= keyEventKeyUp
	}
	return trackedInput{
		record: keyboardRecord(0, unit, flags),
		kind:   trackedUnicode, code: uint32(unit), down: down,
	}
}

func trackedButtonInput(downFlag uint32, down bool) trackedInput {
	flags := downFlag
	if !down {
		flags = mouseButtonUpFlag(downFlag)
	}
	return trackedInput{
		record: inputRecord{Type: inputMouse, Payload: mouseInput{Flags: flags}},
		kind:   trackedButton, code: downFlag, down: down,
	}
}

func trackedMouseInput(flags uint32, data int32) trackedInput {
	return trackedInput{
		record: inputRecord{
			Type:    inputMouse,
			Payload: mouseInput{Flags: flags, MouseData: uint32(data)},
		},
	}
}

func extendedVirtualKey(key uint16) bool {
	switch key {
	case vkRControl, vkSnapshot, vkRMenu, vkPause,
		vkHome, vkUp, vkPrior, vkLeft, vkRight, vkEnd, vkDown, vkNext,
		vkInsert, vkDelete, vkLWin, vkRWin, vkApps, vkDivide, vkNumLock,
		vkVolumeMute, vkVolumeDown, vkVolumeUp,
		vkMediaNextTrack, vkMediaPrevTrack, vkMediaStop, vkMediaPlayPause:
		return true
	default:
		return false
	}
}

type inputSystem interface {
	KeyboardReady() error
	MouseReady() error
	SendInput([]inputRecord) (int, error)
	CursorPosition() (int32, int32, error)
	SetCursorPosition(int32, int32) error
	KeyDown(uint16) bool
	VirtualKeyForRune(uint16) (uint16, uint8, bool)
	ScanCodeForVirtualKey(uint16) uint16
}

type win32System struct {
	sendInput        *windows.LazyProc
	getCursorPos     *windows.LazyProc
	setCursorPos     *windows.LazyProc
	getAsyncKeyState *windows.LazyProc
	vkKeyScanW       *windows.LazyProc
	mapVirtualKeyW   *windows.LazyProc
}

func newWin32System() *win32System {
	user32 := windows.NewLazySystemDLL("user32.dll")
	return &win32System{
		sendInput:        user32.NewProc("SendInput"),
		getCursorPos:     user32.NewProc("GetCursorPos"),
		setCursorPos:     user32.NewProc("SetCursorPos"),
		getAsyncKeyState: user32.NewProc("GetAsyncKeyState"),
		vkKeyScanW:       user32.NewProc("VkKeyScanW"),
		mapVirtualKeyW:   user32.NewProc("MapVirtualKeyW"),
	}
}

func findProcedures(procedures ...*windows.LazyProc) error {
	for _, procedure := range procedures {
		if err := procedure.Find(); err != nil {
			return fmt.Errorf("resolve user32.%s: %w", procedure.Name, err)
		}
	}
	return nil
}

func (system *win32System) KeyboardReady() error {
	return findProcedures(system.sendInput, system.getAsyncKeyState, system.vkKeyScanW, system.mapVirtualKeyW)
}

func (system *win32System) MouseReady() error {
	return findProcedures(system.sendInput, system.getCursorPos, system.setCursorPos, system.getAsyncKeyState)
}

func callFailure(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("%s failed without Win32 error information", operation)
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}

func (system *win32System) SendInput(records []inputRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	inserted, _, callErr := system.sendInput.Call(
		uintptr(len(records)),
		uintptr(unsafe.Pointer(&records[0])),
		unsafe.Sizeof(records[0]),
	)
	count := int(inserted)
	if count == len(records) {
		return count, nil
	}
	if count < 0 || count > len(records) {
		return 0, fmt.Errorf("SendInput returned invalid insertion count %d for %d events", count, len(records))
	}
	return count, fmt.Errorf(
		"SendInput inserted %d of %d events: %w; Windows UIPI can block injection into higher-integrity applications",
		count, len(records), callFailure("SendInput", callErr),
	)
}

type point struct {
	X int32
	Y int32
}

func (system *win32System) CursorPosition() (int32, int32, error) {
	var position point
	result, _, callErr := system.getCursorPos.Call(uintptr(unsafe.Pointer(&position)))
	if result == 0 {
		return 0, 0, callFailure("GetCursorPos", callErr)
	}
	return position.X, position.Y, nil
}

func (system *win32System) SetCursorPosition(x, y int32) error {
	result, _, callErr := system.setCursorPos.Call(
		uintptr(uint32(x)),
		uintptr(uint32(y)),
	)
	if result == 0 {
		return callFailure("SetCursorPos", callErr)
	}
	return nil
}

func (system *win32System) KeyDown(key uint16) bool {
	result, _, _ := system.getAsyncKeyState.Call(uintptr(key))
	return int16(uint16(result)) < 0
}

func (system *win32System) VirtualKeyForRune(value uint16) (uint16, uint8, bool) {
	result, _, _ := system.vkKeyScanW.Call(uintptr(value))
	translated := uint16(result)
	if translated == mathMaxUint16 {
		return 0, 0, false
	}
	return translated & 0xff, uint8(translated >> 8), true
}

func (system *win32System) ScanCodeForVirtualKey(value uint16) uint16 {
	result, _, _ := system.mapVirtualKeyW.Call(uintptr(value), mapVirtualKeyToScanCode)
	return uint16(result)
}

const mathMaxUint16 = ^uint16(0)

func removeUint16(values []uint16, target uint16) []uint16 {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == target {
			return append(values[:index], values[index+1:]...)
		}
	}
	return values
}

func removeUint32(values []uint32, target uint32) []uint32 {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == target {
			return append(values[:index], values[index+1:]...)
		}
	}
	return values
}

func (backend *Backend) applyTrackedInput(input trackedInput) {
	switch input.kind {
	case trackedKey:
		key := uint16(input.code)
		if input.down {
			if _, exists := backend.ownedKeys[key]; !exists {
				backend.ownedKeys[key] = struct{}{}
				backend.ownedKeyExtended[key] = input.extended
				backend.ownedKeyOrder = append(backend.ownedKeyOrder, key)
			}
		} else {
			delete(backend.ownedKeys, key)
			delete(backend.ownedKeyExtended, key)
			backend.ownedKeyOrder = removeUint16(backend.ownedKeyOrder, key)
		}
	case trackedButton:
		if input.down {
			if _, exists := backend.ownedButtons[input.code]; !exists {
				backend.ownedButtons[input.code] = struct{}{}
				backend.ownedButtonOrder = append(backend.ownedButtonOrder, input.code)
			}
		} else {
			delete(backend.ownedButtons, input.code)
			backend.ownedButtonOrder = removeUint32(backend.ownedButtonOrder, input.code)
		}
	case trackedUnicode:
		unit := uint16(input.code)
		if input.down {
			backend.ownedUnicode = append(backend.ownedUnicode, unit)
		} else {
			backend.ownedUnicode = removeUint16(backend.ownedUnicode, unit)
		}
	}
}

func copyUint16Set(source map[uint16]struct{}) map[uint16]struct{} {
	result := make(map[uint16]struct{}, len(source))
	for value := range source {
		result[value] = struct{}{}
	}
	return result
}

func copyUint32Set(source map[uint32]struct{}) map[uint32]struct{} {
	result := make(map[uint32]struct{}, len(source))
	for value := range source {
		result[value] = struct{}{}
	}
	return result
}

func (backend *Backend) prepareRecord(input *trackedInput) inputRecord {
	if input.kind == trackedKey {
		keyboard := (*keyboardInput)(unsafe.Pointer(&input.record.Payload))
		keyboard.ScanCode = backend.system.ScanCodeForVirtualKey(uint16(input.code))
	}
	return input.record
}

func (backend *Backend) rollbackNewOwnershipLocked(
	keysBefore map[uint16]struct{},
	buttonsBefore map[uint32]struct{},
	unicodeCountBefore int,
) error {
	var rollbackErr error
	for index := len(backend.ownedUnicode) - 1; index >= unicodeCountBefore; index-- {
		unit := backend.ownedUnicode[index]
		release := trackedUnicodeInput(unit, false)
		inserted, err := backend.system.SendInput([]inputRecord{
			backend.prepareRecord(&release),
		})
		if inserted == 1 {
			backend.applyTrackedInput(release)
		}
		rollbackErr = errors.Join(rollbackErr, err)
	}
	for index := len(backend.ownedKeyOrder) - 1; index >= 0; index-- {
		key := backend.ownedKeyOrder[index]
		if _, existed := keysBefore[key]; existed {
			continue
		}
		release := trackedKeyInputExtended(key, false, backend.ownedKeyExtended[key])
		inserted, err := backend.system.SendInput([]inputRecord{backend.prepareRecord(&release)})
		if inserted == 1 {
			backend.applyTrackedInput(release)
		}
		rollbackErr = errors.Join(rollbackErr, err)
	}
	for index := len(backend.ownedButtonOrder) - 1; index >= 0; index-- {
		button := backend.ownedButtonOrder[index]
		if _, existed := buttonsBefore[button]; existed {
			continue
		}
		release := trackedButtonInput(button, false)
		inserted, err := backend.system.SendInput([]inputRecord{backend.prepareRecord(&release)})
		if inserted == 1 {
			backend.applyTrackedInput(release)
		}
		rollbackErr = errors.Join(rollbackErr, err)
	}
	return rollbackErr
}

func (backend *Backend) sendTrackedLocked(inputs []trackedInput) error {
	if len(inputs) == 0 {
		return nil
	}
	records := make([]inputRecord, len(inputs))
	for index := range inputs {
		records[index] = backend.prepareRecord(&inputs[index])
	}
	keysBefore := copyUint16Set(backend.ownedKeys)
	buttonsBefore := copyUint32Set(backend.ownedButtons)
	unicodeCountBefore := len(backend.ownedUnicode)
	inserted, sendErr := backend.system.SendInput(records)
	if inserted < 0 {
		inserted = 0
	}
	if inserted > len(inputs) {
		inserted = len(inputs)
	}
	for index := 0; index < inserted; index++ {
		backend.applyTrackedInput(inputs[index])
	}
	if sendErr == nil && inserted == len(inputs) {
		return nil
	}
	if sendErr == nil {
		sendErr = fmt.Errorf("SendInput inserted %d of %d events", inserted, len(inputs))
	}
	rollbackErr := backend.rollbackNewOwnershipLocked(keysBefore, buttonsBefore, unicodeCountBefore)
	if rollbackErr != nil {
		rollbackErr = fmt.Errorf("release partially injected Windows input: %w", rollbackErr)
	}
	return errors.Join(sendErr, rollbackErr)
}
