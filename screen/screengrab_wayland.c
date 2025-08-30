//go:build ignore
// +build ignore

#include "screen_c.h"
#include "../base/bitmap_free_c.h"

#if defined(IS_LINUX)
// capture_screen_wayland attempts to capture the screen using a Wayland protocol.
// This is a stub that currently returns NULL to signal unsupported capture.
MMBitmapRef capture_screen_wayland(int32_t x, int32_t y, int32_t w, int32_t h, int32_t display_id, int8_t isPid) {
        (void)x; (void)y; (void)w; (void)h; (void)display_id; (void)isPid;
        return NULL;
}
#endif
