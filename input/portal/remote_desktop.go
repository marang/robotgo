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

	allDeviceTypes = DeviceKeyboard | DevicePointer
)

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
)

// Capability describes the RemoteDesktop portal interface exposed at runtime.
type Capability struct {
	Version          uint32
	AvailableDevices DeviceType
}

// Supports reports whether all requested device types are available.
func (c Capability) Supports(devices DeviceType) bool {
	return devices != 0 && c.AvailableDevices&devices == devices
}

func validateDevices(devices DeviceType) error {
	if devices == 0 || devices&^allDeviceTypes != 0 {
		return errors.New("remote desktop portal: invalid device mask")
	}
	return nil
}
