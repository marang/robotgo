//go:build darwin

package darwinwindow

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

type nativeAPI struct {
	applicationServicesHandle uintptr
	coreFoundationHandle      uintptr
	coreGraphicsHandle        uintptr
	ownedCFValues             []uintptr

	axIsProcessTrusted                     func() bool
	axUIElementCreateApplication           func(int32) uintptr
	axUIElementCopyAttributeValue          func(uintptr, uintptr, *uintptr) int32
	axUIElementCopyAttributeValues         func(uintptr, uintptr, int64, int64, *uintptr) int32
	axUIElementGetAttributeValueCount      func(uintptr, uintptr, *int64) int32
	axUIElementGetPID                      func(uintptr, *int32) int32
	axUIElementGetTypeID                   func() uintptr
	axUIElementPerformAction               func(uintptr, uintptr) int32
	axUIElementSetAttributeValue           func(uintptr, uintptr, uintptr) int32
	axUIElementGetWindow                   func(uintptr, *uint32) int32
	axValueGetType                         func(uintptr) uint32
	axValueGetTypeID                       func() uintptr
	axValueGetValue                        func(uintptr, uint32, unsafe.Pointer) bool
	getFrontProcess                        func(*processSerialNumber) int32
	getProcessForPID                       func(int32, *processSerialNumber) int32
	getProcessPID                          func(*processSerialNumber, *int32) int32
	setFrontProcessWithOptions             func(*processSerialNumber, uint32) int32
	cgWindowListCreateDescriptionFromArray func(uintptr) uintptr
	cfArrayCreate                          func(uintptr, *uintptr, int64, uintptr) uintptr
	cfArrayGetCount                        func(uintptr) int64
	cfArrayGetValueAtIndex                 func(uintptr, int64) uintptr
	cfBooleanGetValue                      func(uintptr) bool
	cfBooleanGetTypeID                     func() uintptr
	cfDictionaryGetValue                   func(uintptr, uintptr) uintptr
	cfGetTypeID                            func(uintptr) uintptr
	cfNumberGetValue                       func(uintptr, int64, unsafe.Pointer) bool
	cfNumberGetTypeID                      func() uintptr
	cfRelease                              func(uintptr)
	cfRetain                               func(uintptr) uintptr
	cfStringGetCString                     func(uintptr, *byte, int64, uint32) bool
	cfStringCreateWithCString              func(uintptr, *byte, uint32) uintptr
	cfStringGetLength                      func(uintptr) int64
	cfStringGetMaximumSizeForEncoding      func(int64, uint32) int64
	cfStringGetTypeID                      func() uintptr

	axCloseButtonAttribute   uintptr
	axFocusedWindowAttribute uintptr
	axMainWindowAttribute    uintptr
	axMinimizedAttribute     uintptr
	axPositionAttribute      uintptr
	axPressAction            uintptr
	axRaiseAction            uintptr
	axSizeAttribute          uintptr
	axTitleAttribute         uintptr
	axWindowsAttribute       uintptr
	cfBooleanFalse           uintptr
	cfBooleanTrue            uintptr
	cgWindowOwnerPID         uintptr
}

