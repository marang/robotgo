//go:build darwin

package darwininput

import (
	"errors"
	"fmt"

	"github.com/ebitengine/purego"
)

const (
	applicationServicesFramework = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	coreFoundationFramework      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	coreGraphicsFramework        = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"

	cgEventSourceStatePrivate         int32  = -1
	cgEventSourceStateCombinedSession int32  = 0
	cgHIDEventTap                     uint32 = 0
	cgScrollEventUnitPixel            uint32 = 0
	cgMouseEventClickState            uint32 = 1
	cgMouseEventDeltaX                uint32 = 4
	cgMouseEventDeltaY                uint32 = 5
)

type nativeSystem struct {
	applicationServicesHandle uintptr
	coreFoundationHandle      uintptr
	coreGraphicsHandle        uintptr
	eventSource               uintptr

	isProcessTrusted       func() bool
	eventCreate            func(uintptr) uintptr
	eventGetLocation       func(uintptr) point
	eventSourceCreate      func(int32) uintptr
	eventSourceButtonState func(int32, uint32) bool
	eventCreateMouse       func(uintptr, uint32, point, uint32) uintptr
	eventCreateScroll      func(uintptr, uint32, uint32, int32, int32, int32) uintptr
	eventSetIntegerField   func(uintptr, uint32, int64)
	eventPost              func(uint32, uintptr)
	cfRelease              func(uintptr)
}

func openNativeSystem() (inputSystem, error) {
	system := &nativeSystem{}
	var err error
	system.applicationServicesHandle, err = purego.Dlopen(
		applicationServicesFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: load ApplicationServices: %w", ErrUnsupported, err)
	}
	system.coreGraphicsHandle, err = purego.Dlopen(
		coreGraphicsFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		openErr := fmt.Errorf("%w: load CoreGraphics: %w", ErrUnsupported, err)
		return nil, errors.Join(openErr, system.Close())
	}
	system.coreFoundationHandle, err = purego.Dlopen(
		coreFoundationFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		openErr := fmt.Errorf("%w: load CoreFoundation: %w", ErrUnsupported, err)
		return nil, errors.Join(openErr, system.Close())
	}
	required := []struct {
		handle uintptr
		target any
		name   string
	}{
		{system.applicationServicesHandle, &system.isProcessTrusted, "AXIsProcessTrusted"},
		{system.coreGraphicsHandle, &system.eventCreate, "CGEventCreate"},
		{system.coreGraphicsHandle, &system.eventGetLocation, "CGEventGetLocation"},
		{system.coreGraphicsHandle, &system.eventSourceCreate, "CGEventSourceCreate"},
		{system.coreGraphicsHandle, &system.eventSourceButtonState, "CGEventSourceButtonState"},
		{system.coreGraphicsHandle, &system.eventCreateMouse, "CGEventCreateMouseEvent"},
		{system.coreGraphicsHandle, &system.eventCreateScroll, "CGEventCreateScrollWheelEvent2"},
		{system.coreGraphicsHandle, &system.eventSetIntegerField, "CGEventSetIntegerValueField"},
		{system.coreGraphicsHandle, &system.eventPost, "CGEventPost"},
		{system.coreFoundationHandle, &system.cfRelease, "CFRelease"},
	}
	for _, function := range required {
		symbol, symbolErr := purego.Dlsym(function.handle, function.name)
		if symbolErr != nil {
			openErr := fmt.Errorf("%w: resolve macOS symbol %s: %w", ErrUnsupported, function.name, symbolErr)
			return nil, errors.Join(openErr, system.Close())
		}
		purego.RegisterFunc(function.target, symbol)
	}
	// A private source keeps RobotGo's generated-event state independent from
	// other login-session sources. Ownership checks still query the combined
	// session table so physical and foreign synthetic holds are observable.
	system.eventSource = system.eventSourceCreate(cgEventSourceStatePrivate)
	if system.eventSource == 0 {
		openErr := fmt.Errorf("%w: CoreGraphics could not create a private event source", ErrUnsupported)
		return nil, errors.Join(openErr, system.Close())
	}
	return system, nil
}

func (system *nativeSystem) Ready() error {
	if !system.isProcessTrusted() {
		return fmt.Errorf(
			"%w: grant this application access in System Settings > Privacy & Security > Accessibility",
			ErrPermission,
		)
	}
	event := system.eventCreate(0)
	if event == 0 {
		return errors.New("CoreGraphics could not create a pointer-location event; an active macOS GUI session is required")
	}
	system.cfRelease(event)
	return nil
}

func (system *nativeSystem) CursorPosition() (point, error) {
	event := system.eventCreate(0)
	if event == 0 {
		return point{}, errors.New("CoreGraphics could not create a pointer-location event")
	}
	defer system.cfRelease(event)
	return system.eventGetLocation(event), nil
}

func (system *nativeSystem) ButtonDown(button uint32) (bool, error) {
	return system.eventSourceButtonState(cgEventSourceStateCombinedSession, button), nil
}

func (system *nativeSystem) PostMouse(
	eventType uint32,
	location point,
	button uint32,
	clickState int64,
) error {
	event := system.eventCreateMouse(system.eventSource, eventType, location, button)
	if event == 0 {
		return fmt.Errorf("CoreGraphics could not create mouse event type %d", eventType)
	}
	defer system.cfRelease(event)
	if clickState > 0 {
		system.eventSetIntegerField(event, cgMouseEventClickState, clickState)
	}
	switch eventType {
	case eventMouseMoved, eventLeftMouseDragged, eventRightMouseDragged, eventOtherMouseDragged:
		current, err := system.CursorPosition()
		if err != nil {
			return err
		}
		system.eventSetIntegerField(event, cgMouseEventDeltaX, int64(location.X-current.X))
		system.eventSetIntegerField(event, cgMouseEventDeltaY, int64(location.Y-current.Y))
	}
	system.eventPost(cgHIDEventTap, event)
	return nil
}

func (system *nativeSystem) PostScroll(horizontal, vertical int32) error {
	event := system.eventCreateScroll(
		system.eventSource,
		cgScrollEventUnitPixel,
		2,
		vertical,
		horizontal,
		0,
	)
	if event == 0 {
		return errors.New("CoreGraphics could not create scroll-wheel event")
	}
	defer system.cfRelease(event)
	system.eventPost(cgHIDEventTap, event)
	return nil
}

func (system *nativeSystem) Close() error {
	var closeErr error
	if system.eventSource != 0 && system.cfRelease != nil {
		system.cfRelease(system.eventSource)
		system.eventSource = 0
	}
	if system.coreFoundationHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(system.coreFoundationHandle))
		system.coreFoundationHandle = 0
	}
	if system.coreGraphicsHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(system.coreGraphicsHandle))
		system.coreGraphicsHandle = 0
	}
	if system.applicationServicesHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(system.applicationServicesHandle))
		system.applicationServicesHandle = 0
	}
	return closeErr
}
