//go:build linux

package portal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
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

	portalScreenshotMaxEncodedBytes = int64(512 << 20)
	portalScreenshotMaxDecodedBytes = int64(512 << 20)
	portalScreenshotMaxDimension    = int64(32_768)
	portalPNGHeaderSize             = 29
)

const portalPNGSignature = "\x89PNG\r\n\x1a\n"

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
	if err := validateCaptureRegion(x, y, w, h); err != nil {
		return nil, err
	}
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
	if err := validateCaptureRegion(x, y, w, h); err != nil {
		return nil, err
	}
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
			img, err := decodeFileURI(ctx, uri)
			if err != nil {
				return nil, err
			}
			return cropImage(img, x, y, w, h)
		case <-ctx.Done():
			return nil, fmt.Errorf("portal: wait for response: %w", ctx.Err())
		}
	}
}

func validateCaptureRegion(x, y, w, h int) error {
	if w == 0 && h == 0 {
		if x != 0 || y != 0 {
			return fmt.Errorf("portal: full-screen capture requires zero origin, got %d,%d", x, y)
		}
		return nil
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("portal: invalid capture region size %dx%d", w, h)
	}
	if x+w < x || y+h < y {
		return fmt.Errorf("portal: capture region overflows integer coordinates: %d,%d %dx%d", x, y, w, h)
	}
	return nil
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

func decodeFileURI(ctx context.Context, rawURI string) (img image.Image, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path, err := portalScreenshotPath(rawURI)
	if err != nil {
		return nil, err
	}
	f, info, err := openAndUnlinkPortalScreenshot(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("portal: close screenshot: %w", closeErr))
		}
	}()

	return decodePortalScreenshot(ctx, f, info.Size())
}

func portalScreenshotPath(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("portal: parse screenshot uri: %w", err)
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", fmt.Errorf("portal: unsupported screenshot uri %q", rawURI)
	}
	path, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		return "", fmt.Errorf("portal: decode screenshot path: %w", err)
	}
	path = filepath.FromSlash(path)
	if path == "" {
		return "", errors.New("portal: empty screenshot path")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("portal: screenshot path is not absolute: %q", path)
	}
	return path, nil
}

func openAndUnlinkPortalScreenshot(path string) (f *os.File, openedInfo os.FileInfo, retErr error) {
	expectedInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("portal: inspect screenshot: %w", err)
	}
	if !expectedInfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("portal: screenshot is not a regular file: %q", path)
	}
	f, err = os.Open(path)
	if err != nil {
		openErr := fmt.Errorf("portal: open screenshot: %w", err)
		if removeErr := removeVerifiedPortalScreenshot(path, expectedInfo); removeErr != nil {
			return nil, nil, errors.Join(
				openErr,
				fmt.Errorf("portal: remove sensitive screenshot after open failure: %w", removeErr),
			)
		}
		return nil, nil, openErr
	}
	cleanupAttempted := false
	defer func() {
		if retErr == nil {
			return
		}
		if closeErr := f.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("portal: close screenshot after setup failure: %w", closeErr))
		}
		if cleanupAttempted {
			return
		}
		if cleanupErr := removeVerifiedPortalScreenshot(path, expectedInfo); cleanupErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("portal: remove sensitive screenshot: %w", cleanupErr))
		}
	}()

	openedInfo, err = f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("portal: inspect opened screenshot: %w", err)
	}
	if err := verifyPortalScreenshotIdentity(expectedInfo, openedInfo); err != nil {
		return nil, nil, err
	}
	// The portal artifact contains the user's desktop. Unlink it immediately
	// after opening so it cannot remain on disk if decoding or later processing
	// fails. Linux keeps the opened contents available through f until close.
	cleanupAttempted = true
	if removeErr := removeVerifiedPortalScreenshot(path, openedInfo); removeErr != nil {
		return nil, nil, fmt.Errorf("portal: remove sensitive screenshot: %w", removeErr)
	}
	return f, openedInfo, nil
}

