#pragma once

#include <stddef.h>
#include <stdint.h>

enum { ROBOTGO_WAYLAND_ABSOLUTE_EXTENT = 65535 };

/* Map RobotGo's signed global logical coordinates into the unsigned absolute
 * frame used by zwlr_virtual_pointer_v1. The aggregate output origin may be
 * negative when an output is positioned left of or above the primary output. */
static inline int robotgo_wayland_map_absolute(
    int32_t global_x, int32_t global_y,
    int32_t origin_x, int32_t origin_y,
    int32_t width, int32_t height,
    uint32_t extent_x, uint32_t extent_y,
    uint32_t *mapped_x, uint32_t *mapped_y) {
    if (width <= 0 || height <= 0 || extent_x == 0 || extent_y == 0 ||
        mapped_x == NULL || mapped_y == NULL) {
        return -1;
    }

    int64_t local_x = (int64_t)global_x - (int64_t)origin_x;
    int64_t local_y = (int64_t)global_y - (int64_t)origin_y;
    if (local_x < 0 || local_y < 0 || local_x >= width || local_y >= height) {
        return -1;
    }

    *mapped_x = (uint32_t)((uint64_t)local_x * extent_x / (uint32_t)width);
    *mapped_y = (uint32_t)((uint64_t)local_y * extent_y / (uint32_t)height);
    return 0;
}
