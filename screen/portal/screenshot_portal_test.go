//go:build linux

package portal

import (
	"context"
	"errors"
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
	matched         dbus.ObjectPath
	signalCh        chan<- *dbus.Signal
	uri             string
	screenshotCalls int
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
	p.screenshotCalls++
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

func writeTestPNG(t *testing.T) (string, string) {
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
	return (&url.URL{Scheme: "file", Path: path}).String(), path
}

func TestCaptureRegionImageSubscribesBeforeRequestAndCrops(t *testing.T) {
	uri, artifactPath := writeTestPNG(t)
	portal := &fakeScreenshotPortal{uri: uri}
	img, err := captureRegionImage(context.Background(), portal, 2, 1, 1, 1)
	if err != nil {
		t.Fatalf("captureRegionImage failed: %v", err)
	}
	assertSensitiveArtifactRemoved(t, artifactPath)
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

func TestCaptureRegionImageRejectsInvalidRegionBeforeRequest(t *testing.T) {
	tests := []struct {
		name string
		w    int
		h    int
	}{
		{name: "missing width", w: 0, h: 2},
		{name: "missing height", w: 2, h: 0},
		{name: "negative width", w: -1, h: 2},
		{name: "negative height", w: 2, h: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portal := &fakeScreenshotPortal{}
			if _, err := captureRegionImage(context.Background(), portal, 0, 0, test.w, test.h); err == nil {
				t.Fatal("expected invalid-region error")
			}
			if portal.screenshotCalls != 0 {
				t.Fatalf("screenshot requests = %d, want 0", portal.screenshotCalls)
			}
		})
	}
	portal := &fakeScreenshotPortal{}
	maxInt := int(^uint(0) >> 1)
	if _, err := captureRegionImage(context.Background(), portal, maxInt, 0, 1, 1); err == nil {
		t.Fatal("expected coordinate-overflow error")
	}
	if portal.screenshotCalls != 0 {
		t.Fatalf("overflow screenshot requests = %d, want 0", portal.screenshotCalls)
	}
	if _, err := captureRegionImage(context.Background(), portal, 1, 0, 0, 0); err == nil {
		t.Fatal("expected non-zero full-screen origin error")
	}
	if portal.screenshotCalls != 0 {
		t.Fatalf("non-zero-origin screenshot requests = %d, want 0", portal.screenshotCalls)
	}
}

func TestCropImageRejectsDisjointRegion(t *testing.T) {
	_, err := cropImage(image.NewRGBA(image.Rect(0, 0, 4, 4)), 20, 20, 2, 2)
	if err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestCropImageOnlyTreatsZeroByZeroAsFullScreenshot(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 4, 4))
	full, err := cropImage(source, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("full screenshot: %v", err)
	}
	if full != source {
		t.Fatal("full screenshot should return the source image")
	}
	if _, err := cropImage(source, 1, 0, 0, 0); err == nil {
		t.Fatal("non-zero-origin full screenshot unexpectedly succeeded")
	}
	for _, size := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := cropImage(source, 0, 0, size[0], size[1]); err == nil {
			t.Fatalf("crop size %dx%d unexpectedly succeeded", size[0], size[1])
		}
	}
}

func TestDecodeFileURIRejectsNonFileScheme(t *testing.T) {
	if _, err := decodeFileURI("https://example.com/screenshot.png"); err == nil {
		t.Fatal("expected unsupported URI error")
	}
}

func TestDecodeFileURIRemovesSensitiveArtifactOnDecodeFailure(t *testing.T) {
	path := t.TempDir() + "/invalid portal image.png"
	if err := os.WriteFile(path, []byte("sensitive but not a PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	if _, err := decodeFileURI(uri); err == nil {
		t.Fatal("expected PNG decode error")
	}
	assertSensitiveArtifactRemoved(t, path)
}

func assertSensitiveArtifactRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sensitive portal artifact still exists at %q: %v", path, err)
	}
}
