//go:build ignore
// +build ignore

#include <stdint.h>
#include <time.h>

#define WAYLAND_BACKEND_DMABUF 0
#define WAYLAND_BACKEND_WL_SHM 1

// Wayland backend selector.
typedef enum {
    WaylandBackendAuto = -1,
    WaylandBackendDmabuf = WAYLAND_BACKEND_DMABUF,
    WaylandBackendWlShm = WAYLAND_BACKEND_WL_SHM,
} WaylandBackend;

// Forward declaration from implementation file.
MMBitmapRef capture_screen_wayland_impl(int32_t x, int32_t y, int32_t w,
                                       int32_t h, int32_t display_id,
                                       int8_t isPid, int32_t backend,
                                       long deadline_ms, int32_t *err);

static long robotgo_wayland_deadline(int32_t timeout_ms) {
    struct timespec ts;
    if (timeout_ms <= 0 || clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
        return 0;
    }
    return ts.tv_sec * 1000L + ts.tv_nsec / 1000000L + timeout_ms;
}

MMBitmapRef capture_screen_wayland_timeout(int32_t x, int32_t y, int32_t w,
                                           int32_t h, int32_t display_id,
                                           int8_t isPid, int32_t backend,
                                           int32_t timeout_ms, int32_t *err);

// capture_screen_wayland chooses a backend at runtime based on the provided
// enum. If WaylandBackendAuto is passed it will try DMABUF first and then
// fall back to wl_shm.
MMBitmapRef capture_screen_wayland(int32_t x, int32_t y, int32_t w, int32_t h,
                                   int32_t display_id, int8_t isPid,
                                   int32_t backend, int32_t *err) {
    return capture_screen_wayland_timeout(x, y, w, h, display_id, isPid,
                                          backend, 2000, err);
}

MMBitmapRef capture_screen_wayland_timeout(int32_t x, int32_t y, int32_t w,
                                           int32_t h, int32_t display_id,
                                           int8_t isPid, int32_t backend,
                                           int32_t timeout_ms, int32_t *err) {
    long deadline_ms = robotgo_wayland_deadline(timeout_ms);
    if (backend == WaylandBackendWlShm) {
        return capture_screen_wayland_impl(x, y, w, h, display_id, isPid,
                                           WAYLAND_BACKEND_WL_SHM, deadline_ms,
                                           err);
    }
    MMBitmapRef bmp = capture_screen_wayland_impl(x, y, w, h, display_id,
                                                  isPid, WAYLAND_BACKEND_DMABUF,
                                                  deadline_ms, err);
    if (bmp || backend == WaylandBackendDmabuf) {
        return bmp;
    }
    return capture_screen_wayland_impl(x, y, w, h, display_id, isPid,
                                       WAYLAND_BACKEND_WL_SHM, deadline_ms,
                                       err);
}
