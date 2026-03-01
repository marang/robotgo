//go:build linux

package portal

import (
    "context"
    "errors"
    "image"
    "image/png"
    "net/url"
    "os"
    "strings"
    "time"

    "github.com/godbus/dbus/v5"
)

// CaptureRegionImage uses the org.freedesktop.portal.Screenshot API to
// capture a full-screen image and crops it client-side to x,y,w,h if
// dimensions are provided. It returns an image.Image with the screenshot
// contents. This requires a running desktop portal and may prompt the user.
func CaptureRegionImage(ctx context.Context, x, y, w, h int) (image.Image, error) {
    if ctx == nil {
        ctx = context.Background()
    }
    conn, err := dbus.SessionBus()
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
    options := map[string]dbus.Variant{
        // Non-interactive request. Some portals may still show consent.
        "interactive": dbus.MakeVariant(false),
    }
    call := obj.CallWithContext(ctx, "org.freedesktop.portal.Screenshot.Screenshot", 0, "", options)
    if call.Err != nil {
        return nil, call.Err
    }

    // The call returns a request handle object path. Wait for Response.
    if len(call.Body) != 1 {
        return nil, errors.New("unexpected screenshot response body")
    }
    handle, ok := call.Body[0].(dbus.ObjectPath)
    if !ok {
        return nil, errors.New("invalid screenshot handle type")
    }

    // Listen for the Response signal on the request path.
    signalCh := make(chan *dbus.Signal, 1)
    conn.Signal(signalCh)
    defer conn.RemoveSignal(signalCh)

    // Add match rule for the request path/interface.
    _ = conn.AddMatchSignal(
        dbus.WithMatchObjectPath(handle),
        dbus.WithMatchInterface("org.freedesktop.portal.Request"),
    )

    // Wait with timeout.
    timeout := time.After(10 * time.Second)
    for {
        select {
        case sig := <-signalCh:
            if sig == nil || string(sig.Path) != string(handle) || sig.Name != "org.freedesktop.portal.Request.Response" {
                continue
            }
            if len(sig.Body) < 2 {
                return nil, errors.New("portal: malformed response")
            }
            code, _ := sig.Body[0].(uint32)
            results, _ := sig.Body[1].(map[string]dbus.Variant)
            if code != 0 {
                return nil, errors.New("portal: request denied")
            }
            v, ok := results["uri"]
            if !ok {
                return nil, errors.New("portal: missing uri")
            }
            uri, _ := v.Value().(string)
            if uri == "" {
                return nil, errors.New("portal: empty uri")
            }
            // Open the file and decode PNG.
            p := strings.TrimPrefix(uri, "file://")
            if u, perr := url.PathUnescape(p); perr == nil {
                p = u
            }
            f, err := os.Open(p)
            if err != nil {
                return nil, err
            }
            defer f.Close()
            img, err := png.Decode(f)
            if err != nil {
                return nil, err
            }
            // Crop if requested (bounds check).
            if w > 0 && h > 0 {
                r := img.Bounds()
                if x < r.Min.X { x = r.Min.X }
                if y < r.Min.Y { y = r.Min.Y }
                if x+w > r.Max.X { w = r.Max.X - x }
                if h+y > r.Max.Y { h = r.Max.Y - y }
                if w > 0 && h > 0 {
                    type subImager interface{ SubImage(r image.Rectangle) image.Image }
                    if s, ok := img.(subImager); ok {
                        return s.SubImage(image.Rect(x, y, x+w, y+h)), nil
                    }
                }
            }
            return img, nil
        case <-timeout:
            return nil, errors.New("portal: timeout waiting for response")
        }
    }
}
