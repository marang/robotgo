//go:build darwin

package darwinwindow

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"github.com/marang/robotgo/internal/windowbackend"
)

func applicationWindowLocked(api *nativeAPI, pid int32) (windowbackend.Handle, error) {
	application := api.axUIElementCreateApplication(pid)
	if application == 0 {
		return 0, fmt.Errorf("%w: AXUIElementCreateApplication(%d)", errWindowUnavailable, pid)
	}
	defer api.cfRelease(application)
	attributes := []uintptr{api.axFocusedWindowAttribute, api.axMainWindowAttribute}
	var lastErr error
	for _, attribute := range attributes {
		element, err := copyAttributeLocked(api, application, attribute)
		if err != nil {
			lastErr = errors.Join(lastErr, err)
			continue
		}
		if err := requireCFType(
			api,
			element,
			api.axUIElementGetTypeID(),
			"focused/main AX window",
		); err != nil {
			api.cfRelease(element)
			lastErr = errors.Join(lastErr, err)
			continue
		}
		var windowID uint32
		result := api.axUIElementGetWindow(element, &windowID)
		api.cfRelease(element)
		if result == axErrorSuccess {
			if windowID != 0 {
				return windowbackend.Handle(windowID), nil
			}
			lastErr = errors.Join(
				lastErr,
				errors.New("map AX window returned a zero CGWindowID"),
			)
			continue
		}
		lastErr = errors.Join(lastErr, axCallError("map AX window to CGWindowID", result))
	}
	return 0, fmt.Errorf("%w: pid %d has no focused or main window: %w", errWindowUnavailable, pid, lastErr)
}

func windowElementLocked(api *nativeAPI, handle windowbackend.Handle) (uintptr, error) {
	pid, err := windowPIDLocked(api, handle)
	if err != nil {
		return 0, err
	}
	application := api.axUIElementCreateApplication(pid)
	if application == 0 {
		return 0, fmt.Errorf("%w: create AX application for pid %d", errWindowUnavailable, pid)
	}
	defer api.cfRelease(application)
	var count int64
	if result := api.axUIElementGetAttributeValueCount(
		application,
		api.axWindowsAttribute,
		&count,
	); result != axErrorSuccess {
		return 0, classifyWindowLookupError(
			"count AX windows",
			axCallError("count AX windows", result),
		)
	}
	if count <= 0 || count > maximumAXWindows {
		return 0, fmt.Errorf(
			"%w: AX window count %d is outside 1..%d",
			errWindowUnavailable,
			count,
			maximumAXWindows,
		)
	}
	var windows uintptr
	result := api.axUIElementCopyAttributeValues(
		application,
		api.axWindowsAttribute,
		0,
		count,
		&windows,
	)
	if result != axErrorSuccess {
		return 0, classifyWindowLookupError(
			fmt.Sprintf("copy AX windows for pid %d", pid),
			axCallError("copy AX windows", result),
		)
	}
	if windows == 0 {
		return 0, fmt.Errorf(
			"%w: copy AX windows for pid %d returned a nil array",
			errWindowUnavailable,
			pid,
		)
	}
	defer api.cfRelease(windows)
	count = api.cfArrayGetCount(windows)
	var mappingErr error
	for index := int64(0); index < count; index++ {
		element := api.cfArrayGetValueAtIndex(windows, index)
		if element == 0 {
			continue
		}
		if requireCFType(
			api,
			element,
			api.axUIElementGetTypeID(),
			"AXWindows element",
		) != nil {
			continue
		}
		var candidate uint32
		result := api.axUIElementGetWindow(element, &candidate)
		if result == axErrorSuccess && windowbackend.Handle(candidate) == handle {
			return api.cfRetain(element), nil
		}
		if result != axErrorSuccess {
			err := axCallError("map AX window to CGWindowID", result)
			if errors.Is(err, ErrPermission) || errors.Is(err, ErrUnsupported) {
				return 0, err
			}
			mappingErr = errors.Join(mappingErr, err)
		}
	}
	err = fmt.Errorf(
		"%w: CGWindowID %#x has no matching AX window",
		errWindowUnavailable,
		uintptr(handle),
	)
	return 0, errors.Join(err, mappingErr)
}

func windowPIDLocked(api *nativeAPI, handle windowbackend.Handle) (int32, error) {
	if handle == 0 || uint64(handle) > math.MaxUint32 {
		return 0, fmt.Errorf("%w: invalid CGWindowID %#x", errWindowUnavailable, uintptr(handle))
	}
	windowID := uintptr(uint32(handle))
	windowList := api.cfArrayCreate(0, &windowID, 1, 0)
	runtime.KeepAlive(&windowID)
	if windowList == 0 {
		return 0, fmt.Errorf(
			"%w: create CoreGraphics request for CGWindowID %#x",
			errWindowUnavailable,
			uintptr(handle),
		)
	}
	defer api.cfRelease(windowList)
	info := api.cgWindowListCreateDescriptionFromArray(windowList)
	if info == 0 {
		return 0, fmt.Errorf("%w: no CoreGraphics description for CGWindowID %#x", errWindowUnavailable, uintptr(handle))
	}
	defer api.cfRelease(info)
	if api.cfArrayGetCount(info) < 1 {
		return 0, fmt.Errorf("%w: empty CoreGraphics description for CGWindowID %#x", errWindowUnavailable, uintptr(handle))
	}
	description := api.cfArrayGetValueAtIndex(info, 0)
	if description == 0 {
		return 0, fmt.Errorf("%w: nil CoreGraphics description for CGWindowID %#x", errWindowUnavailable, uintptr(handle))
	}
	number := api.cfDictionaryGetValue(description, api.cgWindowOwnerPID)
	if number == 0 {
		return 0, fmt.Errorf("%w: CGWindowID %#x has no owner pid", errWindowUnavailable, uintptr(handle))
	}
	if err := requireCFType(api, number, api.cfNumberGetTypeID(), "kCGWindowOwnerPID"); err != nil {
		return 0, fmt.Errorf("%w: %w", errWindowUnavailable, err)
	}
	var pid int32
	if !api.cfNumberGetValue(number, cfNumberSInt32Type, unsafe.Pointer(&pid)) || pid <= 0 {
		return 0, fmt.Errorf("%w: CGWindowID %#x has invalid owner pid %d", errWindowUnavailable, uintptr(handle), pid)
	}
	return pid, nil
}

