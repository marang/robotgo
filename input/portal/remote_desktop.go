// Package portal provides consent-aware Wayland input through the
// freedesktop RemoteDesktop portal.
package portal

import "errors"

// DeviceType is a bitmask of input device types requested from the portal.
type DeviceType uint32

const (
	// DeviceKeyboard requests keyboard injection.
	DeviceKeyboard DeviceType = 1
	// DevicePointer requests pointer injection.
	DevicePointer DeviceType = 2
	// DeviceTouchscreen requests touchscreen injection. It requires at least one
	// ScreenCast source in OpenOptions.
	DeviceTouchscreen DeviceType = 4

	allDeviceTypes = DeviceKeyboard | DevicePointer | DeviceTouchscreen
)

// SourceType is a bitmask of ScreenCast sources attached to a RemoteDesktop session.
type SourceType uint32

const (
	// SourceMonitor allows the user to select an existing monitor.
	SourceMonitor SourceType = 1
	// SourceWindow allows the user to select an application window.
	SourceWindow SourceType = 2
	// SourceVirtual creates a virtual monitor when supported by the portal.
	SourceVirtual SourceType = 4

	allSourceTypes = SourceMonitor | SourceWindow | SourceVirtual
)

// CursorMode controls how the cursor is represented in a ScreenCast stream.
type CursorMode uint32

const (
	CursorHidden   CursorMode = 1
	CursorEmbedded CursorMode = 2
	CursorMetadata CursorMode = 4
	allCursorModes            = CursorHidden | CursorEmbedded | CursorMetadata
)

// PersistMode controls whether the portal may return a single-use restore token.
type PersistMode uint32

const (
	PersistNone        PersistMode = 0
	PersistApplication PersistMode = 1
	PersistExplicit    PersistMode = 2
)

// OpenOptions configures input devices and optional ScreenCast source mapping.
type OpenOptions struct {
	Devices      DeviceType
	Sources      SourceType
	Multiple     bool
	CursorMode   CursorMode
	PersistMode  PersistMode
	RestoreToken string
}

// Point is a logical compositor coordinate.
type Point struct{ X, Y int32 }

// Size is a logical compositor size.
type Size struct{ Width, Height int32 }

// Stream describes a ScreenCast stream attached to the RemoteDesktop session.
// NodeID is required by the portal Notify*Absolute methods. PipeWireSerial is
// preferred later when connecting to PipeWire for frame capture.
type Stream struct {
	NodeID         uint32
	ID             string
	Position       Point
	HasPosition    bool
	Size           Size
	HasSize        bool
	SourceType     SourceType
	MappingID      string
	PipeWireSerial uint64
}

// PointerAxis identifies a discrete pointer scroll axis.
type PointerAxis uint32

const (
	// PointerAxisVertical is the vertical scroll axis.
	PointerAxisVertical PointerAxis = 0
	// PointerAxisHorizontal is the horizontal scroll axis.
	PointerAxisHorizontal PointerAxis = 1
)

var (
	// ErrUnavailable indicates that the RemoteDesktop portal cannot satisfy the request.
	ErrUnavailable = errors.New("remote desktop portal unavailable")
	// ErrCancelled indicates that the user cancelled the portal interaction.
	ErrCancelled = errors.New("remote desktop portal request cancelled")
	// ErrRejected indicates that the portal rejected or otherwise failed a request.
	ErrRejected = errors.New("remote desktop portal request rejected")
	// ErrClosed indicates that the portal session is no longer active.
	ErrClosed = errors.New("remote desktop portal session closed")
	// ErrDeviceNotGranted indicates that the portal did not grant a requested device.
	ErrDeviceNotGranted = errors.New("remote desktop portal device not granted")
	// ErrScreenCastRequired indicates that an operation needs a selected stream.
	ErrScreenCastRequired = errors.New("remote desktop portal ScreenCast source required")
	// ErrStreamNotFound indicates that a stream ID is not part of the session.
	ErrStreamNotFound = errors.New("remote desktop portal stream not found")
)

// Capability describes the RemoteDesktop portal interface exposed at runtime.
type Capability struct {
	Version              uint32
	AvailableDevices     DeviceType
	ScreenCastVersion    uint32
	AvailableSources     SourceType
	AvailableCursorModes CursorMode
	// ScreenCastIssue describes an optional ScreenCast capability-probe failure.
	// RemoteDesktop keyboard and pointer support remains usable when this is set.
	ScreenCastIssue string
}

// Supports reports whether all requested device types are available.
func (c Capability) Supports(devices DeviceType) bool {
	return devices != 0 && c.AvailableDevices&devices == devices
}

// SupportsSources reports whether every requested ScreenCast source is advertised.
func (c Capability) SupportsSources(sources SourceType) bool {
	return sources != 0 && c.AvailableSources&sources == sources
}

// SupportsCursorMode reports whether a cursor representation is advertised.
func (c Capability) SupportsCursorMode(mode CursorMode) bool {
	return mode != 0 && mode&(mode-1) == 0 && c.AvailableCursorModes&mode == mode
}

func validateDevices(devices DeviceType) error {
	if devices == 0 || devices&^allDeviceTypes != 0 {
		return errors.New("remote desktop portal: invalid device mask")
	}
	return nil
}

func validateOptions(options OpenOptions) error {
	if err := validateDevices(options.Devices); err != nil {
		return err
	}
	if options.Sources&^allSourceTypes != 0 {
		return errors.New("remote desktop portal: invalid ScreenCast source mask")
	}
	if options.CursorMode&^allCursorModes != 0 || options.CursorMode != 0 && options.CursorMode&(options.CursorMode-1) != 0 {
		return errors.New("remote desktop portal: invalid cursor mode")
	}
	if options.PersistMode > PersistExplicit {
		return errors.New("remote desktop portal: invalid persist mode")
	}
	if options.Sources == 0 && options.Multiple {
		return errors.New("remote desktop portal: multiple selection requires ScreenCast sources")
	}
	if options.Sources == 0 && options.CursorMode != 0 {
		return errors.New("remote desktop portal: cursor mode requires ScreenCast sources")
	}
	if options.Devices&DeviceTouchscreen != 0 && options.Sources == 0 {
		return ErrScreenCastRequired
	}
	return nil
}
