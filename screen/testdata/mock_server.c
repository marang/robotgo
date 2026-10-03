//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

#define _GNU_SOURCE
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include <wayland-server.h>
#include "../../wlr-screencopy-unstable-v1-client-protocol.h"
#include "../../linux-dmabuf-unstable-v1-server-protocol.h"
#include "../../mouse/wlr-virtual-pointer-unstable-v1-client-protocol.h"

#define ZWLR_SCREENCOPY_FRAME_V1_BUFFER 0
#define ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF 5
#define ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE 6
#define ZWLR_SCREENCOPY_FRAME_V1_READY 2
#define ZWLR_SCREENCOPY_FRAME_V1_FAILED 3
#define ZWLR_SCREENCOPY_FRAME_V1_FLAGS 1
#define WL_BUFFER_RELEASE 0
#define MOCK_DRM_FORMAT_ARGB8888 0x34325241u

#define MOCK_MODE_NORMAL 0
#define MOCK_MODE_STALL 1
#define MOCK_MODE_FAIL_AFTER_DMABUF 2
#define MOCK_MODE_REGISTRY_STALL 3
#define MOCK_MODE_PIXELS 4
#define MOCK_MODE_PIXELS_Y_INVERT 5
#define MOCK_MODE_PIXELS_Y_INVERT_SCALE 6
#define MOCK_MODE_POINTER 7
#define MOCK_MODE_BUFFER_METADATA 8
#define MOCK_MODE_DMABUF_METADATA 9
#define MOCK_MODE_SHM_THEN_DMABUF 10
#define MOCK_MODE_DMABUF_THEN_SHM 11
#define MOCK_MODE_DUPLICATE_BUFFER 12
#define MOCK_MODE_FAIL_AFTER_SHM_COPY 13
#define MOCK_MODE_SHM_VERSION_ONE 14
#define MOCK_MODE_SHM_VERSION_TWO 15


static struct wl_display *mock_display;
static dev_t mock_dev;
static uint64_t mock_modifier;
static int use_shm;
static uint32_t mock_mode;
static int mock_legacy_shm(void) {
    return mock_mode == MOCK_MODE_SHM_VERSION_ONE || mock_mode == MOCK_MODE_SHM_VERSION_TWO;
}
static uint32_t mock_buffer_width, mock_buffer_height, mock_buffer_stride;
static _Atomic uint32_t mock_pool_requests, mock_copy_requests;
static _Atomic int mock_stop_requested;
static _Atomic uint32_t mock_pointer_frames;
static _Atomic uint32_t mock_pointer_source_count;
static _Atomic uint32_t mock_pointer_source;
static _Atomic int32_t mock_pointer_axis[2];
static _Atomic int32_t mock_pointer_discrete_value[2];
static _Atomic int32_t mock_pointer_discrete[2];

static int mock_has_pixels(void) {
    return mock_mode >= MOCK_MODE_PIXELS && mock_mode <= MOCK_MODE_PIXELS_Y_INVERT_SCALE;
}

struct zwlr_screencopy_frame_v1_interface {
    void (*copy)(struct wl_client *, struct wl_resource *, struct wl_resource *);
    void (*destroy)(struct wl_client *, struct wl_resource *);
    void (*copy_with_damage)(struct wl_client *, struct wl_resource *, struct wl_resource *);
};

struct zwlr_screencopy_manager_v1_interface {
    void (*capture_output)(struct wl_client *, struct wl_resource *, uint32_t, struct wl_resource *);
    void (*capture_output_region)(struct wl_client *, struct wl_resource *, uint32_t, struct wl_resource *, int32_t, int32_t, int32_t, int32_t);
    void (*destroy)(struct wl_client *, struct wl_resource *);
};

