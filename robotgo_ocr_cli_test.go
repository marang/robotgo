//go:build (!ocr || !cgo) && !windows

package robotgo

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetTextContextCancelsTesseract(t *testing.T) {
	directory := t.TempDir()
	tesseract := filepath.Join(directory, "tesseract")
	if err := os.WriteFile(tesseract, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := GetTextContext(ctx, "ignored.png")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetTextContext error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled OCR command took %v", elapsed)
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
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$ROBOTGO_OCR_TEST_MARKER\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(tesseract, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("ROBOTGO_OCR_TEST_MARKER", marker)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := GetTextImgContext(ctx, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetTextImgContext error = %v, want deadline", err)
	}
	pathBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(pathBytes)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary OCR image still exists after cancellation or stat failed: %v", err)
	}
}
