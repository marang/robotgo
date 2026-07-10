//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalDestination = "org.freedesktop.portal.Desktop"
	portalObjectPath  = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	portalRequestIF   = "org.freedesktop.portal.Request"
	portalResponse    = portalRequestIF + ".Response"
	portalScreenshot  = "org.freedesktop.portal.Screenshot.Screenshot"
	portalTimeout     = 10 * time.Second
)

var portalTokenCounter atomic.Uint64

type screenshotPortal interface {
	uniqueName() string
	addResponseMatch(dbus.ObjectPath) error
	removeResponseMatch(dbus.ObjectPath) error
	registerSignals(chan<- *dbus.Signal)
	removeSignals(chan<- *dbus.Signal)
	screenshot(context.Context, map[string]dbus.Variant) (dbus.ObjectPath, error)
	close() error
}

type dbusScreenshotPortal struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

func connectScreenshotPortal() (screenshotPortal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("portal: connect session bus: %w", err)
	}
	return &dbusScreenshotPortal{
		conn: conn,
		obj:  conn.Object(portalDestination, portalObjectPath),
	}, nil
}

// Available reports whether the desktop portal service currently owns its
// well-known D-Bus name. It does not start a screenshot request or prompt the
// user.
func Available(ctx context.Context) (available bool, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false, fmt.Errorf("portal: connect session bus: %w", err)
	}
	defer func() {
		if err := conn.Close(); retErr == nil && err != nil {
			retErr = fmt.Errorf("portal: close session bus: %w", err)
		}
	}()

	obj := conn.Object("org.freedesktop.DBus", dbus.ObjectPath("/org/freedesktop/DBus"))
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, portalDestination)
	if call.Err != nil {
		return false, fmt.Errorf("portal: query service owner: %w", call.Err)
	}
	if err := call.Store(&available); err != nil {
		return false, fmt.Errorf("portal: decode service owner: %w", err)
	}
	return available, nil
}

func (p *dbusScreenshotPortal) uniqueName() string { return p.conn.Names()[0] }

func (p *dbusScreenshotPortal) addResponseMatch(path dbus.ObjectPath) error {
	return p.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(portalRequestIF),
		dbus.WithMatchMember("Response"),
	)
}

func (p *dbusScreenshotPortal) removeResponseMatch(path dbus.ObjectPath) error {
	return p.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(portalRequestIF),
		dbus.WithMatchMember("Response"),
	)
}

func (p *dbusScreenshotPortal) registerSignals(ch chan<- *dbus.Signal) {
	p.conn.Signal(ch)
}

func (p *dbusScreenshotPortal) removeSignals(ch chan<- *dbus.Signal) {
	p.conn.RemoveSignal(ch)
}

func (p *dbusScreenshotPortal) screenshot(ctx context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	call := p.obj.CallWithContext(ctx, portalScreenshot, 0, "", options)
	if call.Err != nil {
		return "", call.Err
	}
	var path dbus.ObjectPath
	if err := call.Store(&path); err != nil {
		return "", err
	}
	return path, nil
}

func (p *dbusScreenshotPortal) close() error { return p.conn.Close() }

func nextPortalToken() string {
	return fmt.Sprintf("robotgo_screenshot_%d", portalTokenCounter.Add(1))
}

func portalSenderPath(uniqueName string) string {
	name := strings.TrimPrefix(uniqueName, ":")
	return strings.ReplaceAll(name, ".", "_")
}

// CaptureRegionImage uses the org.freedesktop.portal.Screenshot API to
// capture a full-screen image and crops it client-side to x,y,w,h when a
// non-empty region is requested. The portal may prompt the user.
func CaptureRegionImage(ctx context.Context, x, y, w, h int) (img image.Image, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	portal, err := connectScreenshotPortal()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := portal.close(); retErr == nil && err != nil {
			retErr = fmt.Errorf("portal: close session bus: %w", err)
		}
	}()

	return captureRegionImage(ctx, portal, x, y, w, h)
}