static void frame_copy(struct wl_client *client, struct wl_resource *resource, struct wl_resource *buffer) {
    atomic_fetch_add(&mock_copy_requests, 1);
    if (mock_mode == MOCK_MODE_FAIL_AFTER_SHM_COPY) {
        wl_resource_post_event(resource, ZWLR_SCREENCOPY_FRAME_V1_FAILED);
        wl_display_flush_clients(mock_display);
        return;
    }
    if (mock_has_pixels()) {
        struct wl_shm_buffer *shm_buffer = wl_shm_buffer_get(buffer);
        if (!shm_buffer) {
            wl_resource_post_event(resource, ZWLR_SCREENCOPY_FRAME_V1_FAILED);
            return;
        }
        wl_shm_buffer_begin_access(shm_buffer);
        uint8_t *pixels = wl_shm_buffer_get_data(shm_buffer);
        int stride = wl_shm_buffer_get_stride(shm_buffer);
        memset(pixels, 0xee, (size_t)stride * 4);
        for (int y = 0; y < 4; y++) {
            int row = mock_mode == MOCK_MODE_PIXELS ? y : 3 - y;
            for (int x = 0; x < 4; x++) {
                uint8_t *pixel = pixels + row * stride + x * 4;
                pixel[0] = 73;
                pixel[1] = 5 + x * 29;
                pixel[2] = 17 + y * 31;
                pixel[3] = 0xff;
            }
        }
        wl_shm_buffer_end_access(shm_buffer);
        wl_resource_post_event(resource, ZWLR_SCREENCOPY_FRAME_V1_FLAGS,
            mock_mode == MOCK_MODE_PIXELS ? 0 : ZWLR_SCREENCOPY_FRAME_V1_FLAGS_Y_INVERT);
    }
    wl_resource_post_event(resource, ZWLR_SCREENCOPY_FRAME_V1_READY, 0, 0, 0);
    wl_resource_post_event(buffer, WL_BUFFER_RELEASE);
    wl_display_flush_clients(mock_display);
}

static void frame_destroy(struct wl_client *client, struct wl_resource *resource) {
    wl_resource_destroy(resource);
}

static void frame_resource_destroy(struct wl_resource *resource) {
    if (mock_display) {
        wl_display_terminate(mock_display);
    }
}

static const struct zwlr_screencopy_frame_v1_interface frame_impl = {
    .copy = frame_copy,
    .copy_with_damage = NULL,
    .destroy = frame_destroy,
};

static void handle_capture_output(struct wl_client *client, struct wl_resource *resource, uint32_t id, struct wl_resource *output) {
    uint32_t version = mock_legacy_shm() ? wl_resource_get_version(resource) : 3;
    struct wl_resource *frame = wl_resource_create(client, &zwlr_screencopy_frame_v1_interface, version, id);
    wl_resource_set_implementation(frame, &frame_impl, NULL, frame_resource_destroy);
    if (mock_mode == MOCK_MODE_STALL) {
        return;
    }
    if (mock_mode == MOCK_MODE_FAIL_AFTER_DMABUF) {
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF, MOCK_DRM_FORMAT_ARGB8888, 64, 64);
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_FAILED);
        wl_display_flush_clients(mock_display);
        return;
    }
    if (mock_mode == MOCK_MODE_DMABUF_METADATA) {
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF,
            MOCK_DRM_FORMAT_ARGB8888, mock_buffer_width, mock_buffer_height);
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
        return;
    }
    if (mock_mode == MOCK_MODE_SHM_THEN_DMABUF || mock_mode == MOCK_MODE_DMABUF_THEN_SHM) {
        if (mock_mode == MOCK_MODE_DMABUF_THEN_SHM) {
            wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF,
                MOCK_DRM_FORMAT_ARGB8888, 0x80000000u, 0);
        }
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888, 64, 64, 256);
        if (mock_mode == MOCK_MODE_SHM_THEN_DMABUF) {
            wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF,
                MOCK_DRM_FORMAT_ARGB8888, 0x80000000u, 0);
        }
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
        return;
    }
    if (mock_mode == MOCK_MODE_DUPLICATE_BUFFER) {
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888, 64, 64, 256);
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888, 1, 1, 4);
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
        return;
    }
    if (use_shm) {
        if (mock_mode == MOCK_MODE_BUFFER_METADATA) {
            wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888,
                mock_buffer_width, mock_buffer_height, mock_buffer_stride);
        } else if (mock_has_pixels()) {
            wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888, 4, 4, 24);
        } else {
            wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER, WL_SHM_FORMAT_ARGB8888, 64, 64, 256);
        }
    } else {
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF, MOCK_DRM_FORMAT_ARGB8888, 64, 64);
    }
    if (!mock_legacy_shm()) {
        wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
    }
}

static const struct zwlr_screencopy_manager_v1_interface screencopy_impl = {
    .capture_output = handle_capture_output,
    .capture_output_region = NULL,
    .destroy = NULL,
};

static void bind_screencopy_manager(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &zwlr_screencopy_manager_v1_interface, mock_legacy_shm() ? version : 3, id);
    wl_resource_set_implementation(res, &screencopy_impl, NULL, NULL);
}

static void params_destroy(struct wl_client *client, struct wl_resource *resource) {
    wl_resource_destroy(resource);
}

static void params_add(struct wl_client *client, struct wl_resource *resource, int32_t fd, uint32_t plane_idx, uint32_t offset, uint32_t stride, uint32_t modifier_hi, uint32_t modifier_lo) {
    close(fd);
}

