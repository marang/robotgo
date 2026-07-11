//go:build !ocr || !cgo

package robotgo

import (
	"context"
	"os/exec"
	"time"
)

const defaultOCRTimeout = 2 * time.Minute

// GetText extracts image text by invoking the Tesseract CLI.
//
// robotgo.GetText(imgPath, lang string)
func GetText(imgPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOCRTimeout)
	defer cancel()
	return GetTextContext(ctx, imgPath, args...)
}

// GetTextContext extracts image text through the Tesseract CLI with
// caller-controlled cancellation.
func GetTextContext(ctx context.Context, imgPath string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lang := "eng"
	if len(args) > 0 {
		lang = args[0]
		if lang == "zh" {
			lang = "chi_sim"
		}
	}

	body, err := exec.CommandContext(ctx, "tesseract", imgPath, "stdout", "-l", lang).Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	return string(body), nil
}
