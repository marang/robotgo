//go:build linux && wayland
// +build linux,wayland

#include "window/get_bounds_wayland.h"
#include <string.h>

struct registry_data {
  struct wl_compositor **compositor;
  struct xdg_wm_base **wm_base;
};

static void registry_handle_global(void *data, struct wl_registry *registry,
                                   uint32_t name, const char *interface,
                                   uint32_t version) {
  struct registry_data *rdata = data;
  if (strcmp(interface, "wl_compositor") == 0) {
    *rdata->compositor =
        wl_registry_bind(registry, name, &wl_compositor_interface, 1);
  } else if (strcmp(interface, "xdg_wm_base") == 0) {
    *rdata->wm_base =
        wl_registry_bind(registry, name, &xdg_wm_base_interface, 1);
  }
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_handle_global,
    .global_remove = NULL,
};

struct bounds_data {
  int *width;
  int *height;
};

static void xdg_toplevel_handle_configure(void *data,
                                          struct xdg_toplevel *toplevel,
                                          int32_t width, int32_t height,
                                          struct wl_array *states) {
  struct bounds_data *bdata = data;
  if (bdata->width) {
    *bdata->width = width;
  }
  if (bdata->height) {
    *bdata->height = height;
  }
}

static const struct xdg_toplevel_listener xdg_toplevel_listener = {
    .configure = xdg_toplevel_handle_configure,
    .close = NULL,
};

int get_bounds_wayland(struct wl_display *display, int *width, int *height) {
  if (!display) {
    return -1;
  }

  struct wl_compositor *compositor = NULL;
  struct xdg_wm_base *wm_base = NULL;
  struct registry_data rdata = {&compositor, &wm_base};

  struct wl_registry *registry = wl_display_get_registry(display);
  wl_registry_add_listener(registry, &registry_listener, &rdata);
  wl_display_roundtrip(display);

  if (!compositor || !wm_base) {
    return -1;
  }

  struct wl_surface *surface = wl_compositor_create_surface(compositor);
  if (!surface) {
    return -1;
  }

  struct xdg_surface *xdg_surface =
      xdg_wm_base_get_xdg_surface(wm_base, surface);
  if (!xdg_surface) {
    wl_surface_destroy(surface);
    return -1;
  }

  struct bounds_data bdata = {width, height};
  struct xdg_toplevel *xdg_toplevel = xdg_surface_get_toplevel(xdg_surface);
  xdg_toplevel_add_listener(xdg_toplevel, &xdg_toplevel_listener, &bdata);

  wl_surface_commit(surface);
  wl_display_roundtrip(display);

  xdg_toplevel_destroy(xdg_toplevel);
  xdg_surface_destroy(xdg_surface);
  wl_surface_destroy(surface);

  return 0;
}