func captureRegionImage(ctx context.Context, portal screenshotPortal, x, y, w, h int) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, portalTimeout)
	defer cancel()

	token := nextPortalToken()
	requestPath := dbus.ObjectPath(fmt.Sprintf(
		"/org/freedesktop/portal/desktop/request/%s/%s",
		portalSenderPath(portal.uniqueName()), token,
	))

	// The request path is predictable when handle_token is supplied. Subscribe
	// before invoking Screenshot so a fast portal cannot race past us.
	if err := portal.addResponseMatch(requestPath); err != nil {
		return nil, fmt.Errorf("portal: add response match: %w", err)
	}
	defer func() { _ = portal.removeResponseMatch(requestPath) }()

	signalCh := make(chan *dbus.Signal, 4)
	portal.registerSignals(signalCh)
	defer portal.removeSignals(signalCh)

	options := map[string]dbus.Variant{
		"interactive":  dbus.MakeVariant(false),
		"handle_token": dbus.MakeVariant(token),
	}
	returnedPath, err := portal.screenshot(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("portal: request screenshot: %w", err)
	}
	if returnedPath != requestPath {
		return nil, fmt.Errorf("portal: unexpected request path %q", returnedPath)
	}

	for {
		select {
		case sig, ok := <-signalCh:
			if !ok {
				return nil, errors.New("portal: session bus closed while waiting for response")
			}
			if sig == nil || sig.Path != requestPath || sig.Name != portalResponse {
				continue
			}
			uri, err := responseURI(sig)
			if err != nil {
				return nil, err
			}
			img, err := decodeFileURI(uri)
			if err != nil {
				return nil, err
			}
			return cropImage(img, x, y, w, h)
		case <-ctx.Done():
			return nil, fmt.Errorf("portal: wait for response: %w", ctx.Err())
		}
	}
}

func responseURI(sig *dbus.Signal) (string, error) {
	if len(sig.Body) < 2 {
		return "", errors.New("portal: malformed response")
	}
	code, ok := sig.Body[0].(uint32)
	if !ok {
		return "", errors.New("portal: malformed response code")
	}
	if code != 0 {
		return "", fmt.Errorf("portal: screenshot request rejected (code %d)", code)
	}
	results, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return "", errors.New("portal: malformed response results")
	}
	v, ok := results["uri"]
	if !ok {
		return "", errors.New("portal: missing uri")
	}
	uri, ok := v.Value().(string)
	if !ok || uri == "" {
		return "", errors.New("portal: invalid uri")
	}
	return uri, nil
}

func decodeFileURI(rawURI string) (image.Image, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, fmt.Errorf("portal: parse screenshot uri: %w", err)
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return nil, fmt.Errorf("portal: unsupported screenshot uri %q", rawURI)
	}
	path, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		return nil, fmt.Errorf("portal: decode screenshot path: %w", err)
	}
	path = filepath.FromSlash(path)
	if path == "" {
		return nil, errors.New("portal: empty screenshot path")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("portal: open screenshot: %w", err)
	}
	img, decodeErr := png.Decode(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return nil, fmt.Errorf("portal: decode screenshot: %w", decodeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("portal: close screenshot: %w", closeErr)
	}
	return img, nil
}

func cropImage(img image.Image, x, y, w, h int) (image.Image, error) {
	if img == nil {
		return nil, errors.New("portal: nil screenshot")
	}
	if w <= 0 || h <= 0 {
		return img, nil
	}
	requested := image.Rect(x, y, x+w, y+h)
	cropped := requested.Intersect(img.Bounds())
	if cropped.Empty() {
		return nil, fmt.Errorf("portal: requested region %v is outside screenshot bounds %v", requested, img.Bounds())
	}
	s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil, errors.New("portal: screenshot does not support cropping")
	}
	return s.SubImage(cropped), nil
}
