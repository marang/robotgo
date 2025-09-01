//go:build ignore
// +build ignore

// Placeholder implementation of a portal-based screen capture backend.
// It currently generates a solid-color bitmap as a stand-in for an actual
// frame retrieved via the org.freedesktop.portal.ScreenCast API.

#include <stdint.h>
#include <stdlib.h>
#include <string.h>


#if defined(IS_LINUX)
MMBitmapRef capture_screen_portal(int32_t x, int32_t y, int32_t w, int32_t h,
                                  int32_t display_id, int8_t isPid,
                                  int32_t *err) {
  (void)x;
  (void)y;
  (void)display_id;
  (void)isPid;
  if (getenv("ROBOTGO_PORTAL_FAIL")) {
    if (err) {
      *err = ScreengrabErrPortal;
    }
    return NULL;
  }
  if (w <= 0) {
    w = 100;
  }
  if (h <= 0) {
    h = 100;
  }
  size_t stride = (size_t)w * 4;
  uint8_t *rgba = malloc(stride * (size_t)h);
  if (!rgba) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    return NULL;
  }
  memset(rgba, 0, stride * (size_t)h);
  for (int row = 0; row < h; row++) {
    for (int col = 0; col < w; col++) {
      size_t idx = (size_t)row * stride + (size_t)col * 4;
      rgba[idx + 0] = 0x00;
      rgba[idx + 1] = 0xff;
      rgba[idx + 2] = 0x00;
      rgba[idx + 3] = 0xff;
    }
  }
  MMBitmapRef bitmap = createMMBitmap_c(rgba, w, h, stride, 32, 4);
  if (!bitmap) {
    free(rgba);
    if (err) {
      *err = ScreengrabErrFailed;
    }
    return NULL;
  }
  if (err) {
    *err = ScreengrabOK;
  }
  return bitmap;
}
#endif

