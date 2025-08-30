//go:build ignore
// +build ignore

// This file is included from screengrab_c.h, which already provides the
// necessary declarations and includes. Avoid including headers here to prevent
// duplicate symbol definitions when screengrab_wayland.c is embedded.

#if defined(IS_LINUX)
// capture_screen_wayland attempts to capture the screen using a Wayland protocol.
// This is a stub that currently returns NULL to signal unsupported capture.
MMBitmapRef capture_screen_wayland(int32_t x, int32_t y, int32_t w, int32_t h, int32_t display_id, int8_t isPid) {
        (void)x; (void)y; (void)w; (void)h; (void)display_id; (void)isPid;
        return NULL;
}
#endif
