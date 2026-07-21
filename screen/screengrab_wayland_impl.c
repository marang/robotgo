// go:build ignore
//  +build ignore

#define _GNU_SOURCE
// This file is included from screengrab_c.h, which provides necessary
// declarations. It implements Wayland screen capture via the wlr-screencopy
// protocol without relying on external utilities.

#include "../linux-dmabuf-unstable-v1-client-protocol.h"
#include "../wlr-screencopy-unstable-v1-client-protocol.h"
#include "../xdg-output-unstable-v1-client-protocol.h"
#include <errno.h>
#include <fcntl.h>
#include <gbm.h>
#include <drm_fourcc.h>
#include <xf86drm.h>
#include <limits.h>
#include <poll.h>
#include <stdint.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include <time.h>
#include <wayland-client.h>

#ifndef MFD_CLOEXEC
#define MFD_CLOEXEC 0x0001
#endif

#define WAYLAND_BACKEND_DMABUF 0
#define WAYLAND_BACKEND_WL_SHM 1

static int robotgo_memfd_create(const char *name, unsigned int flags) {
#ifdef SYS_memfd_create
  return (int)syscall(SYS_memfd_create, name, flags);
#else
  (void)name;
  (void)flags;
  errno = ENOSYS;
  return -1;
#endif
}

#if defined(IS_LINUX)

static inline int screencopy_pixel_to_bitmap_bgra(uint8_t *dst,
                                                  const uint8_t *src,
                                                  uint32_t format,
                                                  int using_dmabuf) {
  const uint32_t argb =
      using_dmabuf ? DRM_FORMAT_ARGB8888 : WL_SHM_FORMAT_ARGB8888;
  const uint32_t xrgb =
      using_dmabuf ? DRM_FORMAT_XRGB8888 : WL_SHM_FORMAT_XRGB8888;
  const uint32_t abgr =
      using_dmabuf ? DRM_FORMAT_ABGR8888 : WL_SHM_FORMAT_ABGR8888;
  const uint32_t xbgr =
      using_dmabuf ? DRM_FORMAT_XBGR8888 : WL_SHM_FORMAT_XBGR8888;

  if (format == argb) {
    dst[0] = src[0];
    dst[1] = src[1];
    dst[2] = src[2];
    dst[3] = src[3];
    return 1;
  }
  if (format == xrgb) {
    dst[0] = src[0];
    dst[1] = src[1];
    dst[2] = src[2];
    dst[3] = 0xff;
    return 1;
  }
  if (format == abgr) {
    dst[0] = src[2];
    dst[1] = src[1];
    dst[2] = src[0];
    dst[3] = src[3];
    return 1;
  }
  if (format == xbgr) {
    dst[0] = src[2];
    dst[1] = src[1];
    dst[2] = src[0];
    dst[3] = 0xff;
    return 1;
  }
  return 0;
}

#if defined(ROBOTGO_WAYLAND_TEST)
int robotgo_wayland_pixel_to_bitmap_bgra(uint8_t *dst, const uint8_t *src,
                                         uint32_t format, int using_dmabuf) {
  return screencopy_pixel_to_bitmap_bgra(dst, src, format, using_dmabuf);
}
#endif

static int screencopy_pixel_format_supported(uint32_t format,
                                             int using_dmabuf) {
  uint8_t src[4] = {0};
  uint8_t dst[4];
  return screencopy_pixel_to_bitmap_bgra(dst, src, format, using_dmabuf);
}

struct fm_entry {
  uint32_t format;
  uint32_t pad;
  uint64_t modifier;
};

struct format_modifier {
  uint32_t format;
  uint64_t modifier;
};

struct feedback {
  struct zwp_linux_dmabuf_feedback_v1 *fb;
  dev_t main_dev;
  struct fm_entry *table;
  size_t table_count;
  int tranche_main;
  struct format_modifier *mods;
  size_t mods_len;
  size_t mods_cap;
};

struct output {
  struct wl_list link;
  struct wl_output *wl_output;
  struct zxdg_output_v1 *xdg_output;
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
  uint32_t name;
};

struct int_rect {
  int x;
  int y;
  int w;
  int h;
};

