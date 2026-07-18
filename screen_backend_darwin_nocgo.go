//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"image"
	"math"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	cgErrorSuccess                 int32  = 0
	cgWindowListOptionOnScreenOnly uint32 = 1 << 0
	cgWindowImageDefault           uint32 = 0
	cgImageAlphaPremultipliedLast  uint32 = 1
	cgBitmapByteOrder32Big         uint32 = 4 << 12
)

type cgPoint struct {
	X float64
	Y float64
}

type cgSize struct {
	Width  float64
	Height float64
}

type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

type darwinGraphicsAPI struct {
	close                     func() error
	preflightCaptureAccess    func() bool
	getActiveDisplayList      func(uint32, *uint32, *uint32) int32
	displayBounds             func(uint32) cgRect
	mainDisplayID             func() uint32
	displayCopyDisplayMode    func(uint32) uintptr
	displayModeGetPixelWidth  func(uintptr) uintptr
	displayModeGetWidth       func(uintptr) uintptr
	displayModeRelease        func(uintptr)
	windowListCreateImage     func(cgRect, uint32, uint32, uint32) uintptr
	imageRelease              func(uintptr)
	colorSpaceCreateDeviceRGB func() uintptr
	colorSpaceRelease         func(uintptr)
	bitmapContextCreate       func(unsafe.Pointer, uintptr, uintptr, uintptr, uintptr, uintptr, uint32) uintptr
	bitmapContextGetData      func(uintptr) unsafe.Pointer
	contextRelease            func(uintptr)
	contextTranslateCTM       func(uintptr, float64, float64)
	contextScaleCTM           func(uintptr, float64, float64)
	contextDrawImage          func(uintptr, cgRect, uintptr)
}

var openDarwinGraphics = openDarwinCoreGraphics

func openDarwinCoreGraphics() (*darwinGraphicsAPI, error) {
	handle, err := purego.Dlopen(coreGraphicsFramework, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreGraphics: %w", err)
	}
	api := &darwinGraphicsAPI{close: func() error { return purego.Dlclose(handle) }}
	bind := func(target any, name string) error {
		symbol, err := purego.Dlsym(handle, name)
		if err != nil {
			return fmt.Errorf("resolve CoreGraphics symbol %s: %w", name, err)
		}
		purego.RegisterFunc(target, symbol)
		return nil
	}
	required := []struct {
		target any
		name   string
	}{
		{&api.getActiveDisplayList, "CGGetActiveDisplayList"},
		{&api.displayBounds, "CGDisplayBounds"},
		{&api.windowListCreateImage, "CGWindowListCreateImage"},
		{&api.imageRelease, "CGImageRelease"},
		{&api.colorSpaceCreateDeviceRGB, "CGColorSpaceCreateDeviceRGB"},
		{&api.colorSpaceRelease, "CGColorSpaceRelease"},
		{&api.bitmapContextCreate, "CGBitmapContextCreate"},
		{&api.bitmapContextGetData, "CGBitmapContextGetData"},
		{&api.contextRelease, "CGContextRelease"},
		{&api.contextTranslateCTM, "CGContextTranslateCTM"},
		{&api.contextScaleCTM, "CGContextScaleCTM"},
		{&api.contextDrawImage, "CGContextDrawImage"},
	}
	for _, function := range required {
		if err := bind(function.target, function.name); err != nil {
			_ = api.close()
			return nil, errors.Join(ErrNotSupported, err)
		}
	}
	scaleFunctions := []struct {
		target any
		name   string
	}{
		{&api.mainDisplayID, "CGMainDisplayID"},
		{&api.displayCopyDisplayMode, "CGDisplayCopyDisplayMode"},
		{&api.displayModeGetPixelWidth, "CGDisplayModeGetPixelWidth"},
		{&api.displayModeGetWidth, "CGDisplayModeGetWidth"},
		{&api.displayModeRelease, "CGDisplayModeRelease"},
	}
	for _, function := range scaleFunctions {
		if err := bind(function.target, function.name); err != nil {
			api.mainDisplayID = nil
			api.displayCopyDisplayMode = nil
			api.displayModeGetPixelWidth = nil
			api.displayModeGetWidth = nil
			api.displayModeRelease = nil
			break
		}
	}
	if symbol, err := purego.Dlsym(handle, "CGPreflightScreenCaptureAccess"); err == nil {
		purego.RegisterFunc(&api.preflightCaptureAccess, symbol)
	}
	return api, nil
}

