//go:build linux

package portal

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeScreenshotPortal struct {
	matched  dbus.ObjectPath
	signalCh chan<- *dbus.Signal
	uri      string
}

func (p *fakeScreenshotPortal) uniqueName() string { return ":1.42" }
func (p *fakeScreenshotPortal) addResponseMatch(path dbus.ObjectPath) error {
	p.matched = path
	return nil
}
func (p *fakeScreenshotPortal) removeResponseMatch(path dbus.ObjectPath) error {
	if p.matched == path {
		p.matched = ""
	}
	return nil
}
func (p *fakeScreenshotPortal) registerSignals(ch chan<- *dbus.Signal) { p.signalCh = ch }
func (p *fakeScreenshotPortal) removeSignals(ch chan<- *dbus.Signal) {
	if p.signalCh == ch {
		p.signalCh = nil
	}
}
func (p *fakeScreenshotPortal) screenshot(_ context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	token, _ := options["handle_token"].Value().(string)
	path := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_42/" + token)
	// Deliver synchronously, before returning from the method call. This proves
	// captureRegionImage subscribed before issuing Screenshot.
	if p.matched == path && p.signalCh != nil && p.uri != "" {
		p.signalCh <- &dbus.Signal{
			Name: portalResponse,
			Path: path,
			Body: []interface{}{
				uint32(0),
				map[string]dbus.Variant{"uri": dbus.MakeVariant(p.uri)},
			},
		}
	}
	return path, nil
}
func (p *fakeScreenshotPortal) close() error { return nil }

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/portal image.png"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.SetRGBA(2, 1, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestCaptureRegionImageSubscribesBeforeRequestAndCrops(t *testing.T) {
	portal := &fakeScreenshotPortal{uri: writeTestPNG(t)}
	img, err := captureRegionImage(context.Background(), portal, 2, 1, 1, 1)
	if err != nil {
		t.Fatalf("captureRegionImage failed: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(2, 1, 3, 2) {
		t.Fatalf("unexpected crop bounds: %v", got)
	}
	r, g, b, _ := img.At(2, 1).RGBA()
	if r>>8 != 0x12 || g>>8 != 0x34 || b>>8 != 0x56 {
		t.Fatalf("unexpected pixel: %02x%02x%02x", r>>8, g>>8, b>>8)
	}
}

func TestCaptureRegionImageHonorsCallerDeadline(t *testing.T) {
	portal := &fakeScreenshotPortal{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := captureRegionImage(ctx, portal, 0, 0, 0, 0)
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("caller deadline was ignored: %v", elapsed)
	}
}

func TestCropImageRejectsDisjointRegion(t *testing.T) {
	_, err := cropImage(image.NewRGBA(image.Rect(0, 0, 4, 4)), 20, 20, 2, 2)
	if err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestDecodeFileURIRejectsNonFileScheme(t *testing.T) {
	if _, err := decodeFileURI("https://example.com/screenshot.png"); err == nil {
		t.Fatal("expected unsupported URI error")
	}
}