func openNativeAPI() (*nativeAPI, error) {
	api := &nativeAPI{}
	var err error
	api.applicationServicesHandle, err = purego.Dlopen(
		applicationServicesFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: load ApplicationServices: %w", ErrUnsupported, err)
	}
	api.coreGraphicsHandle, err = purego.Dlopen(
		coreGraphicsFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("%w: load CoreGraphics: %w", ErrUnsupported, err),
			api.close(),
		)
	}
	api.coreFoundationHandle, err = purego.Dlopen(
		coreFoundationFramework,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("%w: load CoreFoundation: %w", ErrUnsupported, err),
			api.close(),
		)
	}
	functions := []struct {
		handle uintptr
		target any
		name   string
	}{
		{api.applicationServicesHandle, &api.axIsProcessTrusted, "AXIsProcessTrusted"},
		{api.applicationServicesHandle, &api.axUIElementCreateApplication, "AXUIElementCreateApplication"},
		{api.applicationServicesHandle, &api.axUIElementCopyAttributeValue, "AXUIElementCopyAttributeValue"},
		{api.applicationServicesHandle, &api.axUIElementCopyAttributeValues, "AXUIElementCopyAttributeValues"},
		{api.applicationServicesHandle, &api.axUIElementGetAttributeValueCount, "AXUIElementGetAttributeValueCount"},
		{api.applicationServicesHandle, &api.axUIElementGetPID, "AXUIElementGetPid"},
		{api.applicationServicesHandle, &api.axUIElementGetTypeID, "AXUIElementGetTypeID"},
		{api.applicationServicesHandle, &api.axUIElementPerformAction, "AXUIElementPerformAction"},
		{api.applicationServicesHandle, &api.axUIElementSetAttributeValue, "AXUIElementSetAttributeValue"},
		{api.applicationServicesHandle, &api.axUIElementGetWindow, "_AXUIElementGetWindow"},
		{api.applicationServicesHandle, &api.axValueGetType, "AXValueGetType"},
		{api.applicationServicesHandle, &api.axValueGetTypeID, "AXValueGetTypeID"},
		{api.applicationServicesHandle, &api.axValueGetValue, "AXValueGetValue"},
		{api.applicationServicesHandle, &api.getFrontProcess, "GetFrontProcess"},
		{api.applicationServicesHandle, &api.getProcessForPID, "GetProcessForPID"},
		{api.applicationServicesHandle, &api.getProcessPID, "GetProcessPID"},
		{api.applicationServicesHandle, &api.setFrontProcessWithOptions, "SetFrontProcessWithOptions"},
		{api.coreGraphicsHandle, &api.cgWindowListCreateDescriptionFromArray, "CGWindowListCreateDescriptionFromArray"},
		{api.coreFoundationHandle, &api.cfArrayCreate, "CFArrayCreate"},
		{api.coreFoundationHandle, &api.cfArrayGetCount, "CFArrayGetCount"},
		{api.coreFoundationHandle, &api.cfArrayGetValueAtIndex, "CFArrayGetValueAtIndex"},
		{api.coreFoundationHandle, &api.cfBooleanGetValue, "CFBooleanGetValue"},
		{api.coreFoundationHandle, &api.cfBooleanGetTypeID, "CFBooleanGetTypeID"},
		{api.coreFoundationHandle, &api.cfDictionaryGetValue, "CFDictionaryGetValue"},
		{api.coreFoundationHandle, &api.cfGetTypeID, "CFGetTypeID"},
		{api.coreFoundationHandle, &api.cfNumberGetValue, "CFNumberGetValue"},
		{api.coreFoundationHandle, &api.cfNumberGetTypeID, "CFNumberGetTypeID"},
		{api.coreFoundationHandle, &api.cfRelease, "CFRelease"},
		{api.coreFoundationHandle, &api.cfRetain, "CFRetain"},
		{api.coreFoundationHandle, &api.cfStringCreateWithCString, "CFStringCreateWithCString"},
		{api.coreFoundationHandle, &api.cfStringGetCString, "CFStringGetCString"},
		{api.coreFoundationHandle, &api.cfStringGetLength, "CFStringGetLength"},
		{api.coreFoundationHandle, &api.cfStringGetMaximumSizeForEncoding, "CFStringGetMaximumSizeForEncoding"},
		{api.coreFoundationHandle, &api.cfStringGetTypeID, "CFStringGetTypeID"},
	}
	for _, function := range functions {
		if err := bindNativeFunction(function.handle, function.target, function.name); err != nil {
			return nil, errors.Join(err, api.close())
		}
	}
	values := []struct {
		handle uintptr
		target *uintptr
		name   string
	}{
		{api.coreFoundationHandle, &api.cfBooleanFalse, "kCFBooleanFalse"},
		{api.coreFoundationHandle, &api.cfBooleanTrue, "kCFBooleanTrue"},
		{api.coreGraphicsHandle, &api.cgWindowOwnerPID, "kCGWindowOwnerPID"},
	}
	for _, value := range values {
		if err := bindNativeValue(value.handle, value.target, value.name); err != nil {
			return nil, errors.Join(err, api.close())
		}
	}
	strings := []struct {
		target *uintptr
		value  string
	}{
		{&api.axCloseButtonAttribute, axCloseButtonAttributeName},
		{&api.axFocusedWindowAttribute, axFocusedWindowAttributeName},
		{&api.axMainWindowAttribute, axMainWindowAttributeName},
		{&api.axMinimizedAttribute, axMinimizedAttributeName},
		{&api.axPositionAttribute, axPositionAttributeName},
		{&api.axPressAction, axPressActionName},
		{&api.axRaiseAction, axRaiseActionName},
		{&api.axSizeAttribute, axSizeAttributeName},
		{&api.axTitleAttribute, axTitleAttributeName},
		{&api.axWindowsAttribute, axWindowsAttributeName},
	}
	for _, value := range strings {
		ref, stringErr := api.createString(value.value)
		if stringErr != nil {
			return nil, errors.Join(stringErr, api.close())
		}
		*value.target = ref
	}
	return api, nil
}

func (api *nativeAPI) createString(value string) (uintptr, error) {
	bytes := append([]byte(value), 0)
	ref := api.cfStringCreateWithCString(
		0,
		&bytes[0],
		cfStringEncodingUTF8,
	)
	runtime.KeepAlive(bytes)
	if ref == 0 {
		return 0, fmt.Errorf(
			"%w: create macOS string constant %q",
			ErrUnsupported,
			value,
		)
	}
	api.ownedCFValues = append(api.ownedCFValues, ref)
	return ref, nil
}

func bindNativeFunction(handle uintptr, target any, name string) error {
	symbol, err := purego.Dlsym(handle, name)
	if err != nil {
		return fmt.Errorf("%w: resolve macOS function %s: %w", ErrUnsupported, name, err)
	}
	purego.RegisterFunc(target, symbol)
	return nil
}

func bindNativeValue(handle uintptr, target *uintptr, name string) error {
	symbol, err := purego.Dlsym(handle, name)
	if err != nil {
		return fmt.Errorf("%w: resolve macOS value %s: %w", ErrUnsupported, name, err)
	}
	value := *(*uintptr)(unsafe.Pointer(symbol))
	if value == 0 {
		return fmt.Errorf("%w: macOS value %s is nil", ErrUnsupported, name)
	}
	*target = value
	return nil
}

func (api *nativeAPI) close() error {
	var closeErr error
	if api.cfRelease != nil {
		for index := len(api.ownedCFValues) - 1; index >= 0; index-- {
			api.cfRelease(api.ownedCFValues[index])
		}
	}
	api.ownedCFValues = nil
	if api.coreFoundationHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(api.coreFoundationHandle))
		api.coreFoundationHandle = 0
	}
	if api.coreGraphicsHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(api.coreGraphicsHandle))
		api.coreGraphicsHandle = 0
	}
	if api.applicationServicesHandle != 0 {
		closeErr = errors.Join(closeErr, purego.Dlclose(api.applicationServicesHandle))
		api.applicationServicesHandle = 0
	}
	return closeErr
}
