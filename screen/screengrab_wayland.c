#include <stdint.h>
#include "../base/bitmap_free_c.h"

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
                                       int32_t *err);

// capture_screen_wayland chooses a backend at runtime based on the provided
// enum. If WaylandBackendAuto is passed it will try DMABUF first and then
// fall back to wl_shm.
MMBitmapRef capture_screen_wayland(int32_t x, int32_t y, int32_t w, int32_t h,
                                   int32_t display_id, int8_t isPid,
                                   int32_t backend, int32_t *err) {
    if (backend == WaylandBackendWlShm) {
        return capture_screen_wayland_impl(x, y, w, h, display_id, isPid,
                                           WAYLAND_BACKEND_WL_SHM, err);
    }
    MMBitmapRef bmp = capture_screen_wayland_impl(x, y, w, h, display_id,
                                                  isPid, WAYLAND_BACKEND_DMABUF,
                                                  err);
    if (bmp || backend == WaylandBackendDmabuf) {
        return bmp;
    }
    return capture_screen_wayland_impl(x, y, w, h, display_id, isPid,
                                       WAYLAND_BACKEND_WL_SHM, err);
}
