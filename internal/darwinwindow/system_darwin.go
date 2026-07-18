//go:build darwin

package darwinwindow

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/marang/robotgo/internal/windowbackend"
)

const (
	applicationServicesFramework = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	coreFoundationFramework      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	coreGraphicsFramework        = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"

	axErrorSuccess                           int32  = 0
	axErrorInvalidUIElement                  int32  = -25202
	axErrorAttributeUnsupported              int32  = -25205
	axErrorActionUnsupported                 int32  = -25206
	axErrorNotImplemented                    int32  = -25208
	axErrorAPIDisabled                       int32  = -25211
	axErrorParameterizedAttributeUnsupported int32  = -25213
	axValueCGPointType                       uint32 = 1
	axValueCGSizeType                        uint32 = 2
	cfNumberSInt32Type                       int64  = 3
	cfStringEncodingUTF8                     uint32 = 0x08000100
	setFrontProcessFrontWindowOnly           uint32 = 1 << 0
	maximumAXWindows                                = 4096
	maximumAXStringBytes                            = 1 << 20
	maximumExactCoordinate                          = 1 << 53
)

var errWindowUnavailable = errors.New("macOS window is unavailable")

type processSerialNumber struct {
	HighLongOfPSN uint32
	LowLongOfPSN  uint32
}

type point struct {
	X float64
	Y float64
}

type size struct {
	Width  float64
	Height float64
}

type nativeSystem struct {
	mu  sync.Mutex
	api *nativeAPI
}

func newNativeSystem() System { return &nativeSystem{} }

func (system *nativeSystem) Ready() error {
	system.mu.Lock()
	defer system.mu.Unlock()
	_, err := system.readyLocked()
	return err
}

func (system *nativeSystem) ActiveWindow() (windowbackend.Handle, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return 0, err
	}
	var psn processSerialNumber
	if status := api.getFrontProcess(&psn); status != 0 {
		return 0, fmt.Errorf("GetFrontProcess failed with OSStatus %d", status)
	}
	var pid int32
	if status := api.getProcessPID(&psn, &pid); status != 0 || pid <= 0 {
		return 0, fmt.Errorf("GetProcessPID failed with OSStatus %d and pid %d", status, pid)
	}
	return applicationWindowLocked(api, pid)
}

func (system *nativeSystem) WindowExists(handle windowbackend.Handle) (bool, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return false, err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		if errors.Is(err, ErrPermission) || errors.Is(err, ErrUnsupported) {
			return false, err
		}
		if errors.Is(err, errWindowUnavailable) {
			return false, nil
		}
		return false, err
	}
	api.cfRelease(element)
	return true, nil
}

func (system *nativeSystem) FindWindowByPID(pid int32) (windowbackend.Handle, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, fmt.Errorf("%w: invalid pid %d", errWindowUnavailable, pid)
	}
	return applicationWindowLocked(api, pid)
}

func (system *nativeSystem) WindowPID(handle windowbackend.Handle) (int32, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return 0, err
	}
	return windowPIDLocked(api, handle)
}

func (system *nativeSystem) WindowTitle(handle windowbackend.Handle) (string, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return "", err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return "", err
	}
	defer api.cfRelease(element)
	value, err := copyAttributeLocked(api, element, api.axTitleAttribute)
	if err != nil {
		return "", err
	}
	defer api.cfRelease(value)
	return cfStringLocked(api, value)
}

func (system *nativeSystem) WindowRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return windowbackend.Rect{}, err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return windowbackend.Rect{}, err
	}
	defer api.cfRelease(element)
	positionValue, err := copyAttributeLocked(api, element, api.axPositionAttribute)
	if err != nil {
		return windowbackend.Rect{}, err
	}
	defer api.cfRelease(positionValue)
	if err := requireCFType(
		api,
		positionValue,
		api.axValueGetTypeID(),
		"AXPosition",
	); err != nil {
		return windowbackend.Rect{}, err
	}
	if valueType := api.axValueGetType(positionValue); valueType != axValueCGPointType {
		return windowbackend.Rect{}, fmt.Errorf(
			"AX position has type %d, want CGPoint type %d",
			valueType,
			axValueCGPointType,
		)
	}
	sizeValue, err := copyAttributeLocked(api, element, api.axSizeAttribute)
	if err != nil {
		return windowbackend.Rect{}, err
	}
	defer api.cfRelease(sizeValue)
	if err := requireCFType(
		api,
		sizeValue,
		api.axValueGetTypeID(),
		"AXSize",
	); err != nil {
		return windowbackend.Rect{}, err
	}
	if valueType := api.axValueGetType(sizeValue); valueType != axValueCGSizeType {
		return windowbackend.Rect{}, fmt.Errorf(
			"AX size has type %d, want CGSize type %d",
			valueType,
			axValueCGSizeType,
		)
	}
	var position point
	if !api.axValueGetValue(positionValue, axValueCGPointType, unsafe.Pointer(&position)) {
		return windowbackend.Rect{}, errors.New("AXValueGetValue could not decode window position")
	}
	var dimensions size
	if !api.axValueGetValue(sizeValue, axValueCGSizeType, unsafe.Pointer(&dimensions)) {
		return windowbackend.Rect{}, errors.New("AXValueGetValue could not decode window size")
	}
	return enclosingRect(position, dimensions)
}

