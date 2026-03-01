// go:build ignore
//  +build ignore

#define _GNU_SOURCE
// This file is included from screengrab_c.h, which provides necessary
// declarations. It implements Wayland screen capture via the wlr-screencopy
// protocol without relying on external utilities.

#include "../linux-dmabuf-unstable-v1-client-protocol.h"
#include "../wlr-screencopy-unstable-v1-client-protocol.h"
#include <errno.h>
#include <fcntl.h>
#include <gbm.h>
#include <xf86drm.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include <time.h>
#include <wayland-client.h>

#ifndef MFD_CLOEXEC
#define MFD_CLOEXEC 0x0001
#endif

#define WAYLAND_BACKEND_DMABUF 0
#define WAYLAND_BACKEND_WL_SHM 1

#if defined(IS_LINUX)

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
  } else if (strcmp(interface, wl_output_interface.name) == 0) {
    struct output *out = malloc(sizeof(*out));
    if (!out) {
      return;
    }
    out->wl_output = wl_registry_bind(registry, name, &wl_output_interface, 2);
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

  char shm_name[64];
  snprintf(shm_name, sizeof(shm_name), "/robotgo-wl-%d", getpid());
  int fd = shm_open(shm_name, O_CREAT | O_EXCL | O_RDWR, 0600);
  if (fd < 0) {
    cap->failed = 1;
    cap->err_code = ScreengrabErrFailed;
    return;
  }
  shm_unlink(shm_name);
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
  cap->buffer = wl_shm_pool_create_buffer(pool, 0, (int)width, (int)height,
                                          (int)stride, format);
  wl_shm_pool_destroy(pool);
  close(fd);
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
}

static void frame_buffer_done(void *data,
                              struct zwlr_screencopy_frame_v1 *frame) {
  struct capture *cap = data;
  if (!cap->using_dmabuf) {
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
    wl_output_destroy(out->wl_output);
    free(out);
  }
  if (cap->shm) {
    wl_shm_destroy(cap->shm);
  }
  if (cap->dmabuf) {
    zwp_linux_dmabuf_v1_destroy(cap->dmabuf);
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

// capture_screen_wayland_impl performs the actual capture logic. The backend
// parameter selects whether dmabuf (zero-copy) or wl_shm (shared memory)
// should be used.
MMBitmapRef capture_screen_wayland_impl(int32_t x, int32_t y, int32_t w,
                                        int32_t h, int32_t display_id,
                                        int8_t isPid, int32_t backend,
                                        int32_t *err) {
  (void)display_id;
  (void)isPid;
  if (err) {
    *err = ScreengrabOK;
  }
  struct capture cap = {0};
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
  struct output *out = wl_container_of(cap.outputs.next, out, link);
  cap.frame =
      zwlr_screencopy_manager_v1_capture_output(cap.manager, 0, out->wl_output);
  zwlr_screencopy_frame_v1_add_listener(cap.frame, &frame_listener, &cap);

  struct timespec ts_start, ts_now;
#ifdef CLOCK_MONOTONIC
  clock_gettime(CLOCK_MONOTONIC, &ts_start);
#else
  clock_gettime(CLOCK_REALTIME, &ts_start);
#endif
  const long timeout_ms = 2000; // 2s safety timeout
  while (!cap.done && !cap.failed) {
    int dres = wl_display_dispatch(cap.display);
    if (dres < 0) {
      cap.failed = 1;
      cap.err_code = ScreengrabErrFailed;
      break;
    }
#ifdef CLOCK_MONOTONIC
    clock_gettime(CLOCK_MONOTONIC, &ts_now);
#else
    clock_gettime(CLOCK_REALTIME, &ts_now);
#endif
    long elapsed_ms = (ts_now.tv_sec - ts_start.tv_sec) * 1000L +
                      (ts_now.tv_nsec - ts_start.tv_nsec) / 1000000L;
    if (elapsed_ms > timeout_ms) {
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
    // Try portal fallback for any failure case
    int32_t perr = ScreengrabOK;
    MMBitmapRef p = capture_screen_portal(x, y, w, h, display_id, isPid, &perr);
    if (p) {
      if (err) {
        *err = perr;
      }
      return p;
    }
    if (err) {
      *err = code;
    }
    return NULL;
  }

  if (x < 0)
    x = 0;
  if (y < 0)
    y = 0;
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
      rgba[didx + 0] = src[sidx + 2];
      rgba[didx + 1] = src[sidx + 1];
      rgba[didx + 2] = src[sidx + 0];
      rgba[didx + 3] = src[sidx + 3];
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