static void params_create_immed(struct wl_client *client, struct wl_resource *resource, uint32_t id, int32_t width, int32_t height, uint32_t format, uint32_t flags) {
    struct wl_resource *buf = wl_resource_create(client, &wl_buffer_interface, 1, id);
    wl_resource_set_implementation(buf, NULL, NULL, NULL);
}

static const struct zwp_linux_buffer_params_v1_interface params_impl = {
    .destroy = params_destroy,
    .add = params_add,
    .create = NULL,
    .create_immed = params_create_immed,
};

static void dmabuf_create_params(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
    uint32_t version = wl_resource_get_version(resource);
    struct wl_resource *params = wl_resource_create(client, &zwp_linux_buffer_params_v1_interface, version, id);
    wl_resource_set_implementation(params, &params_impl, NULL, NULL);
}

static void dmabuf_get_default_feedback(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
    struct wl_resource *fb = wl_resource_create(client, &zwp_linux_dmabuf_feedback_v1_interface, 4, id);
    static const struct zwp_linux_dmabuf_feedback_v1_interface feedback_impl = {
        .destroy = params_destroy,
    };
    wl_resource_set_implementation(fb, &feedback_impl, NULL, NULL);
    struct {
        uint32_t format;
        uint32_t pad;
        uint64_t modifier;
    } entry = {MOCK_DRM_FORMAT_ARGB8888, 0, mock_modifier};
    int fd = memfd_create("tbl", MFD_CLOEXEC);
    if (fd < 0 || write(fd, &entry, sizeof(entry)) != (ssize_t)sizeof(entry)) {
        if (fd >= 0) {
            close(fd);
        }
        wl_resource_post_no_memory(fb);
        return;
    }
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_FORMAT_TABLE, fd, sizeof(entry));
    close(fd);
    struct wl_array arr;
    wl_array_init(&arr);
    dev_t *devp = wl_array_add(&arr, sizeof(dev_t));
    *devp = mock_dev;
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_MAIN_DEVICE, &arr);
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_TRANCHE_TARGET_DEVICE, &arr);
    wl_array_release(&arr);
    struct wl_array indices;
    wl_array_init(&indices);
    uint16_t *idx = wl_array_add(&indices, sizeof(uint16_t));
    *idx = 0;
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_TRANCHE_FORMATS, &indices);
    wl_array_release(&indices);
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_TRANCHE_DONE);
    wl_resource_post_event(fb, ZWP_LINUX_DMABUF_FEEDBACK_V1_DONE);
}

static const struct zwp_linux_dmabuf_v1_interface dmabuf_impl = {
    .destroy = params_destroy,
    .create_params = dmabuf_create_params,
    .get_default_feedback = dmabuf_get_default_feedback,
};

static void bind_dmabuf(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &zwp_linux_dmabuf_v1_interface, 4, id);
    wl_resource_set_implementation(res, &dmabuf_impl, NULL, NULL);
}

static void bind_output(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &wl_output_interface, 2, id);
    wl_resource_set_implementation(res, NULL, NULL, NULL);
    if (mock_has_pixels() || mock_mode == MOCK_MODE_POINTER) {
        int size = mock_has_pixels() ? 4 : 64;
        wl_output_send_geometry(res, 0, 0, 0, 0, WL_OUTPUT_SUBPIXEL_UNKNOWN,
            "robotgo", "synthetic", WL_OUTPUT_TRANSFORM_NORMAL);
        wl_output_send_mode(res, WL_OUTPUT_MODE_CURRENT, size, size, 60000);
        wl_output_send_scale(res, mock_mode == MOCK_MODE_PIXELS_Y_INVERT_SCALE ? 2 : 1);
        wl_output_send_done(res);
    }
}

struct zwlr_virtual_pointer_v1_interface {
    void (*motion)(struct wl_client *, struct wl_resource *, uint32_t, wl_fixed_t, wl_fixed_t);
    void (*motion_absolute)(struct wl_client *, struct wl_resource *, uint32_t, uint32_t, uint32_t, uint32_t, uint32_t);
    void (*button)(struct wl_client *, struct wl_resource *, uint32_t, uint32_t, uint32_t);
    void (*axis)(struct wl_client *, struct wl_resource *, uint32_t, uint32_t, wl_fixed_t);
    void (*frame)(struct wl_client *, struct wl_resource *);
    void (*axis_source)(struct wl_client *, struct wl_resource *, uint32_t);
    void (*axis_stop)(struct wl_client *, struct wl_resource *, uint32_t, uint32_t);
    void (*axis_discrete)(struct wl_client *, struct wl_resource *, uint32_t, uint32_t, wl_fixed_t, int32_t);
    void (*destroy)(struct wl_client *, struct wl_resource *);
};