func (system *nativeSystem) RaiseWindow(handle windowbackend.Handle) error {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return err
	}
	defer api.cfRelease(element)
	raiseErr := axCallError(
		"raise AX window",
		api.axUIElementPerformAction(element, api.axRaiseAction),
	)
	var pid int32
	pidErr := axCallError(
		"get AX window pid",
		api.axUIElementGetPID(element, &pid),
	)
	if pidErr == nil && pid <= 0 {
		pidErr = fmt.Errorf("get AX window pid returned invalid pid %d", pid)
	}
	var activateErr error
	if pidErr == nil {
		var psn processSerialNumber
		if status := api.getProcessForPID(pid, &psn); status != 0 {
			activateErr = fmt.Errorf("GetProcessForPID(%d) failed with OSStatus %d", pid, status)
		} else if status := api.setFrontProcessWithOptions(
			&psn,
			setFrontProcessFrontWindowOnly,
		); status != 0 {
			activateErr = fmt.Errorf(
				"SetFrontProcessWithOptions(%d) failed with OSStatus %d",
				pid,
				status,
			)
		}
	}
	return errors.Join(raiseErr, pidErr, activateErr)
}

func (system *nativeSystem) SetMinimized(handle windowbackend.Handle, enabled bool) error {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return err
	}
	defer api.cfRelease(element)
	value := api.cfBooleanFalse
	if enabled {
		value = api.cfBooleanTrue
	}
	if result := api.axUIElementSetAttributeValue(element, api.axMinimizedAttribute, value); result != axErrorSuccess {
		return axCallError("set AXMinimized", result)
	}
	return nil
}

func (system *nativeSystem) IsMinimized(handle windowbackend.Handle) (bool, error) {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return false, err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return false, err
	}
	defer api.cfRelease(element)
	value, err := copyAttributeLocked(api, element, api.axMinimizedAttribute)
	if err != nil {
		return false, err
	}
	defer api.cfRelease(value)
	if err := requireCFType(
		api,
		value,
		api.cfBooleanGetTypeID(),
		"AXMinimized",
	); err != nil {
		return false, err
	}
	return api.cfBooleanGetValue(value), nil
}

func (system *nativeSystem) CloseWindow(handle windowbackend.Handle) error {
	system.mu.Lock()
	defer system.mu.Unlock()
	api, err := system.readyLocked()
	if err != nil {
		return err
	}
	element, err := windowElementLocked(api, handle)
	if err != nil {
		return err
	}
	defer api.cfRelease(element)
	button, err := copyAttributeLocked(api, element, api.axCloseButtonAttribute)
	if err != nil {
		return err
	}
	defer api.cfRelease(button)
	if err := requireCFType(
		api,
		button,
		api.axUIElementGetTypeID(),
		"AXCloseButton",
	); err != nil {
		return err
	}
	if result := api.axUIElementPerformAction(button, api.axPressAction); result != axErrorSuccess {
		return axCallError("press AX close button", result)
	}
	return nil
}

func (system *nativeSystem) Close() error {
	system.mu.Lock()
	defer system.mu.Unlock()
	if system.api == nil {
		return nil
	}
	err := system.api.close()
	system.api = nil
	return err
}

func (system *nativeSystem) readyLocked() (*nativeAPI, error) {
	if system.api == nil {
		api, err := openNativeAPI()
		if err != nil {
			return nil, err
		}
		system.api = api
	}
	if !system.api.axIsProcessTrusted() {
		return nil, fmt.Errorf(
			"%w: grant this application access in System Settings > Privacy & Security > Accessibility",
			ErrPermission,
		)
	}
	return system.api, nil
}
