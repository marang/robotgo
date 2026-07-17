//go:build linux && wayland
// +build linux,wayland

#include "window/get_bounds_wayland.h"
#include "xdg-output-unstable-v1-client-protocol.h"
#include <limits.h>
#include <stdlib.h>
#include <string.h>

struct registry_data {
  struct zxdg_output_manager_v1 **xdg_output_manager;
  struct wl_list *outputs;
};

struct output_info {
  struct wl_list link;
  struct wl_output *output;
  struct zxdg_output_v1 *xdg_output;
  uint32_t name;
  int32_t x;
  int32_t y;
  int32_t logical_x;
  int32_t logical_y;
  int32_t logical_w;
  int32_t logical_h;
  int32_t mode_w;
  int32_t mode_h;
  int32_t transform;
  int32_t scale;
  int has_mode;
  int has_logical_pos;
  int has_logical_size;
};

static void output_logical_size(const struct output_info *oi, int *lw, int *lh) {
  if (oi->has_logical_size && oi->logical_w > 0 && oi->logical_h > 0) {
    if (lw) {
      *lw = oi->logical_w;
    }
    if (lh) {
      *lh = oi->logical_h;
    }
    return;
  }

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

static void output_logical_origin(const struct output_info *oi, int *ox, int *oy) {
  if (oi->has_logical_pos) {
    if (ox) {
      *ox = oi->logical_x;
    }
    if (oy) {
      *oy = oi->logical_y;
    }
    return;
  }
  if (ox) {
    *ox = oi->x;
  }
  if (oy) {
    *oy = oi->y;
  }
}

struct output_rect_ref {
  const struct output_info *output;
  int x;
  int y;
  int w;
  int h;
  int primary;
};

static int output_rect(const struct output_info *oi,
                       struct output_rect_ref *rect) {
  if (!oi || !rect) {
    return -1;
  }

  int w = 0;
  int h = 0;
  int x = 0;
  int y = 0;
  output_logical_size(oi, &w, &h);
  output_logical_origin(oi, &x, &y);
  if (w <= 0 || h <= 0) {
    return -1;
  }

  long long right = (long long)x + (long long)w;
  long long bottom = (long long)y + (long long)h;
  if (right > INT_MAX || right < INT_MIN ||
      bottom > INT_MAX || bottom < INT_MIN) {
    return -1;
  }

  rect->output = oi;
  rect->x = x;
  rect->y = y;
  rect->w = w;
  rect->h = h;
  rect->primary = x <= 0 && right > 0 && y <= 0 && bottom > 0;
  return 0;
}

static int compare_output_rect_refs(const void *left, const void *right) {
  const struct output_rect_ref *a = left;
  const struct output_rect_ref *b = right;
  if (a->primary != b->primary) {
    return b->primary - a->primary;
  }
  if (a->y != b->y) {
    return a->y < b->y ? -1 : 1;
  }
  if (a->x != b->x) {
    return a->x < b->x ? -1 : 1;
  }
  if (a->output->name != b->output->name) {
    return a->output->name < b->output->name ? -1 : 1;
  }
  return 0;
}

static int collect_output_rects(struct wl_list *outputs,
                                struct output_rect_ref **rects_out) {
  if (!outputs || !rects_out) {
    return -1;
  }

  int capacity = 0;
  struct output_info *oi;
  wl_list_for_each(oi, outputs, link) {
    capacity++;
  }
  if (capacity <= 0) {
    return 0;
  }

  struct output_rect_ref *rects =
      calloc((size_t)capacity, sizeof(*rects));
  if (!rects) {
    return -1;
  }

  int count = 0;
  wl_list_for_each(oi, outputs, link) {
    if (output_rect(oi, &rects[count]) == 0) {
      count++;
    }
  }
  if (count == 0) {
    free(rects);
    return 0;
  }

  qsort(rects, (size_t)count, sizeof(*rects), compare_output_rect_refs);
  *rects_out = rects;
  return count;
}

static int resolve_output_rect(struct wl_list *outputs, int display_id,
                               int *x, int *y, int *width, int *height) {
  struct output_rect_ref *rects = NULL;
  int count = collect_output_rects(outputs, &rects);
  if (count <= 0) {
    return -1;
  }

  long long min_x;
  long long min_y;
  long long max_x;
  long long max_y;
  if (display_id >= 0) {
    if (display_id >= count) {
      free(rects);
      return -1;
    }
    const struct output_rect_ref *rect = &rects[display_id];
    min_x = rect->x;
    min_y = rect->y;
    max_x = (long long)rect->x + rect->w;
    max_y = (long long)rect->y + rect->h;
  } else {
    min_x = rects[0].x;
    min_y = rects[0].y;
    max_x = (long long)rects[0].x + rects[0].w;
    max_y = (long long)rects[0].y + rects[0].h;
    for (int i = 1; i < count; i++) {
      long long right = (long long)rects[i].x + rects[i].w;
      long long bottom = (long long)rects[i].y + rects[i].h;
      if (rects[i].x < min_x) {
        min_x = rects[i].x;
      }
      if (rects[i].y < min_y) {
        min_y = rects[i].y;
      }
      if (right > max_x) {
        max_x = right;
      }
      if (bottom > max_y) {
        max_y = bottom;
      }
    }
  }
  free(rects);

  long long w = max_x - min_x;
  long long h = max_y - min_y;
  if (min_x < INT_MIN || min_x > INT_MAX ||
      min_y < INT_MIN || min_y > INT_MAX ||
      w <= 0 || w > INT_MAX || h <= 0 || h > INT_MAX) {
    return -1;
  }
  if (x) {
    *x = (int)min_x;
  }
  if (y) {
    *y = (int)min_y;
  }
  if (width) {
    *width = (int)w;
  }
  if (height) {
    *height = (int)h;
  }
  return 0;
}

#ifdef ROBOTGO_WAYLAND_TEST
int robotgo_wayland_resolve_bounds_for_test(const int *values, int count,
                                             int display_id,
                                             int *x, int *y,
                                             int *width, int *height) {
  if (!values || count <= 0) {
    return -1;
  }

  struct wl_list outputs;
  wl_list_init(&outputs);
  for (int i = 0; i < count; i++) {
    const int *value = &values[i * 12];
    struct output_info *oi = calloc(1, sizeof(*oi));
    if (!oi) {
      struct output_info *item, *tmp;
      wl_list_for_each_safe(item, tmp, &outputs, link) {
        wl_list_remove(&item->link);
        free(item);
      }
      return -1;
    }
    oi->x = value[0];
    oi->y = value[1];
    oi->mode_w = value[2];
    oi->mode_h = value[3];
    oi->transform = value[4];
    oi->scale = value[5];
    oi->logical_x = value[6];
    oi->logical_y = value[7];
    oi->logical_w = value[8];
    oi->logical_h = value[9];
    oi->has_mode = value[2] > 0 && value[3] > 0;
    oi->has_logical_pos = (value[10] & 1) != 0;
    oi->has_logical_size = (value[10] & 2) != 0;
    oi->name = (uint32_t)value[11];
    wl_list_insert(outputs.prev, &oi->link);
  }

  int result =
      resolve_output_rect(&outputs, display_id, x, y, width, height);
  struct output_info *oi, *tmp;
  wl_list_for_each_safe(oi, tmp, &outputs, link) {
    wl_list_remove(&oi->link);
    free(oi);
  }
  return result;
}
#endif

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

static void xdg_output_logical_position(void *data, struct zxdg_output_v1 *zxdg_output_v1,
                                        int32_t x, int32_t y) {
  (void)zxdg_output_v1;
  struct output_info *oi = data;
  oi->logical_x = x;
  oi->logical_y = y;
  oi->has_logical_pos = 1;
}

static void xdg_output_logical_size(void *data, struct zxdg_output_v1 *zxdg_output_v1,
                                    int32_t width, int32_t height) {
  (void)zxdg_output_v1;
  struct output_info *oi = data;
  oi->logical_w = width;
  oi->logical_h = height;
  oi->has_logical_size = 1;
}

static void xdg_output_done(void *data, struct zxdg_output_v1 *zxdg_output_v1) {
  (void)data;
  (void)zxdg_output_v1;
}

static void xdg_output_name(void *data, struct zxdg_output_v1 *zxdg_output_v1,
                            const char *name) {
  (void)data;
  (void)zxdg_output_v1;
  (void)name;
}

static void xdg_output_description(void *data, struct zxdg_output_v1 *zxdg_output_v1,
                                   const char *description) {
  (void)data;
  (void)zxdg_output_v1;
  (void)description;
}

static const struct zxdg_output_v1_listener xdg_output_listener = {
    .logical_position = xdg_output_logical_position,
    .logical_size = xdg_output_logical_size,
    .done = xdg_output_done,
    .name = xdg_output_name,
    .description = xdg_output_description,
};

static void attach_xdg_output(struct output_info *oi,
                              struct zxdg_output_manager_v1 *manager) {
  if (!oi || !manager || !oi->output || oi->xdg_output) {
    return;
  }
  oi->xdg_output = zxdg_output_manager_v1_get_xdg_output(manager, oi->output);
  if (oi->xdg_output) {
    zxdg_output_v1_add_listener(oi->xdg_output, &xdg_output_listener, oi);
  }
}

static void registry_handle_global(void *data, struct wl_registry *registry,
                                   uint32_t name, const char *interface,
                                   uint32_t version) {
  struct registry_data *rdata = data;
  if (strcmp(interface, zxdg_output_manager_v1_interface.name) == 0) {
    uint32_t ver = version > 3 ? 3 : version;
    *rdata->xdg_output_manager =
        wl_registry_bind(registry, name, &zxdg_output_manager_v1_interface, ver);
    if (rdata->outputs && *rdata->xdg_output_manager) {
      struct output_info *it;
      wl_list_for_each(it, rdata->outputs, link) {
        attach_xdg_output(it, *rdata->xdg_output_manager);
      }
    }
  } else if (strcmp(interface, "wl_output") == 0 && rdata->outputs) {
    struct output_info *oi = calloc(1, sizeof(*oi));
    if (!oi) {
      return;
    }
    oi->scale = 1;
    oi->name = name;
    uint32_t ver = version > 2 ? 2 : version;
    oi->output = wl_registry_bind(registry, name, &wl_output_interface, ver);
    if (!oi->output) {
      free(oi);
      return;
    }
    wl_output_add_listener(oi->output, &output_listener, oi);
    if (rdata->xdg_output_manager && *rdata->xdg_output_manager) {
      attach_xdg_output(oi, *rdata->xdg_output_manager);
    }
    wl_list_insert(rdata->outputs, &oi->link);
  }
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_handle_global,
    .global_remove = NULL,
};

static void cleanup_outputs(struct wl_list *outputs) {
  if (!outputs) {
    return;
  }
  struct output_info *oi, *tmp;
  wl_list_for_each_safe(oi, tmp, outputs, link) {
    if (oi->xdg_output) {
      zxdg_output_v1_destroy(oi->xdg_output);
    }
    if (oi->output) {
      wl_output_destroy(oi->output);
    }
    wl_list_remove(&oi->link);
    free(oi);
  }
}

static void cleanup_xdg_output_manager(
    struct zxdg_output_manager_v1 *xdg_output_manager) {
  if (xdg_output_manager) {
    zxdg_output_manager_v1_destroy(xdg_output_manager);
  }
}

int get_screen_rect_wayland(struct wl_display *display, int display_id,
                            int *x, int *y, int *width, int *height) {
  if (!display) {
    return -1;
  }

  struct zxdg_output_manager_v1 *xdg_output_manager = NULL;
  struct wl_list outputs;
  wl_list_init(&outputs);
  struct registry_data rdata = {&xdg_output_manager, &outputs};

  struct wl_registry *registry = wl_display_get_registry(display);
  if (!registry ||
      wl_registry_add_listener(registry, &registry_listener, &rdata) != 0) {
    if (registry) {
      wl_registry_destroy(registry);
    }
    return -1;
  }
  int ready = wl_display_roundtrip(display) >= 0;
  wl_registry_destroy(registry);
  // Drain wl_output listeners.
  if (ready) {
    ready = wl_display_roundtrip(display) >= 0;
  }
  if (!ready) {
    cleanup_outputs(&outputs);
    cleanup_xdg_output_manager(xdg_output_manager);
    return -1;
  }

  int resolved_x = 0;
  int resolved_y = 0;
  int resolved_w = 0;
  int resolved_h = 0;
  int resolved = resolve_output_rect(&outputs, display_id,
                                     &resolved_x, &resolved_y,
                                     &resolved_w, &resolved_h);
  cleanup_outputs(&outputs);

  if (resolved == 0) {
    if (x) {
      *x = resolved_x;
    }
    if (y) {
      *y = resolved_y;
    }
    if (width) {
      *width = resolved_w;
    }
    if (height) {
      *height = resolved_h;
    }
    cleanup_xdg_output_manager(xdg_output_manager);
    return 0;
  }

  cleanup_xdg_output_manager(xdg_output_manager);
  return -1;
}

int get_bounds_wayland(struct wl_display *display, int *width, int *height) {
  return get_screen_rect_wayland(display, -1, NULL, NULL, width, height);
}

static int query_display_count(struct wl_display *display) {
  if (!display) {
    return 0;
  }

  struct zxdg_output_manager_v1 *xdg_output_manager = NULL;
  struct wl_list outputs;
  wl_list_init(&outputs);
  struct registry_data rdata = {&xdg_output_manager, &outputs};

  struct wl_registry *registry = wl_display_get_registry(display);
  if (!registry) {
    return 0;
  }
  int ready =
      wl_registry_add_listener(registry, &registry_listener, &rdata) == 0 &&
      wl_display_roundtrip(display) >= 0;
  wl_registry_destroy(registry);
  if (ready) {
    ready = wl_display_roundtrip(display) >= 0;
  }

  struct output_rect_ref *rects = NULL;
  int count = ready ? collect_output_rects(&outputs, &rects) : 0;
  free(rects);

  cleanup_outputs(&outputs);
  cleanup_xdg_output_manager(xdg_output_manager);
  return count > 0 ? count : 0;
}

int get_num_displays_wayland(struct wl_display *display) {
  return query_display_count(display);
}

int get_main_display_wayland(struct wl_display *display) {
  return query_display_count(display) > 0 ? 0 : -1;
}
