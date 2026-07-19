//go:build (!ocr || !cgo) && !windows

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ocrTestSynchronizationTimeout = 2 * time.Second

func startCancelableOCRTestCall(
	t *testing.T,
	call func(context.Context) error,
) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- call(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(ocrTestSynchronizationTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Error("canceled OCR call did not stop during test cleanup")
		}
	})
	return cancel, result
}

func waitForOCRTestPath(ctx context.Context, path string) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("observe OCR test path %q: %w", path, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for OCR test path %q: %w", path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func requireOCRTestPath(t *testing.T, path string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), ocrTestSynchronizationTimeout)
	defer cancel()
	if err := waitForOCRTestPath(ctx, path); err != nil {
		t.Fatal(err)
	}
}

func requireCanceledOCRTestCall(t *testing.T, result <-chan error) {
	t.Helper()

	timer := time.NewTimer(ocrTestSynchronizationTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("OCR call error = %v, want cancellation", err)
		}
	case <-timer.C:
		t.Fatal("OCR call did not return after cancellation")
	}
}

func TestGetTextContextCancelsTesseract(t *testing.T) {
	directory := t.TempDir()
	tesseract := filepath.Join(directory, "tesseract")
	ready := filepath.Join(directory, "ready")
	script := "#!/bin/sh\nprintf ready > \"$ROBOTGO_OCR_TEST_READY\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(tesseract, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ROBOTGO_OCR_TEST_READY", ready)

	cancel, result := startCancelableOCRTestCall(t, func(ctx context.Context) error {
		_, err := GetTextContext(ctx, "ignored.png")
		return err
	})
	requireOCRTestPath(t, ready)
	cancel()
	requireCanceledOCRTestCall(t, result)
}

func TestGetTextContextDoesNotStartTesseractWhenAlreadyCanceled(t *testing.T) {
	directory := t.TempDir()
	tesseract := filepath.Join(directory, "tesseract")
	invoked := filepath.Join(directory, "invoked")
	script := "#!/bin/sh\nprintf invoked > \"$ROBOTGO_OCR_TEST_INVOKED\"\n"
	if err := os.WriteFile(tesseract, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ROBOTGO_OCR_TEST_INVOKED", invoked)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GetTextContext(ctx, "ignored.png")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetTextContext error = %v, want cancellation", err)
	}
	if _, err := os.Stat(invoked); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("already-canceled OCR call invoked backend or stat failed: %v", err)
	}
}

func TestGetTextImgContextRemovesPrivateTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	tesseract := filepath.Join(directory, "tesseract")
	marker := filepath.Join(directory, "image-path")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$ROBOTGO_OCR_TEST_MARKER\"\nprintf recognized\n"
	if err := os.WriteFile(tesseract, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ROBOTGO_OCR_TEST_MARKER", marker)

	text, err := GetTextImgContext(context.Background(), image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if err != nil {
		t.Fatalf("GetTextImgContext: %v", err)
	}
	if strings.TrimSpace(text) != "recognized" {
		t.Fatalf("OCR text = %q", text)
	}
	pathBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(pathBytes)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary OCR image still exists or stat failed: %v", err)
	}
}

func TestGetTextImgContextRemovesPrivateTemporaryFileAfterCancellation(t *testing.T) {
	directory := t.TempDir()
	tesseract := filepath.Join(directory, "tesseract")
	marker := filepath.Join(directory, "image-path")
	ready := filepath.Join(directory, "ready")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$ROBOTGO_OCR_TEST_MARKER\"\nprintf ready > \"$ROBOTGO_OCR_TEST_READY\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(tesseract, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ROBOTGO_OCR_TEST_MARKER", marker)
	t.Setenv("ROBOTGO_OCR_TEST_READY", ready)

	cancel, result := startCancelableOCRTestCall(t, func(ctx context.Context) error {
		_, err := GetTextImgContext(ctx, image.NewRGBA(image.Rect(0, 0, 1, 1)))
		return err
	})
	requireOCRTestPath(t, ready)
	pathBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	requireCanceledOCRTestCall(t, result)
	if _, err := os.Stat(string(pathBytes)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary OCR image still exists after cancellation or stat failed: %v", err)
	}
}