static void output_logical_size(const struct output *out, int *lw, int *lh) {
  if (out->has_logical_size && out->logical_w > 0 && out->logical_h > 0) {
    if (lw) {
      *lw = out->logical_w;
    }
    if (lh) {
      *lh = out->logical_h;
    }
    return;
  }

  int s = out->scale > 0 ? out->scale : 1;
  int w = out->mode_w > 0 ? out->mode_w / s : 0;
  int h = out->mode_h > 0 ? out->mode_h / s : 0;

  switch (out->transform) {
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

static void output_logical_origin(const struct output *out, int *ox, int *oy) {
  if (out->has_logical_pos) {
    if (ox) {
      *ox = out->logical_x;
    }
    if (oy) {
      *oy = out->logical_y;
    }
    return;
  }
  if (ox) {
    *ox = out->x;
  }
  if (oy) {
    *oy = out->y;
  }
}

struct output_ref {
  struct output *output;
  int x;
  int y;
  int w;
  int h;
  int primary;
};

static int compare_output_refs(const void *left, const void *right) {
  const struct output_ref *a = left;
  const struct output_ref *b = right;
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

static struct output *output_by_stable_index(struct wl_list *outputs,
                                             int display_id) {
  if (!outputs || display_id < 0) {
    return NULL;
  }

  int capacity = 0;
  struct output *it;
  wl_list_for_each(it, outputs, link) {
    capacity++;
  }
  if (capacity <= 0) {
    return NULL;
  }

  struct output_ref *refs = calloc((size_t)capacity, sizeof(*refs));
  if (!refs) {
    return NULL;
  }
  int count = 0;
  wl_list_for_each(it, outputs, link) {
    int x = 0;
    int y = 0;
    int w = 0;
    int h = 0;
    output_logical_origin(it, &x, &y);
    output_logical_size(it, &w, &h);
    long long right = (long long)x + w;
    long long bottom = (long long)y + h;
    if (w <= 0 || h <= 0 ||
        right > INT_MAX || right < INT_MIN ||
        bottom > INT_MAX || bottom < INT_MIN) {
      continue;
    }
    refs[count].output = it;
    refs[count].x = x;
    refs[count].y = y;
    refs[count].w = w;
    refs[count].h = h;
    refs[count].primary =
        x <= 0 && right > 0 && y <= 0 && bottom > 0;
    count++;
  }
  if (display_id >= count) {
    free(refs);
    return NULL;
  }

  qsort(refs, (size_t)count, sizeof(*refs), compare_output_refs);
  struct output *selected = refs[display_id].output;
  free(refs);
  return selected;
}

static long long rect_intersection_area(struct int_rect a, struct int_rect b) {
  int x1 = a.x > b.x ? a.x : b.x;
  int y1 = a.y > b.y ? a.y : b.y;
  long long x2a = (long long)a.x + (long long)a.w;
  long long y2a = (long long)a.y + (long long)a.h;
  long long x2b = (long long)b.x + (long long)b.w;
  long long y2b = (long long)b.y + (long long)b.h;
  long long x2 = x2a < x2b ? x2a : x2b;
  long long y2 = y2a < y2b ? y2a : y2b;
  if (x2 <= x1 || y2 <= y1) {
    return 0;
  }
  return (x2 - x1) * (y2 - y1);
}

static int rect_candidate_is_better(struct int_rect request,
                                    struct int_rect candidate,
                                    long long best_area,
                                    long long *candidate_area) {
  long long area = rect_intersection_area(request, candidate);
  if (candidate_area) {
    *candidate_area = area;
  }
  return area > best_area;
}

static int scale_floor_coord(int value, int extent, int logical_extent) {
  return (int)(((long long)value * (long long)extent) /
               (long long)logical_extent);
}

static int scale_ceil_coord(int value, int extent, int logical_extent) {
  long long product = (long long)value * (long long)extent;
  return (int)((product + (long long)logical_extent - 1) /
               (long long)logical_extent);
}

static int transform_is_rotated(int32_t transform) {
  return transform == WL_OUTPUT_TRANSFORM_90 ||
         transform == WL_OUTPUT_TRANSFORM_270 ||
         transform == WL_OUTPUT_TRANSFORM_FLIPPED_90 ||
         transform == WL_OUTPUT_TRANSFORM_FLIPPED_270;
}

static void map_logical_rect_to_buffer(const struct output *out, int cap_w, int cap_h,
                                       int logical_w, int logical_h, int *x, int *y,
                                       int *w, int *h) {
  if (!out || !x || !y || !w || !h || cap_w <= 0 || cap_h <= 0 ||
      logical_w <= 0 || logical_h <= 0) {
    return;
  }

  int lx = *x;
  int ly = *y;
  int lw = *w;
  int lh = *h;
  long long x2_long = (lw <= 0) ? logical_w : (long long)lx + (long long)lw;
  long long y2_long = (lh <= 0) ? logical_h : (long long)ly + (long long)lh;
  if (lx < 0) {
    lx = 0;
  }
  if (ly < 0) {
    ly = 0;
  }

  int x2 = x2_long > INT_MAX ? INT_MAX : (int)x2_long;
  int y2 = y2_long > INT_MAX ? INT_MAX : (int)y2_long;
  if (x2 > logical_w) {
    x2 = logical_w;
  }
  if (y2 > logical_h) {
    y2 = logical_h;
  }
  if (lx > logical_w) {
    lx = logical_w;
  }
  if (ly > logical_h) {
    ly = logical_h;
  }
  if (x2 < lx) {
    x2 = lx;
  }
  if (y2 < ly) {
    y2 = ly;
  }

  long long direct_err = llabs((long long)cap_w * (long long)logical_h -
                               (long long)cap_h * (long long)logical_w);
  long long swapped_err = llabs((long long)cap_w * (long long)logical_w -
                                (long long)cap_h * (long long)logical_h);
  int use_transform = 0;
  if (out->transform != WL_OUTPUT_TRANSFORM_NORMAL) {
    if (transform_is_rotated(out->transform)) {
      use_transform = swapped_err <= direct_err;
    } else {
      use_transform = 1;
    }
  }

  if (!use_transform) {
    int bx1 = scale_floor_coord(lx, cap_w, logical_w);
    int by1 = scale_floor_coord(ly, cap_h, logical_h);
    int bx2 = scale_ceil_coord(x2, cap_w, logical_w);
    int by2 = scale_ceil_coord(y2, cap_h, logical_h);
    if (bx1 < 0) bx1 = 0;
    if (by1 < 0) by1 = 0;
    if (bx2 > cap_w) bx2 = cap_w;
    if (by2 > cap_h) by2 = cap_h;
    if (bx2 < bx1) bx2 = bx1;
    if (by2 < by1) by2 = by1;
    *x = bx1;
    *y = by1;
    *w = bx2 - bx1;
    *h = by2 - by1;
    return;
  }

  int tx1 = lx, tx2 = x2, tx_extent = logical_w;
  int ty1 = ly, ty2 = y2, ty_extent = logical_h;
  switch (out->transform) {
  case WL_OUTPUT_TRANSFORM_90:
    tx1 = ly;
    tx2 = y2;
    tx_extent = logical_h;
    ty1 = logical_w - x2;
    ty2 = logical_w - lx;
    ty_extent = logical_w;
    break;
  case WL_OUTPUT_TRANSFORM_180:
    tx1 = logical_w - x2;
    tx2 = logical_w - lx;
    ty1 = logical_h - y2;
    ty2 = logical_h - ly;
    break;
  case WL_OUTPUT_TRANSFORM_270:
    tx1 = logical_h - y2;
    tx2 = logical_h - ly;
    tx_extent = logical_h;
    ty1 = lx;
    ty2 = x2;
    ty_extent = logical_w;
    break;
  case WL_OUTPUT_TRANSFORM_FLIPPED:
    tx1 = logical_w - x2;
    tx2 = logical_w - lx;
    break;
  case WL_OUTPUT_TRANSFORM_FLIPPED_90:
    tx1 = ly;
    tx2 = y2;
    tx_extent = logical_h;
    ty1 = lx;
    ty2 = x2;
    ty_extent = logical_w;
    break;
  case WL_OUTPUT_TRANSFORM_FLIPPED_180:
    ty1 = logical_h - y2;
    ty2 = logical_h - ly;
    break;
  case WL_OUTPUT_TRANSFORM_FLIPPED_270:
    tx1 = logical_h - y2;
    tx2 = logical_h - ly;
    tx_extent = logical_h;
    ty1 = logical_w - x2;
    ty2 = logical_w - lx;
    ty_extent = logical_w;
    break;
  case WL_OUTPUT_TRANSFORM_NORMAL:
  default:
    break;
  }

  int bx1 = scale_floor_coord(tx1, cap_w, tx_extent);
  int by1 = scale_floor_coord(ty1, cap_h, ty_extent);
  int bx2 = scale_ceil_coord(tx2, cap_w, tx_extent);
  int by2 = scale_ceil_coord(ty2, cap_h, ty_extent);

  if (bx1 < 0) bx1 = 0;
  if (by1 < 0) by1 = 0;
  if (bx2 > cap_w) bx2 = cap_w;
  if (by2 > cap_h) by2 = cap_h;
  if (bx2 < bx1) bx2 = bx1;
  if (by2 < by1) by2 = by1;

  *x = bx1;
  *y = by1;
  *w = bx2 - bx1;
  *h = by2 - by1;
}

#ifdef ROBOTGO_WAYLAND_TEST
void robotgo_wayland_map_logical_rect_for_test(int cap_w, int cap_h,
                                                int logical_w, int logical_h,
                                                int transform, int *x, int *y,
                                                int *w, int *h) {
  struct output out = {0};
  out.transform = transform;
  map_logical_rect_to_buffer(&out, cap_w, cap_h, logical_w, logical_h, x, y,
                             w, h);
}

int robotgo_wayland_select_output_rect_for_test(int req_x, int req_y,
                                                 int req_w, int req_h,
                                                 const int *rects,
                                                 int rect_count) {
  if (!rects || rect_count <= 0) {
    return -1;
  }
  struct int_rect request = {req_x, req_y, req_w, req_h};
  long long best_area = -1;
  int best_index = -1;
  for (int index = 0; index < rect_count; index++) {
    struct int_rect candidate = {rects[index * 4], rects[index * 4 + 1],
                                 rects[index * 4 + 2], rects[index * 4 + 3]};
    long long area = 0;
    if (rect_candidate_is_better(request, candidate, best_area, &area)) {
      best_area = area;
      best_index = index;
    }
  }
  return best_index;
}

int robotgo_wayland_stable_output_name_for_test(const int *values,
                                                 int output_count,
                                                 int display_id) {
  if (!values || output_count <= 0) {
    return -1;
  }
  struct wl_list outputs;
  wl_list_init(&outputs);
  for (int i = 0; i < output_count; i++) {
    const int *value = &values[i * 5];
    struct output *out = calloc(1, sizeof(*out));
    if (!out) {
      struct output *item, *tmp;
      wl_list_for_each_safe(item, tmp, &outputs, link) {
        wl_list_remove(&item->link);
        free(item);
      }
      return -1;
    }
    out->logical_x = value[0];
    out->logical_y = value[1];
    out->logical_w = value[2];
    out->logical_h = value[3];
    out->has_logical_pos = 1;
    out->has_logical_size = 1;
    out->name = (uint32_t)value[4];
    wl_list_insert(outputs.prev, &out->link);
  }

  struct output *selected = output_by_stable_index(&outputs, display_id);
  int result = selected ? (int)selected->name : -1;
  struct output *out, *tmp;
  wl_list_for_each_safe(out, tmp, &outputs, link) {
    wl_list_remove(&out->link);
    free(out);
  }
  return result;
}
#endif

static void output_geometry(void *data, struct wl_output *output, int32_t x,
                            int32_t y, int32_t physical_width,
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
  struct output *out = data;
  out->x = x;
  out->y = y;
  out->transform = transform;
}

static void output_mode(void *data, struct wl_output *output, uint32_t flags,
                        int32_t width, int32_t height, int32_t refresh) {
  (void)output;
  (void)refresh;
  struct output *out = data;
  if (flags & WL_OUTPUT_MODE_CURRENT) {
    out->mode_w = width;
    out->mode_h = height;
    out->has_mode = 1;
  }
}

static void output_done(void *data, struct wl_output *output) {
  (void)data;
  (void)output;
}

static void output_scale(void *data, struct wl_output *output, int32_t factor) {
  (void)output;
  struct output *out = data;
  if (factor > 0) {
    out->scale = factor;
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
  struct output *out = data;
  out->logical_x = x;
  out->logical_y = y;
  out->has_logical_pos = 1;
}

static void xdg_output_logical_size(void *data, struct zxdg_output_v1 *zxdg_output_v1,
                                    int32_t width, int32_t height) {
  (void)zxdg_output_v1;
  struct output *out = data;
  out->logical_w = width;
  out->logical_h = height;
  out->has_logical_size = 1;
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

int drm_find_render_node(dev_t dev) {
  int count = drmGetDevices2(0, NULL, 0);
  if (count <= 0) {
    return -1;
  }
  drmDevicePtr *devs = calloc((size_t)count, sizeof(*devs));
  if (!devs) {
    return -1;
  }
  count = drmGetDevices2(0, devs, count);
  if (count < 0) {
    free(devs);
    return -1;
  }
  int fd = -1;
  for (int i = 0; i < count; ++i) {
    drmDevicePtr d = devs[i];
    if (!d) continue;
    if (!(d->available_nodes & (1 << DRM_NODE_RENDER)) || !d->nodes[DRM_NODE_RENDER]) {
      continue;
    }
    struct stat st;
    if (stat(d->nodes[DRM_NODE_RENDER], &st) == 0) {
      if (major(st.st_rdev) == major(dev) && minor(st.st_rdev) == minor(dev)) {
        fd = open(d->nodes[DRM_NODE_RENDER], O_RDWR | O_CLOEXEC);
        break;
      }
    }
  }
  for (int i = 0; i < count; ++i) {
    drmFreeDevice(&devs[i]);
  }
  free(devs);
  return fd;
}

struct capture {
  struct wl_display *display;
  struct wl_registry *registry;
  struct wl_shm *shm;
  struct zwp_linux_dmabuf_v1 *dmabuf;
  struct zxdg_output_manager_v1 *xdg_output_manager;
  struct zwlr_screencopy_manager_v1 *manager;
  struct zwlr_screencopy_frame_v1 *frame;
  struct wl_buffer *buffer;
  struct gbm_device *gbm;
  struct gbm_bo *bo;
  void *map_data;
  int drm_fd;
  struct feedback fb;
  struct wl_list outputs;
  void *data;
  int width;
  int height;
  int stride;
  int done;
  int failed;
  int using_dmabuf;
  uint32_t format;
  int err_code;
};

static _Atomic uint32_t robotgo_last_screencopy_version = 0;

static void attach_xdg_output(struct capture *cap, struct output *out) {
  if (!cap || !out || !cap->xdg_output_manager || !out->wl_output || out->xdg_output) {
    return;
  }
  out->xdg_output =
      zxdg_output_manager_v1_get_xdg_output(cap->xdg_output_manager, out->wl_output);
  if (out->xdg_output) {
    zxdg_output_v1_add_listener(out->xdg_output, &xdg_output_listener, out);
  }
}

static void registry_global(void *data, struct wl_registry *registry,
                            uint32_t name, const char *interface,
                            uint32_t version) {
  struct capture *cap = data;
  if (strcmp(interface, wl_shm_interface.name) == 0) {
    cap->shm = wl_registry_bind(registry, name, &wl_shm_interface, 1);
  } else if (strcmp(interface, zwlr_screencopy_manager_v1_interface.name) ==
             0) {
    uint32_t ver = version > 3 ? 3 : version;
    if (ver > 1 && cap->shm == NULL) {
      ver = 1;
    }
    cap->manager = wl_registry_bind(registry, name,
                                    &zwlr_screencopy_manager_v1_interface, ver);
  } else if (strcmp(interface, zwp_linux_dmabuf_v1_interface.name) == 0) {
    uint32_t ver = version > 4 ? 4 : version;
    cap->dmabuf =
        wl_registry_bind(registry, name, &zwp_linux_dmabuf_v1_interface, ver);
  } else if (strcmp(interface, zxdg_output_manager_v1_interface.name) == 0) {
    uint32_t ver = version > 3 ? 3 : version;
    cap->xdg_output_manager =
        wl_registry_bind(registry, name, &zxdg_output_manager_v1_interface, ver);
    if (cap->xdg_output_manager) {
      struct output *it;
      wl_list_for_each(it, &cap->outputs, link) {
        attach_xdg_output(cap, it);
      }
    }
  } else if (strcmp(interface, wl_output_interface.name) == 0) {
    struct output *out = malloc(sizeof(*out));
    if (!out) {
      return;
    }
    memset(out, 0, sizeof(*out));
    out->scale = 1;
    out->name = name;
    uint32_t ver = version > 2 ? 2 : version;
    out->wl_output = wl_registry_bind(registry, name, &wl_output_interface, ver);
    if (out->wl_output) {
      wl_output_add_listener(out->wl_output, &output_listener, out);
      attach_xdg_output(cap, out);
    }
    wl_list_insert(&cap->outputs, &out->link);
  }
}

static void registry_remove(void *data, struct wl_registry *registry,
                            uint32_t name) {
  (void)data;
  (void)registry;
  (void)name;
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_global,
    .global_remove = registry_remove,
};

static void feedback_done(void *data, struct zwp_linux_dmabuf_feedback_v1 *fb) {
  (void)fb;
  struct capture *cap = data;
  if (cap->fb.table) {
    munmap(cap->fb.table, cap->fb.table_count * sizeof(struct fm_entry));
    cap->fb.table = NULL;
  }
  cap->fb.table_count = 0;
  if (cap->fb.fb) {
    zwp_linux_dmabuf_feedback_v1_destroy(cap->fb.fb);
    cap->fb.fb = NULL;
  }
}

static void feedback_format_table(void *data,
                                  struct zwp_linux_dmabuf_feedback_v1 *fb,
                                  int32_t fd, uint32_t size) {
  (void)fb;
  struct capture *cap = data;
  cap->fb.table = mmap(NULL, size, PROT_READ, MAP_PRIVATE, fd, 0);
  if (cap->fb.table == MAP_FAILED) {
    cap->fb.table = NULL;
    cap->fb.table_count = 0;
  } else {
    cap->fb.table_count = size / sizeof(struct fm_entry);
  }
  close(fd);
}

static void feedback_main_device(void *data,
                                 struct zwp_linux_dmabuf_feedback_v1 *fb,
                                 struct wl_array *dev) {
  (void)fb;
  struct capture *cap = data;
  if (dev->size >= sizeof(dev_t)) {
    cap->fb.main_dev = *(dev_t *)dev->data;
  }
}

static void feedback_tranche_target_device(
    void *data, struct zwp_linux_dmabuf_feedback_v1 *fb, struct wl_array *dev) {
  (void)fb;
  struct capture *cap = data;
  dev_t d = 0;
  if (dev->size >= sizeof(dev_t)) {
    d = *(dev_t *)dev->data;
  }
  cap->fb.tranche_main = (d == cap->fb.main_dev);
}

static void feedback_tranche_formats(void *data,
                                     struct zwp_linux_dmabuf_feedback_v1 *fb,
                                     struct wl_array *idxs) {
  (void)fb;
  struct capture *cap = data;
  if (!cap->fb.tranche_main || !cap->fb.table)
    return;
  uint16_t *ind = idxs->data;
  size_t count = idxs->size / sizeof(uint16_t);
  for (size_t i = 0; i < count; ++i) {
    uint16_t id = ind[i];
    if (id >= cap->fb.table_count)
      continue;
    struct fm_entry *e = &cap->fb.table[id];
    if (cap->fb.mods_len == cap->fb.mods_cap) {
      size_t new_cap = cap->fb.mods_cap ? cap->fb.mods_cap * 2 : 4;
      struct format_modifier *tmp =
          realloc(cap->fb.mods, new_cap * sizeof(*tmp));
      if (!tmp)
        continue;
      cap->fb.mods = tmp;
      cap->fb.mods_cap = new_cap;
    }
    cap->fb.mods[cap->fb.mods_len].format = e->format;
    cap->fb.mods[cap->fb.mods_len].modifier = e->modifier;
    cap->fb.mods_len++;
  }
}

static void feedback_tranche_done(void *data,
                                  struct zwp_linux_dmabuf_feedback_v1 *fb) {
  (void)data;
  (void)fb;
}

static void feedback_tranche_flags(void *data,
                                   struct zwp_linux_dmabuf_feedback_v1 *fb,
                                   uint32_t flags) {
  (void)data;
  (void)fb;
  (void)flags;
}

static const struct zwp_linux_dmabuf_feedback_v1_listener feedback_listener = {
    .done = feedback_done,
    .format_table = feedback_format_table,
    .main_device = feedback_main_device,
    .tranche_done = feedback_tranche_done,
    .tranche_target_device = feedback_tranche_target_device,
    .tranche_formats = feedback_tranche_formats,
    .tranche_flags = feedback_tranche_flags,
};

static void frame_buffer(void *data, struct zwlr_screencopy_frame_v1 *frame,
                         uint32_t format, uint32_t width, uint32_t height,
                         uint32_t stride) {
  (void)frame;
  (void)format;
  struct capture *cap = data;
  cap->width = (int)width;
  cap->height = (int)height;
  cap->stride = (int)stride;
  cap->format = format;

  if (!screencopy_pixel_format_supported(format, 0)) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrPixelFormat;
    return;
  }

  int fd = robotgo_memfd_create("robotgo-wl", MFD_CLOEXEC);
  if (fd < 0) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  size_t size = (size_t)stride * height;
  if (ftruncate(fd, (off_t)size) < 0) {
    close(fd);
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  cap->data = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
  if (cap->data == MAP_FAILED) {
    close(fd);
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  struct wl_shm_pool *pool = wl_shm_create_pool(cap->shm, fd, (int)size);
  if (!pool) {
    munmap(cap->data, size);
    cap->data = NULL;
    close(fd);
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  cap->buffer = wl_shm_pool_create_buffer(pool, 0, (int)width, (int)height,
                                          (int)stride, format);
  wl_shm_pool_destroy(pool);
  close(fd);
  if (!cap->buffer) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  zwlr_screencopy_frame_v1_copy(frame, cap->buffer);
}

static void frame_flags(void *data, struct zwlr_screencopy_frame_v1 *frame,
                        uint32_t flags) {
  (void)data;
  (void)frame;
  (void)flags;
}

static void frame_damage(void *data, struct zwlr_screencopy_frame_v1 *frame,
                         uint32_t x, uint32_t y, uint32_t width,
                         uint32_t height) {
  (void)data;
  (void)frame;
  (void)x;
  (void)y;
  (void)width;
  (void)height;
}

static void frame_linux_dmabuf(void *data,
                               struct zwlr_screencopy_frame_v1 *frame,
                               uint32_t format, uint32_t width,
                               uint32_t height) {
  (void)frame;
  struct capture *cap = data;
  cap->using_dmabuf = 1;
  cap->width = (int)width;
  cap->height = (int)height;
  cap->format = format;
  if (!screencopy_pixel_format_supported(format, 1)) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrPixelFormat;
  }
}

static void frame_buffer_done(void *data,
                              struct zwlr_screencopy_frame_v1 *frame) {
  struct capture *cap = data;
  if (!cap->using_dmabuf || cap->failed) {
    return;
  }
  cap->drm_fd = drm_find_render_node(cap->fb.main_dev);
  if (cap->drm_fd < 0) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrDmabufDevice;
    return;
  }
  cap->gbm = gbm_create_device(cap->drm_fd);
  if (!cap->gbm) {
    close(cap->drm_fd);
    cap->drm_fd = -1;
    cap->failed = 1;
    cap->err_code = ScreengrabErrDmabufDevice;
    return;
  }
  uint64_t mods[64];
  size_t mod_count = 0;
  for (size_t i = 0; i < cap->fb.mods_len && mod_count < 64; ++i) {
    if (cap->fb.mods[i].format == cap->format) {
      mods[mod_count++] = cap->fb.mods[i].modifier;
    }
  }
  if (mod_count > 0) {
    cap->bo = gbm_bo_create_with_modifiers(cap->gbm, (uint32_t)cap->width,
                                           (uint32_t)cap->height, cap->format,
                                           mods, mod_count);
  } else {
    if (!gbm_device_is_format_supported(
            cap->gbm, cap->format, GBM_BO_USE_LINEAR | GBM_BO_USE_RENDERING)) {
      gbm_device_destroy(cap->gbm);
      cap->gbm = NULL;
      close(cap->drm_fd);
      cap->drm_fd = -1;
      cap->failed = 1;
      cap->err_code = ScreengrabErrDmabufModifiers;
      return;
    }
    cap->bo =
        gbm_bo_create(cap->gbm, (uint32_t)cap->width, (uint32_t)cap->height,
                      cap->format, GBM_BO_USE_LINEAR | GBM_BO_USE_RENDERING);
  }
  if (!cap->bo) {
    gbm_device_destroy(cap->gbm);
    cap->gbm = NULL;
    close(cap->drm_fd);
    cap->drm_fd = -1;
    cap->failed = 1;
    cap->err_code = ScreengrabErrDmabufModifiers;
    return;
  }
  int fd = gbm_bo_get_fd(cap->bo);
  if (fd < 0) {
    gbm_bo_destroy(cap->bo);
    cap->bo = NULL;
    gbm_device_destroy(cap->gbm);
    cap->gbm = NULL;
    close(cap->drm_fd);
    cap->drm_fd = -1;
    cap->failed = 1;
    cap->err_code = ScreengrabErrDmabufImport;
    return;
  }
  cap->stride = (int)gbm_bo_get_stride(cap->bo);
  struct zwp_linux_buffer_params_v1 *params =
      zwp_linux_dmabuf_v1_create_params(cap->dmabuf);
  zwp_linux_buffer_params_v1_add(params, fd, 0, 0, (uint32_t)cap->stride, 0, 0);
  cap->buffer = zwp_linux_buffer_params_v1_create_immed(
      params, cap->width, cap->height, cap->format, 0);
  zwp_linux_buffer_params_v1_destroy(params);
  close(fd);
  zwlr_screencopy_frame_v1_copy(frame, cap->buffer);
}

static void frame_ready(void *data, struct zwlr_screencopy_frame_v1 *frame,
                        uint32_t tv_sec_hi, uint32_t tv_sec_lo,
                        uint32_t tv_nsec) {
  (void)frame;
  (void)tv_sec_hi;
  (void)tv_sec_lo;
  (void)tv_nsec;
  struct capture *cap = data;
  cap->done = 1;
}

static void frame_failed(void *data, struct zwlr_screencopy_frame_v1 *frame) {
  (void)frame;
  struct capture *cap = data;
  cap->failed = 1;
  cap->err_code = ScreengrabErrFailed;
}

static const struct zwlr_screencopy_frame_v1_listener frame_listener = {
    .buffer = frame_buffer,
    .flags = frame_flags,
    .damage = frame_damage,
    .linux_dmabuf = frame_linux_dmabuf,
    .buffer_done = frame_buffer_done,
    .ready = frame_ready,
    .failed = frame_failed,
};

static long monotonic_millis(void) {
  struct timespec ts;
#ifdef CLOCK_MONOTONIC
  if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
    return -1;
  }
#else
  if (clock_gettime(CLOCK_REALTIME, &ts) != 0) {
    return -1;
  }
#endif
  return ts.tv_sec * 1000L + ts.tv_nsec / 1000000L;
}

// Dispatch pending Wayland events while respecting a real deadline. A plain
// wl_display_dispatch() can block forever and therefore cannot implement a
// timeout by checking the clock after it returns.
static int dispatch_until(struct wl_display *display, long deadline_ms) {
  while (wl_display_prepare_read(display) != 0) {
    if (wl_display_dispatch_pending(display) < 0) {
      return -1;
    }
  }

  if (wl_display_flush(display) < 0 && errno != EAGAIN) {
    wl_display_cancel_read(display);
    return -1;
  }

  long now = monotonic_millis();
  if (now < 0 || now >= deadline_ms) {
    wl_display_cancel_read(display);
    return 0;
  }
  long remaining = deadline_ms - now;
  if (remaining > INT_MAX) {
    remaining = INT_MAX;
  }

  struct pollfd pfd = {
      .fd = wl_display_get_fd(display),
      .events = POLLIN,
      .revents = 0,
  };
  int ready;
  do {
    ready = poll(&pfd, 1, (int)remaining);
  } while (ready < 0 && errno == EINTR);

  if (ready <= 0 || (pfd.revents & (POLLERR | POLLHUP | POLLNVAL))) {
    wl_display_cancel_read(display);
    return ready == 0 ? 0 : -1;
  }
  if (wl_display_read_events(display) < 0) {
    return -1;
  }
  if (wl_display_dispatch_pending(display) < 0) {
    return -1;
  }
  return 1;
}

static void cleanup_capture(struct capture *cap) {
  if (cap->using_dmabuf) {
    if (cap->data && cap->bo && cap->map_data) {
      gbm_bo_unmap(cap->bo, cap->map_data);
    }
    if (cap->bo) {
      gbm_bo_destroy(cap->bo);
    }
    if (cap->gbm) {
      gbm_device_destroy(cap->gbm);
    }
    if (cap->drm_fd >= 0) {
      close(cap->drm_fd);
    }
  } else if (cap->data) {
    munmap(cap->data, (size_t)cap->stride * (size_t)cap->height);
  }
  if (cap->buffer) {
    wl_buffer_destroy(cap->buffer);
  }
  if (cap->frame) {
    zwlr_screencopy_frame_v1_destroy(cap->frame);
  }
  struct output *out, *tmp;
  wl_list_for_each_safe(out, tmp, &cap->outputs, link) {
    if (out->xdg_output) {
      zxdg_output_v1_destroy(out->xdg_output);
    }
    wl_output_destroy(out->wl_output);
    free(out);
  }
  if (cap->shm) {
    wl_shm_destroy(cap->shm);
  }
  if (cap->dmabuf) {
    zwp_linux_dmabuf_v1_destroy(cap->dmabuf);
  }
  if (cap->xdg_output_manager) {
    zxdg_output_manager_v1_destroy(cap->xdg_output_manager);
  }
  if (cap->manager) {
    zwlr_screencopy_manager_v1_destroy(cap->manager);
  }
  if (cap->fb.fb) {
    if (cap->fb.table) {
      munmap(cap->fb.table, cap->fb.table_count * sizeof(struct fm_entry));
    }
    zwp_linux_dmabuf_feedback_v1_destroy(cap->fb.fb);
  }
  if (cap->fb.mods) {
    free(cap->fb.mods);
  }
  if (cap->registry) {
    wl_registry_destroy(cap->registry);
  }
  if (cap->display) {
    wl_display_disconnect(cap->display);
  }
}

int robotgo_wayland_screencopy_ready(void) {
  struct capture cap = {0};
  cap.drm_fd = -1;
  wl_list_init(&cap.outputs);
  atomic_store(&robotgo_last_screencopy_version, 0);

  cap.display = wl_display_connect(NULL);
  if (!cap.display) {
    return 0;
  }
  cap.registry = wl_display_get_registry(cap.display);
  if (!cap.registry) {
    cleanup_capture(&cap);
    return 0;
  }
  wl_registry_add_listener(cap.registry, &registry_listener, &cap);
  if (wl_display_roundtrip(cap.display) < 0 ||
      wl_display_roundtrip(cap.display) < 0) {
    cleanup_capture(&cap);
    return 0;
  }

  if (cap.manager) {
    atomic_store(&robotgo_last_screencopy_version,
                 zwlr_screencopy_manager_v1_get_version(cap.manager));
  }
  int ready = cap.manager != NULL && !wl_list_empty(&cap.outputs) &&
              (cap.shm != NULL || cap.dmabuf != NULL);
  cleanup_capture(&cap);
  return ready;
}

uint32_t robotgo_wayland_screencopy_version(void) {
  return atomic_load(&robotgo_last_screencopy_version);
}

// capture_screen_wayland_impl performs the actual capture logic. The backend
// parameter selects whether dmabuf (zero-copy) or wl_shm (shared memory)
// should be used.
MMBitmapRef capture_screen_wayland_impl(int32_t x, int32_t y, int32_t w,
                                        int32_t h, int32_t display_id,
                                        int8_t isPid, int32_t backend,
                                        int32_t *err) {
  (void)isPid;
  if (err) {
    *err = ScreengrabOK;
  }
  struct capture cap = {0};
  cap.drm_fd = -1;
  wl_list_init(&cap.outputs);

  cap.display = wl_display_connect(NULL);
  if (!cap.display) {
    if (err) {
      *err = ScreengrabErrDisplay;
    }
    return NULL;
  }
  cap.registry = wl_display_get_registry(cap.display);
  if (!cap.registry) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  wl_registry_add_listener(cap.registry, &registry_listener, &cap);
  wl_display_roundtrip(cap.display);
  // Drain output listeners so geometry/mode/scale metadata is populated.
  wl_display_roundtrip(cap.display);

  if (backend == WAYLAND_BACKEND_WL_SHM) {
    if (cap.dmabuf) {
      zwp_linux_dmabuf_v1_destroy(cap.dmabuf);
      cap.dmabuf = NULL;
    }
  } else if (backend == WAYLAND_BACKEND_DMABUF && !cap.dmabuf) {
    if (err) {
      *err = ScreengrabErrDmabufDevice;
    }
    cleanup_capture(&cap);
    return NULL;
  }

  if (!cap.manager) {
    if (err) {
      *err = ScreengrabErrNoManager;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  if (wl_list_empty(&cap.outputs)) {
    if (err) {
      *err = ScreengrabErrNoOutputs;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  if (!cap.shm && !cap.dmabuf) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  if (cap.dmabuf &&
      wl_proxy_get_version((struct wl_proxy *)cap.dmabuf) >= 4) {
    cap.fb.fb = zwp_linux_dmabuf_v1_get_default_feedback(cap.dmabuf);
    if (cap.fb.fb) {
      zwp_linux_dmabuf_feedback_v1_add_listener(cap.fb.fb, &feedback_listener,
                                                &cap);
      wl_display_roundtrip(cap.display);
    }
  }
  struct output *out = NULL;
  if (display_id >= 0) {
    out = output_by_stable_index(&cap.outputs, display_id);
    if (!out) {
      if (err) {
        *err = ScreengrabErrNoOutputs;
      }
      cleanup_capture(&cap);
      return NULL;
    }
  }
  if (!out) {
    struct int_rect req = {x, y, w > 0 ? w : 1, h > 0 ? h : 1};
    long long best_area = -1;
    struct output *it;
    wl_list_for_each(it, &cap.outputs, link) {
      int lw = 0;
      int lh = 0;
      int ox = 0;
      int oy = 0;
      output_logical_size(it, &lw, &lh);
      output_logical_origin(it, &ox, &oy);
      if (lw <= 0 || lh <= 0) {
        continue;
      }
      struct int_rect out_rect = {ox, oy, lw, lh};
      long long area = 0;
      if (rect_candidate_is_better(req, out_rect, best_area, &area)) {
        best_area = area;
        out = it;
      }
    }
  }
  if (!out) {
    out = wl_container_of(cap.outputs.next, out, link);
  }
  if (!out) {
    if (err) {
      *err = ScreengrabErrNoOutputs;
    }
    cleanup_capture(&cap);
    return NULL;
  }

  // Convert global logical capture rectangle to output-local logical coords.
  int out_ox = 0;
  int out_oy = 0;
  int out_lw = 0;
  int out_lh = 0;
  output_logical_origin(out, &out_ox, &out_oy);
  output_logical_size(out, &out_lw, &out_lh);

  long long local_x = (long long)x - (long long)out_ox;
  long long local_y = (long long)y - (long long)out_oy;
  int crop_lx = local_x > INT_MAX ? INT_MAX :
                local_x < INT_MIN ? INT_MIN : (int)local_x;
  int crop_ly = local_y > INT_MAX ? INT_MAX :
                local_y < INT_MIN ? INT_MIN : (int)local_y;
  int crop_lw = w;
  int crop_lh = h;
  x = crop_lx;
  y = crop_ly;
  w = crop_lw;
  h = crop_lh;

  cap.frame =
      zwlr_screencopy_manager_v1_capture_output(cap.manager, 0, out->wl_output);
  if (!cap.frame) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  zwlr_screencopy_frame_v1_add_listener(cap.frame, &frame_listener, &cap);

  const long timeout_ms = 2000; // 2s safety timeout
  long start_ms = monotonic_millis();
  long deadline_ms = start_ms < 0 ? 0 : start_ms + timeout_ms;
  while (!cap.done && !cap.failed) {
    int dres = dispatch_until(cap.display, deadline_ms);
    if (dres < 0) {
      cap.failed = 1;
      cap.err_code = ScreengrabErrFailed;
      break;
    }
    if (dres == 0) {
      cap.failed = 1;
      cap.err_code = ScreengrabErrFailed;
      break;
    }
  }

  if (!cap.failed && cap.using_dmabuf) {
    uint32_t stride = 0;
    cap.data =
        gbm_bo_map(cap.bo, 0, 0, (uint32_t)cap.width, (uint32_t)cap.height,
                   GBM_BO_TRANSFER_READ, &stride, &cap.map_data);
    if (!cap.data) {
      cap.failed = 1;
      cap.err_code = ScreengrabErrDmabufMap;
    } else {
      cap.stride = (int)stride;
    }
  }

  if (cap.failed || !cap.data) {
    int32_t code = cap.err_code ? cap.err_code : ScreengrabErrFailed;
    cleanup_capture(&cap);
    if (err) {
      *err = code;
    }
    return NULL;
  }

  if (x < 0)
    x = 0;
  if (y < 0)
    y = 0;
  map_logical_rect_to_buffer(out, cap.width, cap.height, out_lw, out_lh, &x, &y, &w, &h);
  if (x > cap.width || y > cap.height) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  if (w <= 0 || x + w > cap.width) {
    w = cap.width - x;
  }
  if (h <= 0 || y + h > cap.height) {
    h = cap.height - y;
  }

  size_t stride = (size_t)w * 4;
  uint8_t *rgba = malloc(stride * (size_t)h);
  if (!rgba) {
    if (err) {
      *err = ScreengrabErrFailed;
    }
    cleanup_capture(&cap);
    return NULL;
  }
  uint8_t *src = cap.data;
  for (int row = 0; row < h; ++row) {
    for (int col = 0; col < w; ++col) {
      size_t sidx = (size_t)(row + y) * cap.stride + (size_t)(col + x) * 4;
      size_t didx = (size_t)row * stride + (size_t)col * 4;
      if (!screencopy_pixel_to_bitmap_bgra(rgba + didx, src + sidx,
                                           cap.format, cap.using_dmabuf)) {
        free(rgba);
        cleanup_capture(&cap);
        if (err) {
          *err = ScreengrabErrPixelFormat;
        }
        return NULL;
      }
    }
  }

  cleanup_capture(&cap);

  MMBitmapRef bitmap = createMMBitmap_c(rgba, w, h, stride, 32, 4);
  if (!bitmap) {
    free(rgba);
    if (err) {
      *err = ScreengrabErrFailed;
    }
    return NULL;
  }
  return bitmap;
}

#endif // IS_LINUX
