//go:build linux && wayland
// +build linux,wayland

#include "window/get_bounds_wayland.h"
#include <limits.h>
#include <stdlib.h>
#include <string.h>

struct registry_data {
  struct wl_compositor **compositor;
  struct xdg_wm_base **wm_base;
  struct wl_list *outputs;
};

struct output_info {
  struct wl_list link;
  struct wl_output *output;
  int32_t x;
  int32_t y;
  int32_t mode_w;
  int32_t mode_h;
  int32_t transform;
  int32_t scale;
  int has_mode;
};

static void output_logical_size(const struct output_info *oi, int *lw, int *lh) {
  int s = oi->scale > 0 ? oi->scale : 1;
  int w = oi->mode_w > 0 ? oi->mode_w / s : 0;
  int h = oi->mode_h > 0 ? oi->mode_h / s : 0;

  switch (oi->transform) {
  case WL_OUTPUT_TRANSFORM_90:
  case WL_OUTPUT_TRANSFORM_270:
  case WL_OUTPUT_TRANSFORM_FLIPPED_90:
  case WL_OUTPUT_TRANSFORM_FLIPPED_270: {
    int t = w;
    w = h;
    h = t;
    break;
  }
  default:
    break;
  }

  if (lw) {
    *lw = w;
  }
  if (lh) {
    *lh = h;
  }
}

static void output_geometry(void *data, struct wl_output *output,
                            int32_t x, int32_t y, int32_t physical_width,
                            int32_t physical_height, int32_t subpixel,
                            const char *make, const char *model,
                            int32_t transform) {
  (void)output;
  (void)physical_width;
  (void)physical_height;
  (void)subpixel;
  (void)make;
  (void)model;
  (void)transform;
  struct output_info *oi = data;
  oi->x = x;
  oi->y = y;
  oi->transform = transform;
}

static void output_mode(void *data, struct wl_output *output, uint32_t flags,
                        int32_t width, int32_t height, int32_t refresh) {
  (void)output;
  (void)refresh;
  struct output_info *oi = data;
  if (flags & WL_OUTPUT_MODE_CURRENT) {
    oi->mode_w = width;
    oi->mode_h = height;
    oi->has_mode = 1;
  }
}

static void output_done(void *data, struct wl_output *output) {
  (void)data;
  (void)output;
}

static void output_scale(void *data, struct wl_output *output, int32_t factor) {
  (void)output;
  struct output_info *oi = data;
  if (factor > 0) {
    oi->scale = factor;
  }
}

static const struct wl_output_listener output_listener = {
    .geometry = output_geometry,
    .mode = output_mode,
    .done = output_done,
    .scale = output_scale,
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
  } else if (strcmp(interface, "wl_output") == 0 && rdata->outputs) {
    struct output_info *oi = calloc(1, sizeof(*oi));
    if (!oi) {
      return;
    }
    oi->scale = 1;
    uint32_t ver = version > 2 ? 2 : version;
    oi->output = wl_registry_bind(registry, name, &wl_output_interface, ver);
    if (!oi->output) {
      free(oi);
      return;
    }
    wl_output_add_listener(oi->output, &output_listener, oi);
    wl_list_insert(rdata->outputs, &oi->link);
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
  struct wl_list outputs;
  wl_list_init(&outputs);
  struct registry_data rdata = {&compositor, &wm_base, &outputs};

  struct wl_registry *registry = wl_display_get_registry(display);
  wl_registry_add_listener(registry, &registry_listener, &rdata);
  wl_display_roundtrip(display);
  wl_registry_destroy(registry);
  // Drain wl_output listeners.
  wl_display_roundtrip(display);

  int min_x = INT_MAX;
  int min_y = INT_MAX;
  int max_x = INT_MIN;
  int max_y = INT_MIN;
  struct output_info *oi, *tmp;
  wl_list_for_each(oi, &outputs, link) {
    if (!oi->has_mode || oi->mode_w <= 0 || oi->mode_h <= 0) {
      continue;
    }
    int lw = 0;
    int lh = 0;
    output_logical_size(oi, &lw, &lh);
    if (lw <= 0 || lh <= 0) {
      continue;
    }
    if (oi->x < min_x) {
      min_x = oi->x;
    }
    if (oi->y < min_y) {
      min_y = oi->y;
    }
    if (oi->x + lw > max_x) {
      max_x = oi->x + lw;
    }
    if (oi->y + lh > max_y) {
      max_y = oi->y + lh;
    }
  }

  wl_list_for_each_safe(oi, tmp, &outputs, link) {
    if (oi->output) {
      wl_output_destroy(oi->output);
    }
    wl_list_remove(&oi->link);
    free(oi);
  }

  if (min_x != INT_MAX && max_x > min_x && max_y > min_y) {
    if (width) {
      *width = max_x - min_x;
    }
    if (height) {
      *height = max_y - min_y;
    }
    if (wm_base) {
      xdg_wm_base_destroy(wm_base);
    }
    if (compositor) {
      wl_compositor_destroy(compositor);
    }
    return 0;
  }

  if (!compositor || !wm_base) {
    if (wm_base) {
      xdg_wm_base_destroy(wm_base);
    }
    if (compositor) {
      wl_compositor_destroy(compositor);
    }
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
  xdg_wm_base_destroy(wm_base);
  wl_compositor_destroy(compositor);

  if (width && *width <= 0) {
    return -1;
  }
  if (height && *height <= 0) {
    return -1;
  }
  return 0;
}
