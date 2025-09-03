//go:build !linux

package portal

import (
    "context"
    "fmt"
    "image"
)

// CaptureRegionImage is not available without the portal build tag;
// return an error so callers can fall back to the C implementation in the
// main robotgo package.
func CaptureRegionImage(ctx context.Context, x, y, w, h int) (image.Image, error) {
    return nil, fmt.Errorf("portal image capture not available on this OS")
}