func decodePortalScreenshot(ctx context.Context, f *os.File, encodedSize int64) (image.Image, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("portal: decode screenshot: %w", ctxErr)
	}
	if encodedSize < 0 || encodedSize > portalScreenshotMaxEncodedBytes {
		return nil, fmt.Errorf(
			"portal: screenshot file size %d exceeds limit %d",
			encodedSize, portalScreenshotMaxEncodedBytes,
		)
	}

	config, err := png.DecodeConfig(&portalContextReader{
		ctx:    ctx,
		reader: io.LimitReader(f, portalScreenshotMaxEncodedBytes),
	})
	if err != nil {
		return nil, portalScreenshotDecodeError(ctx, "inspect", err)
	}
	if err := validatePortalScreenshotConfig(f, config); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("portal: rewind screenshot: %w", err)
	}
	img, err := png.Decode(&portalContextReader{
		ctx:    ctx,
		reader: io.LimitReader(f, portalScreenshotMaxEncodedBytes),
	})
	if err != nil {
		return nil, portalScreenshotDecodeError(ctx, "decode", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("portal: decode screenshot: %w", ctxErr)
	}
	return img, nil
}

type portalContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *portalContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func verifyPortalScreenshotIdentity(expected, actual os.FileInfo) error {
	if expected == nil || actual == nil || !expected.Mode().IsRegular() || !actual.Mode().IsRegular() {
		return errors.New("portal: screenshot file identity is not regular")
	}
	if !os.SameFile(expected, actual) {
		return errors.New("portal: screenshot file changed before sensitive cleanup")
	}
	return nil
}

func removeVerifiedPortalScreenshot(path string, expected os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("re-inspect screenshot before cleanup: %w", err)
	}
	if err := verifyPortalScreenshotIdentity(expected, current); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validatePortalScreenshotConfig(f *os.File, config image.Config) error {
	if config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("portal: invalid screenshot dimensions %dx%d", config.Width, config.Height)
	}
	width, height := int64(config.Width), int64(config.Height)
	if width > portalScreenshotMaxDimension || height > portalScreenshotMaxDimension {
		return fmt.Errorf(
			"portal: screenshot dimensions %dx%d exceed per-axis limit %d",
			config.Width, config.Height, portalScreenshotMaxDimension,
		)
	}

	header := make([]byte, portalPNGHeaderSize)
	if _, err := f.ReadAt(header, 0); err != nil {
		return fmt.Errorf("portal: read screenshot PNG header: %w", err)
	}
	if string(header[:len(portalPNGSignature)]) != portalPNGSignature ||
		string(header[12:16]) != "IHDR" {
		return errors.New("portal: invalid screenshot PNG header")
	}
	headerWidth := int64(binary.BigEndian.Uint32(header[16:20]))
	headerHeight := int64(binary.BigEndian.Uint32(header[20:24]))
	if headerWidth != width || headerHeight != height {
		return errors.New("portal: screenshot PNG header changed during validation")
	}

	bytesPerPixel := int64(4)
	if header[24] == 16 {
		bytesPerPixel = 8
	} else if header[25] == 3 {
		bytesPerPixel = 1
	}
	allocationFactor := bytesPerPixel
	if header[28] != 0 {
		// Adam7 holds the full destination and one partial pass concurrently.
		allocationFactor *= 2
	}
	decodedBytes := width * height * allocationFactor
	if decodedBytes > portalScreenshotMaxDecodedBytes {
		return fmt.Errorf(
			"portal: estimated screenshot decode size %d exceeds limit %d",
			decodedBytes, portalScreenshotMaxDecodedBytes,
		)
	}
	return nil
}

func portalScreenshotDecodeError(ctx context.Context, operation string, decodeErr error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("portal: %s screenshot: %w", operation, ctxErr)
	}
	return fmt.Errorf("portal: %s screenshot: %w", operation, decodeErr)
}

func cropImage(img image.Image, x, y, w, h int) (image.Image, error) {
	if img == nil {
		return nil, errors.New("portal: nil screenshot")
	}
	if err := validateCaptureRegion(x, y, w, h); err != nil {
		return nil, err
	}
	if w == 0 && h == 0 {
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