struct zwlr_virtual_pointer_manager_v1_interface {
    void (*create_virtual_pointer)(struct wl_client *, struct wl_resource *, struct wl_resource *, uint32_t);
    void (*destroy)(struct wl_client *, struct wl_resource *);
    void (*create_virtual_pointer_with_output)(struct wl_client *, struct wl_resource *, struct wl_resource *, struct wl_resource *, uint32_t);
};

static void mock_pointer_axis_request(struct wl_client *client, struct wl_resource *resource,
                                     uint32_t time, uint32_t axis, wl_fixed_t value) {
    if (axis < 2) atomic_store(&mock_pointer_axis[axis], value);
}

static void mock_pointer_discrete_request(struct wl_client *client, struct wl_resource *resource,
                                         uint32_t time, uint32_t axis, wl_fixed_t value, int32_t discrete) {
    if (axis < 2) {
        atomic_store(&mock_pointer_discrete_value[axis], value);
        atomic_store(&mock_pointer_discrete[axis], discrete);
    }
}

static void mock_pointer_source_request(struct wl_client *client, struct wl_resource *resource, uint32_t source) {
    atomic_store(&mock_pointer_source, source);
    atomic_fetch_add(&mock_pointer_source_count, 1);
}

static void mock_pointer_frame_request(struct wl_client *client, struct wl_resource *resource) {
    atomic_fetch_add(&mock_pointer_frames, 1);
}

static const struct zwlr_virtual_pointer_v1_interface mock_pointer_impl = {
    .axis = mock_pointer_axis_request,
    .frame = mock_pointer_frame_request,
    .axis_source = mock_pointer_source_request,
    .axis_discrete = mock_pointer_discrete_request,
    .destroy = frame_destroy,
};

static void mock_create_pointer(struct wl_client *client, struct wl_resource *resource,
                                struct wl_resource *seat, uint32_t id) {
    struct wl_resource *pointer = wl_resource_create(client, &zwlr_virtual_pointer_v1_interface, 1, id);
    wl_resource_set_implementation(pointer, &mock_pointer_impl, NULL, NULL);
}

static const struct zwlr_virtual_pointer_manager_v1_interface mock_pointer_manager_impl = {
    .create_virtual_pointer = mock_create_pointer,
    .destroy = frame_destroy,
};

static void bind_pointer_manager(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *resource = wl_resource_create(client, &zwlr_virtual_pointer_manager_v1_interface, 1, id);
    wl_resource_set_implementation(resource, &mock_pointer_manager_impl, NULL, NULL);
}

static void bind_pointer_seat(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *resource = wl_resource_create(client, &wl_seat_interface, 1, id);
    wl_resource_set_implementation(resource, NULL, NULL, NULL);
    wl_seat_send_capabilities(resource, WL_SEAT_CAPABILITY_POINTER);
}

uint32_t mock_pointer_frame_count(void) { return atomic_load(&mock_pointer_frames); }
uint32_t mock_pointer_axis_source_count(void) { return atomic_load(&mock_pointer_source_count); }
uint32_t mock_pointer_axis_source(void) { return atomic_load(&mock_pointer_source); }
int32_t mock_pointer_axis_value(uint32_t axis) { return atomic_load(&mock_pointer_axis[axis]); }
int32_t mock_pointer_axis_discrete_value(uint32_t axis) { return atomic_load(&mock_pointer_discrete_value[axis]); }
int32_t mock_pointer_axis_discrete_count(uint32_t axis) { return atomic_load(&mock_pointer_discrete[axis]); }

static void shm_pool_destroy(struct wl_client *client, struct wl_resource *resource) {
    wl_resource_destroy(resource);
}

static void shm_pool_create_buffer(struct wl_client *client, struct wl_resource *resource, uint32_t id, int32_t offset, int32_t width, int32_t height, int32_t stride, uint32_t format) {
    struct wl_resource *buf = wl_resource_create(client, &wl_buffer_interface, 1, id);
    wl_resource_set_implementation(buf, NULL, NULL, NULL);
}

static void shm_pool_resize(struct wl_client *client, struct wl_resource *resource, int32_t size) { }

static const struct wl_shm_pool_interface shm_pool_impl = {
    .destroy = shm_pool_destroy,
    .create_buffer = shm_pool_create_buffer,
    .resize = shm_pool_resize,
};

