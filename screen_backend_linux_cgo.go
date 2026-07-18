//go:build cgo && linux

package robotgo

import (
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xinerama"
	"github.com/vcaesar/screenshot"
)

const (
	envLinuxSessionType     = "XDG_SESSION_TYPE"
	linuxSessionTypeWayland = "wayland"
)

func platformDisplayBoundsE(displayIndex int) (image.Rectangle, error) {
	server := selectedDisplayServer()
	if server == DisplayServerX11 {
		if err := linuxX11SessionConflictError("display bounds"); err != nil {
			return image.Rectangle{}, err
		}
	}
	waylandBounds := func(index int) (image.Rectangle, error) {
		rect := GetScreenRect(index)
		return image.Rect(rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H), nil
	}
	return dispatchLinuxDisplayBounds(
		server,
		displayIndex,
		waylandBounds,
		x11DisplayBounds,
	)
}

func x11DisplayBounds(displayIndex int) (image.Rectangle, error) {
	if displayIndex < 0 {
		return image.Rectangle{}, invalidDisplayIndexError(displayIndex)
	}
	unlock := lockNativeX11Display()
	display := getXDisplayNameLocked()
	unlock()

	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("robotgo: connect to X11 display for bounds: %w", err)
	}
	defer conn.Close()
	if err := xinerama.Init(conn); err != nil {
		return image.Rectangle{}, fmt.Errorf("robotgo: initialize Xinerama for bounds: %w", err)
	}
	reply, err := xinerama.QueryScreens(conn).Reply()
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("robotgo: query Xinerama screens: %w", err)
	}
	count := int(reply.Number)
	if count != len(reply.ScreenInfo) {
		return image.Rectangle{}, fmt.Errorf(
			"robotgo: malformed Xinerama reply: count=%d records=%d",
			count,
			len(reply.ScreenInfo),
		)
	}
	if displayIndex >= count {
		return image.Rectangle{}, fmt.Errorf(
			"robotgo: display index %d is outside active Xinerama count %d",
			displayIndex,
			count,
		)
	}
	primary := reply.ScreenInfo[0]
	screen := reply.ScreenInfo[displayIndex]
	x := int(screen.XOrg) - int(primary.XOrg)
	y := int(screen.YOrg) - int(primary.YOrg)
	width := int(screen.Width)
	height := int(screen.Height)
	return image.Rect(x, y, x+width, y+height), nil
}

func platformCapture(x, y, width, height int) (*image.RGBA, error) {
	nativeCapture := func(x, y, width, height int) (*image.RGBA, error) {
		captured, err := CaptureImg(x, y, width, height)
		if err != nil {
			return nil, err
		}
		rgba, ok := captured.(*image.RGBA)
		if !ok {
			return nil, fmt.Errorf(
				"robotgo: native Linux capture returned %T instead of *image.RGBA",
				captured,
			)
		}
		return rgba, nil
	}
	x11Capture := nativeCapture
	if !nativeX11BackendCompiled() && !linuxCaptureUsesPortalBackend() {
		// A Wayland-enabled CGO binary may still run in a real X11 session.
		// Its C build intentionally omits the native X11 capture backend, so
		// preserve the portable XGB/Xinerama path used by Capture before the
		// Linux session dispatcher was introduced.
		x11Capture = func(x, y, width, height int) (*image.RGBA, error) {
			if err := linuxX11SessionConflictError("capture"); err != nil {
				return nil, err
			}
			img, err := screenshot.Capture(x, y, width, height)
			if err != nil {
				return nil, fmt.Errorf(
					"robotgo: capture X11 from a Wayland-enabled build: %w",
					err,
				)
			}
			setLastBackend(BackendX11)
			return img, nil
		}
	}
	return dispatchLinuxCapture(
		selectedDisplayServer(),
		x,
		y,
		width,
		height,
		nativeCapture,
		x11Capture,
	)
}

func linuxCaptureUsesPortalBackend() bool {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend)))
	return os.Getenv(envForcePortal) != "" ||
		backend == waylandBackendPortalName ||
		backend == waylandBackendScreenCast
}

func linuxX11SessionConflictError(feature string) error {
	if !strings.EqualFold(
		strings.TrimSpace(os.Getenv(envLinuxSessionType)),
		linuxSessionTypeWayland,
	) {
		return nil
	}
	return fmt.Errorf(
		"%w: the X11 backend is selected but %s selects Wayland; refusing implicit Xwayland %s",
		ErrNotSupported,
		envLinuxSessionType,
		feature,
	)
}

func platformCaptureImgFallback(args ...int) (image.Image, bool, error) {
	if nativeX11BackendCompiled() ||
		selectedDisplayServer() != DisplayServerX11 ||
		linuxCaptureUsesPortalBackend() {
		return nil, false, nil
	}
	if err := validateCaptureArguments(args); err != nil {
		return nil, true, err
	}
	if len(args) > 5 {
		return nil, true, fmt.Errorf(
			"%w: process-targeted X11 capture is unavailable in a Wayland-enabled build",
			ErrNotSupported,
		)
	}
	img, err := Capture(args...)
	return img, true, err
}
