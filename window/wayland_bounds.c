//go:build ignore

#include "wayland_bounds.h"
#include "get_bounds_wayland.h"

Bounds wayland_get_bounds(void) {
  Bounds bounds = {0};
  struct wl_display *display = wl_display_connect(NULL);
  if (!display) {
    return bounds;
  }

  int width = 0;
  int height = 0;
  if (get_bounds_wayland(display, &width, &height) == 0) {
    bounds.X = 0;
    bounds.Y = 0;
    bounds.W = width;
    bounds.H = height;
  }
  wl_display_disconnect(display);
  return bounds;
}