static void shm_create_pool(struct wl_client *client, struct wl_resource *resource, uint32_t id, int32_t fd, int32_t size) {
    atomic_fetch_add(&mock_pool_requests, 1);
    close(fd);
    struct wl_resource *pool = wl_resource_create(client, &wl_shm_pool_interface, 1, id);
    wl_resource_set_implementation(pool, &shm_pool_impl, NULL, NULL);
}

static const struct wl_shm_interface shm_impl = {
    .create_pool = shm_create_pool,
};

static void bind_shm(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &wl_shm_interface, 1, id);
    wl_resource_set_implementation(res, &shm_impl, NULL, NULL);
}

void run_mock_server_buffer_metadata(const char *socket, uint32_t maj, uint32_t min, uint64_t modifier, uint32_t mode,
    uint32_t width, uint32_t height, uint32_t stride) {
    mock_dev = makedev(maj, min);
    mock_modifier = modifier;
    mock_mode = mode;
    mock_buffer_width = width;
    mock_buffer_height = height;
    mock_buffer_stride = stride;
    atomic_store(&mock_pool_requests, 0);
    atomic_store(&mock_copy_requests, 0);
    mock_stop_requested = 0;
    atomic_store(&mock_pointer_frames, 0);
    atomic_store(&mock_pointer_source_count, 0);
    atomic_store(&mock_pointer_source, UINT32_MAX);
    for (int axis = 0; axis < 2; axis++) {
        atomic_store(&mock_pointer_axis[axis], 0);
        atomic_store(&mock_pointer_discrete_value[axis], 0);
        atomic_store(&mock_pointer_discrete[axis], 0);
    }
    use_shm = (maj == 0 && min == 0 && modifier == 0);
    mock_display = wl_display_create();
    wl_display_add_socket(mock_display, socket);
    wl_global_create(mock_display, &wl_output_interface, 2, NULL, bind_output);
    if (!use_shm || mock_mode == MOCK_MODE_DMABUF_METADATA ||
        mock_mode == MOCK_MODE_SHM_THEN_DMABUF || mock_mode == MOCK_MODE_DMABUF_THEN_SHM) {
        wl_global_create(mock_display, &zwp_linux_dmabuf_v1_interface, 4, NULL, bind_dmabuf);
    }
    if (use_shm) {
        if (mock_has_pixels()) {
            wl_display_init_shm(mock_display);
        } else {
            wl_global_create(mock_display, &wl_shm_interface, 1, NULL, bind_shm);
        }
    }
    if (mock_mode == MOCK_MODE_POINTER) {
        wl_global_create(mock_display, &wl_seat_interface, 1, NULL, bind_pointer_seat);
        wl_global_create(mock_display, &zwlr_virtual_pointer_manager_v1_interface, 1, NULL, bind_pointer_manager);
    }
    uint32_t screencopy_version = mock_mode == MOCK_MODE_SHM_VERSION_ONE ? 1 :
        mock_mode == MOCK_MODE_SHM_VERSION_TWO ? 2 : 3;
    wl_global_create(mock_display, &zwlr_screencopy_manager_v1_interface, screencopy_version, NULL, bind_screencopy_manager);
    if (mock_mode == MOCK_MODE_REGISTRY_STALL) {
        while (!mock_stop_requested) {
            usleep(1000);
        }
    } else if (mock_mode == MOCK_MODE_POINTER) {
        while (!mock_stop_requested) {
            wl_event_loop_dispatch(wl_display_get_event_loop(mock_display), 10);
            wl_display_flush_clients(mock_display);
        }
    } else {
        wl_display_run(mock_display);
    }
    wl_display_destroy_clients(mock_display);
    wl_display_destroy(mock_display);
    mock_display = NULL;
}

void run_mock_server_mode(const char *socket, uint32_t maj, uint32_t min, uint64_t modifier, uint32_t mode) {
    run_mock_server_buffer_metadata(socket, maj, min, modifier, mode, 64, 64, 256);
}

uint32_t mock_pool_request_count(void) { return atomic_load(&mock_pool_requests); }
uint32_t mock_copy_request_count(void) { return atomic_load(&mock_copy_requests); }

void run_mock_server(const char *socket, uint32_t maj, uint32_t min, uint64_t modifier) {
    run_mock_server_mode(socket, maj, min, modifier, MOCK_MODE_NORMAL);
}

void stop_mock_server(void) {
    mock_stop_requested = 1;
    if (mock_mode == MOCK_MODE_POINTER) {
        return;
    }
    if (mock_display) {
        wl_display_terminate(mock_display);
    }
}