func pureGoPlatformCaptureCapabilities() (FeatureCapability, FeatureCapability) {
	const backend = featureBackendPureGoCoreGraphics
	capture := FeatureCapability{Backend: backend}
	bounds := FeatureCapability{Backend: backend}
	api, err := openDarwinGraphics()
	if err != nil {
		capture.Reason = err.Error()
		capture.Notes = ErrNotSupported.Error()
		bounds.Reason = capture.Reason
		bounds.Notes = capture.Notes
		return capture, bounds
	}
	defer func() { _ = api.close() }()
	count, err := darwinDisplayCount(api)
	if err != nil || count == 0 {
		if err != nil {
			capture.Reason = err.Error()
		} else {
			capture.Reason = "CoreGraphics reports no active displays"
		}
		capture.Notes = "an active macOS GUI session is required"
		bounds.Reason = capture.Reason
		bounds.Notes = capture.Notes
		return capture, bounds
	}
	bounds.Available = true
	bounds.Reason = "CoreGraphics display enumeration is available"
	bounds.Notes = fmt.Sprintf("active displays=%d", count)
	if api.preflightCaptureAccess != nil && !api.preflightCaptureAccess() {
		capture.Reason = ErrPermissionDenied.Error()
		capture.Notes = "grant Screen Recording access to this application in System Settings"
		return capture, bounds
	}
	capture.Available = true
	capture.Reason = "CoreGraphics capture is available without CGO"
	capture.Notes = "Screen Recording permission is granted or cannot be preflighted on this macOS version"
	return capture, bounds
}

func platformDisplayCount() int {
	api, err := openDarwinGraphics()
	if err != nil {
		return 0
	}
	defer func() { _ = api.close() }()
	count, err := darwinDisplayCount(api)
	if err != nil {
		return 0
	}
	return count
}

func platformDisplayBoundsE(displayIndex int) (image.Rectangle, error) {
	api, err := openDarwinGraphics()
	if err != nil {
		return image.Rectangle{}, err
	}
	defer func() { _ = api.close() }()
	bounds, err := darwinDisplayBounds(api, displayIndex)
	if err != nil {
		return image.Rectangle{}, err
	}
	return bounds, nil
}

func platformCapture(x, y, width, height int) (*image.RGBA, error) {
	api, err := openDarwinGraphics()
	if err != nil {
		return nil, err
	}
	defer func() { _ = api.close() }()
	return captureDarwinWithAPI(api, image.Rect(x, y, x+width, y+height))
}

func platformDarwinScale(displayID ...int) float64 {
	api, err := openDarwinGraphics()
	if err != nil {
		return 1
	}
	defer func() { _ = api.close() }()
	scale, err := darwinDisplayScale(api, displayID...)
	if err != nil {
		return 1
	}
	return scale
}

func darwinDisplayCount(api *darwinGraphicsAPI) (int, error) {
	var count uint32
	if result := api.getActiveDisplayList(0, nil, &count); result != cgErrorSuccess {
		return 0, fmt.Errorf("query active displays: CoreGraphics error %d", result)
	}
	return int(count), nil
}

func darwinDisplayBounds(api *darwinGraphicsAPI, displayIndex int) (image.Rectangle, error) {
	if displayIndex < 0 {
		return image.Rectangle{}, fmt.Errorf("invalid display index %d", displayIndex)
	}
	count, err := darwinDisplayCount(api)
	if err != nil {
		return image.Rectangle{}, err
	}
	if displayIndex >= count {
		return image.Rectangle{}, fmt.Errorf("display index %d out of range (active displays: %d)", displayIndex, count)
	}
	displays := make([]uint32, count)
	written := uint32(count)
	if result := api.getActiveDisplayList(uint32(count), &displays[0], &written); result != cgErrorSuccess {
		return image.Rectangle{}, fmt.Errorf("list active displays: CoreGraphics error %d", result)
	}
	if displayIndex >= int(written) {
		return image.Rectangle{}, fmt.Errorf("display index %d disappeared during enumeration", displayIndex)
	}
	bounds := api.displayBounds(displays[displayIndex])
	minX := int(math.Floor(bounds.Origin.X))
	minY := int(math.Floor(bounds.Origin.Y))
	maxX := int(math.Ceil(bounds.Origin.X + bounds.Size.Width))
	maxY := int(math.Ceil(bounds.Origin.Y + bounds.Size.Height))
	result := image.Rect(minX, minY, maxX, maxY)
	if result.Empty() {
		return image.Rectangle{}, fmt.Errorf("display %d returned empty bounds", displayIndex)
	}
	return result, nil
}

