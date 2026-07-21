#pragma once

#include <errno.h>
#include <stddef.h>

enum RobotGoWaylandFlushResult {
    ROBOTGO_WAYLAND_FLUSHED = 0,
    ROBOTGO_WAYLAND_FLUSH_QUEUED = 1,
    ROBOTGO_WAYLAND_FLUSH_FAILED = -1
};

typedef int (*robotgo_wayland_flush_fn)(void *context);
typedef int (*robotgo_wayland_wait_writable_fn)(void *context, int timeout_ms);

/* Retry a nonblocking Wayland flush without treating EAGAIN as disconnect.
 * Exhausting the bounded wait still leaves the request safely queued inside
 * libwayland; only permanent flush/poll errors are reported as failures. */
static inline int robotgo_wayland_flush_with_retry(
    void *context,
    robotgo_wayland_flush_fn flush,
    robotgo_wayland_wait_writable_fn wait_writable,
    int timeout_ms,
    int attempts) {
    if (flush == NULL || wait_writable == NULL || timeout_ms < 0 ||
        attempts < 0) {
        return ROBOTGO_WAYLAND_FLUSH_FAILED;
    }
    if (flush(context) >= 0) {
        return ROBOTGO_WAYLAND_FLUSHED;
    }
    if (errno != EAGAIN && errno != EWOULDBLOCK) {
        return ROBOTGO_WAYLAND_FLUSH_FAILED;
    }

    for (int attempt = 0; attempt < attempts; attempt++) {
        int ready = wait_writable(context, timeout_ms);
        if (ready < 0 && errno == EINTR) {
            continue;
        }
        if (ready < 0) {
            return ROBOTGO_WAYLAND_FLUSH_FAILED;
        }
        if (ready == 0) {
            continue;
        }
        if (flush(context) >= 0) {
            return ROBOTGO_WAYLAND_FLUSHED;
        }
        if (errno != EAGAIN && errno != EWOULDBLOCK) {
            return ROBOTGO_WAYLAND_FLUSH_FAILED;
        }
    }
    return ROBOTGO_WAYLAND_FLUSH_QUEUED;
}
