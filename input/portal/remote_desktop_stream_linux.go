//go:build linux

package portal

import (
	"context"
	"fmt"
	"math"

	"github.com/godbus/dbus/v5"
)

type rawStream struct {
	NodeID     uint32
	Properties map[string]dbus.Variant
}

type dbusPoint struct {
	X int32
	Y int32
}

func resultStreams(results map[string]dbus.Variant) ([]Stream, error) {
	value, ok := results["streams"]
	if !ok {
		return nil, nil
	}
	var raw []rawStream
	if err := value.Store(&raw); err != nil {
		return nil, fmt.Errorf("remote desktop portal: invalid streams: %w", err)
	}
	streams := make([]Stream, 0, len(raw))
	seen := make(map[uint32]struct{}, len(raw))
	for _, item := range raw {
		if _, duplicate := seen[item.NodeID]; duplicate {
			return nil, fmt.Errorf("remote desktop portal: duplicate stream node ID %d", item.NodeID)
		}
		seen[item.NodeID] = struct{}{}
		stream := Stream{NodeID: item.NodeID}
		if err := decodeStreamProperties(&stream, item.Properties); err != nil {
			return nil, fmt.Errorf("remote desktop portal: stream %d: %w", item.NodeID, err)
		}
		streams = append(streams, stream)
	}
	return streams, nil
}

func decodeStreamProperties(stream *Stream, properties map[string]dbus.Variant) error {
	if value, ok := properties["id"]; ok {
		if err := value.Store(&stream.ID); err != nil {
			return fmt.Errorf("invalid id: %w", err)
		}
	}
	if value, ok := properties["position"]; ok {
		var position dbusPoint
		if err := value.Store(&position); err != nil {
			return fmt.Errorf("invalid position: %w", err)
		}
		stream.Position = Point(position)
		stream.HasPosition = true
	}
	for _, key := range []string{"size", "logical_size"} {
		value, ok := properties[key]
		if !ok {
			continue
		}
		var size dbusPoint
		if err := value.Store(&size); err != nil {
			return fmt.Errorf("invalid %s: %w", key, err)
		}
		if size.X <= 0 || size.Y <= 0 {
			return fmt.Errorf("invalid %s dimensions %dx%d", key, size.X, size.Y)
		}
		stream.Size = Size{Width: size.X, Height: size.Y}
		stream.HasSize = true
		break
	}
	if value, ok := properties["source_type"]; ok {
		var source uint32
		if err := value.Store(&source); err != nil {
			return fmt.Errorf("invalid source_type: %w", err)
		}
		stream.SourceType = SourceType(source) & allSourceTypes
	}
	if value, ok := properties["mapping_id"]; ok {
		if err := value.Store(&stream.MappingID); err != nil {
			return fmt.Errorf("invalid mapping_id: %w", err)
		}
	}
	if value, ok := properties["pipewire-serial"]; ok {
		if err := value.Store(&stream.PipeWireSerial); err != nil {
			return fmt.Errorf("invalid pipewire-serial: %w", err)
		}
	}
	return nil
}

func resultOptionalString(results map[string]dbus.Variant, key string) (string, error) {
	value, ok := results[key]
	if !ok {
		return "", nil
	}
	var result string
	if err := value.Store(&result); err != nil {
		return "", fmt.Errorf("remote desktop portal: invalid %s: %w", key, err)
	}
	return result, nil
}

// Streams returns a copy of the ScreenCast streams attached to the session.
func (s *Session) Streams() []Stream {
	if s == nil {
		return nil
	}
	return append([]Stream(nil), s.streams...)
}

// RestoreToken returns the single-use token supplied by a persistent portal
// session, or an empty string when persistence was not granted.
func (s *Session) RestoreToken() string {
	if s == nil {
		return ""
	}
	return s.restoreToken
}

// PointerMotionAbsolute moves the pointer within a selected ScreenCast stream's
// logical coordinate space.
func (s *Session) PointerMotionAbsolute(ctx context.Context, stream uint32, x, y float64) error {
	if err := s.ensureStreamCoordinate(stream, x, y); err != nil {
		return err
	}
	return s.notify(ctx, DevicePointer, notifyPointerAbsolute, stream, x, y)
}

// TouchDown starts a touch contact in a selected ScreenCast stream.
func (s *Session) TouchDown(ctx context.Context, stream, slot uint32, x, y float64) error {
	if err := s.ensureStreamCoordinate(stream, x, y); err != nil {
		return err
	}
	return s.notify(ctx, DeviceTouchscreen, notifyTouchDown, stream, slot, x, y)
}

// TouchMotion moves an active touch contact in a selected ScreenCast stream.
func (s *Session) TouchMotion(ctx context.Context, stream, slot uint32, x, y float64) error {
	if err := s.ensureStreamCoordinate(stream, x, y); err != nil {
		return err
	}
	return s.notify(ctx, DeviceTouchscreen, notifyTouchMotion, stream, slot, x, y)
}

// TouchUp ends an active touch contact.
func (s *Session) TouchUp(ctx context.Context, slot uint32) error {
	return s.notify(ctx, DeviceTouchscreen, notifyTouchUp, slot)
}

func (s *Session) ensureStreamCoordinate(nodeID uint32, x, y float64) error {
	if len(s.streams) == 0 {
		return ErrScreenCastRequired
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return fmt.Errorf("remote desktop portal: non-finite stream coordinate (%g,%g)", x, y)
	}
	for _, stream := range s.streams {
		if stream.NodeID != nodeID {
			continue
		}
		if x < 0 || y < 0 {
			return fmt.Errorf("remote desktop portal: negative stream coordinate (%g,%g)", x, y)
		}
		if stream.HasSize && (x >= float64(stream.Size.Width) || y >= float64(stream.Size.Height)) {
			return fmt.Errorf("remote desktop portal: coordinate (%g,%g) outside stream %d size %dx%d", x, y, nodeID, stream.Size.Width, stream.Size.Height)
		}
		return nil
	}
	return fmt.Errorf("%w: node=%d", ErrStreamNotFound, nodeID)
}