func darwinDisplayScale(api *darwinGraphicsAPI, displayID ...int) (float64, error) {
	if api.mainDisplayID == nil ||
		api.displayCopyDisplayMode == nil ||
		api.displayModeGetPixelWidth == nil ||
		api.displayModeGetWidth == nil ||
		api.displayModeRelease == nil {
		return 0, fmt.Errorf("%w: CoreGraphics display-mode scale symbols are unavailable", ErrNotSupported)
	}

	id := api.mainDisplayID()
	if len(displayID) > 0 && displayID[0] != -1 {
		if displayID[0] < 0 || uint64(displayID[0]) > uint64(^uint32(0)) {
			return 0, fmt.Errorf("invalid CoreGraphics display ID %d", displayID[0])
		}
		id = uint32(displayID[0])
	}
	mode := api.displayCopyDisplayMode(id)
	if mode == 0 {
		return 0, fmt.Errorf("CoreGraphics returned no display mode for display %d", id)
	}
	defer api.displayModeRelease(mode)

	pixelWidth := api.displayModeGetPixelWidth(mode)
	logicalWidth := api.displayModeGetWidth(mode)
	if pixelWidth == 0 || logicalWidth == 0 {
		return 0, fmt.Errorf(
			"CoreGraphics returned invalid display-mode widths for display %d: pixels=%d logical=%d",
			id,
			pixelWidth,
			logicalWidth,
		)
	}
	scale := float64(pixelWidth) / float64(logicalWidth)
	if !(scale > 0) || math.IsInf(scale, 0) || math.IsNaN(scale) {
		return 0, fmt.Errorf("CoreGraphics returned invalid display scale %v for display %d", scale, id)
	}
	return scale, nil
}

func captureDarwinWithAPI(api *darwinGraphicsAPI, region image.Rectangle) (*image.RGBA, error) {
	if region.Empty() {
		return nil, fmt.Errorf("invalid macOS capture region %v", region)
	}
	if api.preflightCaptureAccess != nil && !api.preflightCaptureAccess() {
		return nil, fmt.Errorf("%w: grant Screen Recording access to this application in System Settings", ErrPermissionDenied)
	}
	cgRegion := cgRect{
		Origin: cgPoint{X: float64(region.Min.X), Y: float64(region.Min.Y)},
		Size:   cgSize{Width: float64(region.Dx()), Height: float64(region.Dy())},
	}
	captured := api.windowListCreateImage(
		cgRegion,
		cgWindowListOptionOnScreenOnly,
		0,
		cgWindowImageDefault,
	)
	if captured == 0 {
		return nil, errors.New("CoreGraphics returned no screen image; verify Screen Recording permission and an active GUI session")
	}
	defer api.imageRelease(captured)

	result := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	colorSpace := api.colorSpaceCreateDeviceRGB()
	if colorSpace == 0 {
		return nil, errors.New("CoreGraphics could not create an RGB color space")
	}
	defer api.colorSpaceRelease(colorSpace)
	context := api.bitmapContextCreate(
		nil,
		uintptr(result.Rect.Dx()),
		uintptr(result.Rect.Dy()),
		8,
		uintptr(result.Stride),
		colorSpace,
		cgImageAlphaPremultipliedLast|cgBitmapByteOrder32Big,
	)
	if context == 0 {
		return nil, errors.New("CoreGraphics could not create an RGBA bitmap context")
	}
	defer api.contextRelease(context)

	api.contextTranslateCTM(context, 0, float64(result.Rect.Dy()))
	api.contextScaleCTM(context, 1, -1)
	api.contextDrawImage(context, cgRect{Size: cgSize{
		Width:  float64(result.Rect.Dx()),
		Height: float64(result.Rect.Dy()),
	}}, captured)
	data := api.bitmapContextGetData(context)
	if data == nil {
		return nil, errors.New("CoreGraphics bitmap context returned no pixel buffer")
	}
	copy(result.Pix, unsafe.Slice((*byte)(data), len(result.Pix)))
	return result, nil
}