func copyAttributeLocked(api *nativeAPI, element, attribute uintptr) (uintptr, error) {
	var value uintptr
	result := api.axUIElementCopyAttributeValue(element, attribute, &value)
	if result != axErrorSuccess {
		return 0, axCallError("copy AX attribute", result)
	}
	if value == 0 {
		return 0, errors.New("copy AX attribute returned a nil value")
	}
	return value, nil
}

func classifyWindowLookupError(context string, err error) error {
	if errors.Is(err, ErrPermission) || errors.Is(err, ErrUnsupported) {
		return err
	}
	return fmt.Errorf("%w: %s: %w", errWindowUnavailable, context, err)
}

func cfStringLocked(api *nativeAPI, value uintptr) (string, error) {
	if err := requireCFType(api, value, api.cfStringGetTypeID(), "AX title"); err != nil {
		return "", err
	}
	length := api.cfStringGetLength(value)
	if length < 0 || length >= maximumAXStringBytes {
		return "", fmt.Errorf("CoreFoundation returned invalid string length %d", length)
	}
	maximum := api.cfStringGetMaximumSizeForEncoding(length, cfStringEncodingUTF8)
	if maximum < 0 || maximum >= maximumAXStringBytes {
		return "", fmt.Errorf("CoreFoundation UTF-8 string size %d is invalid", maximum)
	}
	buffer := make([]byte, maximum+1)
	if !api.cfStringGetCString(value, &buffer[0], int64(len(buffer)), cfStringEncodingUTF8) {
		return "", errors.New("CFStringGetCString could not encode AX title as UTF-8")
	}
	runtime.KeepAlive(buffer)
	for index, value := range buffer {
		if value == 0 {
			return string(buffer[:index]), nil
		}
	}
	return "", errors.New("CFStringGetCString did not terminate AX title")
}

func requireCFType(api *nativeAPI, value, expected uintptr, name string) error {
	if value == 0 || expected == 0 {
		return fmt.Errorf("%s has an invalid CoreFoundation reference", name)
	}
	if actual := api.cfGetTypeID(value); actual != expected {
		return fmt.Errorf(
			"%s has CoreFoundation type %d, want %d",
			name,
			actual,
			expected,
		)
	}
	return nil
}

func axCallError(operation string, result int32) error {
	if result == axErrorSuccess {
		return nil
	}
	switch result {
	case axErrorAPIDisabled:
		return fmt.Errorf("%w: %s failed with AXError %d", ErrPermission, operation, result)
	case axErrorAttributeUnsupported,
		axErrorActionUnsupported,
		axErrorNotImplemented,
		axErrorParameterizedAttributeUnsupported:
		return fmt.Errorf("%w: %s failed with AXError %d", ErrUnsupported, operation, result)
	case axErrorInvalidUIElement:
		return fmt.Errorf("%w: %s failed with AXError %d", errWindowUnavailable, operation, result)
	default:
		return fmt.Errorf("%s failed with AXError %d", operation, result)
	}
}

func enclosingRect(position point, dimensions size) (windowbackend.Rect, error) {
	values := []float64{position.X, position.Y, dimensions.Width, dimensions.Height}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			value < -maximumExactCoordinate || value > maximumExactCoordinate {
			return windowbackend.Rect{}, fmt.Errorf("macOS Accessibility returned invalid window geometry %v", values)
		}
	}
	if dimensions.Width <= 0 || dimensions.Height <= 0 {
		return windowbackend.Rect{}, fmt.Errorf("macOS Accessibility returned non-positive window size %v", dimensions)
	}
	minX := math.Floor(position.X)
	minY := math.Floor(position.Y)
	maxX := math.Ceil(position.X + dimensions.Width)
	maxY := math.Ceil(position.Y + dimensions.Height)
	if maxX > maximumExactCoordinate || maxY > maximumExactCoordinate ||
		minX < -maximumExactCoordinate || minY < -maximumExactCoordinate {
		return windowbackend.Rect{}, fmt.Errorf("macOS Accessibility window edges cannot be represented exactly")
	}
	return windowbackend.Rect{
		X:      int(minX),
		Y:      int(minY),
		Width:  int(maxX - minX),
		Height: int(maxY - minY),
	}, nil
}
