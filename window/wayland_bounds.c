//go:build ignore

#include "wayland_bounds.h"
#include "get_bounds_wayland.h"

Bounds wayland_get_bounds(void) {
  Bounds bounds = {0};
  struct wl_display *display = wl_display_connect(NULL);
  if (!display) {
    return bounds;
  }

  int x = 0;
  int y = 0;
  int width = 0;
  int height = 0;
  if (get_screen_rect_wayland(display, -1, &x, &y, &width, &height) == 0) {
    bounds.X = x;
    bounds.Y = y;
    bounds.W = width;
    bounds.H = height;
  }
  wl_display_disconnect(display);
  return bounds;
}
