//go:build !portal || !linux

package portal

import (
	"context"
	"fmt"
)

/*
#include "../screengrab_c.h"
*/
import "C"

// CBitmap mirrors robotgo.CBitmap without importing the root package.
type CBitmap = C.MMBitmapRef

// Capture invokes the C fallback implementation. It is built when the
// portal build tag is not enabled, allowing the rest of the module to
// compile without portal dependencies.
func Capture(ctx context.Context, x, y, w, h int) (CBitmap, error) {
	var cerr C.int32_t
	bit := C.capture_screen_portal(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), 0, 0, &cerr)
	if bit == nil {
		return nil, fmt.Errorf("portal capture failed: %d", int(cerr))
	}
	return bit, nil
}
