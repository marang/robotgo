//go:build ignore
// +build ignore

// This file is included from screengrab_c.h, which provides necessary declarations.
// It implements Wayland screen capture via the wlr-screencopy protocol without
// relying on external utilities.

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
#include <wayland-client.h>
#include "../wlr-screencopy-unstable-v1-client-protocol.h"

#if defined(IS_LINUX)

struct output {
  struct wl_list link;
  struct wl_output *wl_output;
};

struct capture {
  struct wl_display *display;
  struct wl_registry *registry;
  struct wl_shm *shm;
  struct zwlr_screencopy_manager_v1 *manager;
  struct zwlr_screencopy_frame_v1 *frame;
  struct wl_buffer *buffer;
  struct wl_list outputs;
  void *data;
  int width;
  int height;
  int stride;
  int done;
  int failed;
};

static void registry_global(void *data, struct wl_registry *registry, uint32_t name,
                            const char *interface, uint32_t version) {
  struct capture *cap = data;
  if (strcmp(interface, wl_shm_interface.name) == 0) {
    uint32_t ver = version < 1 ? version : 1;
    cap->shm = wl_registry_bind(registry, name, &wl_shm_interface, ver);
  } else if (strcmp(interface, zwlr_screencopy_manager_v1_interface.name) == 0) {
    uint32_t ver = version < 3 ? version : 3;
    cap->manager = wl_registry_bind(registry, name, &zwlr_screencopy_manager_v1_interface, ver);
  } else if (strcmp(interface, wl_output_interface.name) == 0) {
    struct output *out = malloc(sizeof(*out));
    if (!out) {
      return;
    }
    uint32_t ver = version < 2 ? version : 2;
    out->wl_output = wl_registry_bind(registry, name, &wl_output_interface, ver);
    wl_list_insert(&cap->outputs, &out->link);
  }
}

static void registry_remove(void *data, struct wl_registry *registry, uint32_t name) {
  (void)data;
  (void)registry;
  (void)name;
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_global,
    .global_remove = registry_remove,
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

  char shm_name[64];
  snprintf(shm_name, sizeof(shm_name), "/robotgo-wl-%d", getpid());
  int fd = shm_open(shm_name, O_CREAT | O_EXCL | O_RDWR, 0600);
  if (fd < 0) {
    cap->failed = 1;
    return;
  }
  shm_unlink(shm_name);
  size_t size = (size_t)stride * height;
  if (ftruncate(fd, (off_t)size) < 0) {
    close(fd);
    cap->failed = 1;
    return;
  }
  cap->data = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
  if (cap->data == MAP_FAILED) {
    close(fd);
    cap->failed = 1;
    return;
  }
  struct wl_shm_pool *pool = wl_shm_create_pool(cap->shm, fd, (int)size);
  cap->buffer = wl_shm_pool_create_buffer(pool, 0, (int)width, (int)height,
                                          (int)stride, WL_SHM_FORMAT_ARGB8888);
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

static void frame_ready(void *data, struct zwlr_screencopy_frame_v1 *frame,
                        uint32_t tv_sec_hi, uint32_t tv_sec_lo, uint32_t tv_nsec) {
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
}

static const struct zwlr_screencopy_frame_v1_listener frame_listener = {
    .buffer = frame_buffer,
    .flags = frame_flags,
    .ready = frame_ready,
    .failed = frame_failed,
};

static void cleanup_capture(struct capture *cap) {
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
  if (cap->manager) {
    zwlr_screencopy_manager_v1_destroy(cap->manager);
  }
  if (cap->registry) {
    wl_registry_destroy(cap->registry);
  }
  if (cap->display) {
    wl_display_disconnect(cap->display);
  }
}

MMBitmapRef capture_screen_wayland(int32_t x, int32_t y, int32_t w, int32_t h,
                                   int32_t display_id, int8_t isPid) {
  (void)display_id;
  (void)isPid;
  struct capture cap = {0};
  wl_list_init(&cap.outputs);

  cap.display = wl_display_connect(NULL);
  if (!cap.display) {
    return NULL;
  }
  cap.registry = wl_display_get_registry(cap.display);
  if (!cap.registry) {
    cleanup_capture(&cap);
    return NULL;
  }
  wl_registry_add_listener(cap.registry, &registry_listener, &cap);
  wl_display_roundtrip(cap.display);

  if (!cap.manager || !cap.shm || wl_list_empty(&cap.outputs)) {
    cleanup_capture(&cap);
    return NULL;
  }
  struct output *out = wl_container_of(cap.outputs.next, out, link);
  cap.frame = zwlr_screencopy_manager_v1_capture_output(cap.manager, 0,
                                                        out->wl_output);
  zwlr_screencopy_frame_v1_add_listener(cap.frame, &frame_listener, &cap);

  while (!cap.done && !cap.failed) {
    wl_display_dispatch(cap.display);
  }

  if (cap.failed || !cap.data) {
    cleanup_capture(&cap);
    return NULL;
  }

  if (x < 0) x = 0;
  if (y < 0) y = 0;
  if (x > cap.width || y > cap.height) {
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

  munmap(cap.data, (size_t)cap.stride * (size_t)cap.height);
  cleanup_capture(&cap);

  MMBitmapRef bitmap = createMMBitmap_c(rgba, w, h, stride, 32, 4);
  if (!bitmap) {
    free(rgba);
  }
  return bitmap;
}

#endif  // IS_LINUX
