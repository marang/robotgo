//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

#define _GNU_SOURCE
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include <wayland-server.h>
#include "../../wlr-screencopy-unstable-v1-client-protocol.h"
#include "../../linux-dmabuf-unstable-v1-server-protocol.h"

#define ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF 5
#define ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE 6
#define ZWLR_SCREENCOPY_FRAME_V1_READY 3
#define WL_BUFFER_RELEASE 0

static struct wl_display *mock_display;
static dev_t mock_dev;
static uint64_t mock_modifier;

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
    wl_resource_post_event(resource, ZWLR_SCREENCOPY_FRAME_V1_READY, 0, 0, 0);
    wl_resource_post_event(buffer, WL_BUFFER_RELEASE);
    wl_display_flush_clients(mock_display);
    wl_display_terminate(mock_display);
}

static const struct zwlr_screencopy_frame_v1_interface frame_impl = {
    .copy = frame_copy,
    .copy_with_damage = NULL,
    .destroy = NULL,
};

static void handle_capture_output(struct wl_client *client, struct wl_resource *resource, uint32_t id, struct wl_resource *output) {
    struct wl_resource *frame = wl_resource_create(client, &zwlr_screencopy_frame_v1_interface, 3, id);
    wl_resource_set_implementation(frame, &frame_impl, NULL, NULL);
    wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF, WL_SHM_FORMAT_ARGB8888, 64, 64);
    wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
}

static const struct zwlr_screencopy_manager_v1_interface screencopy_impl = {
    .capture_output = handle_capture_output,
    .capture_output_region = NULL,
    .destroy = NULL,
};

static void bind_screencopy_manager(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &zwlr_screencopy_manager_v1_interface, 3, id);
    wl_resource_set_implementation(res, &screencopy_impl, NULL, NULL);
}

struct zwp_linux_buffer_params_v1_interface {
    void (*destroy)(struct wl_client *, struct wl_resource *);
    void (*add)(struct wl_client *, struct wl_resource *, int32_t, uint32_t, uint32_t, uint32_t, uint32_t, uint32_t);
    void (*create)(struct wl_client *, struct wl_resource *, int32_t, int32_t, uint32_t, uint32_t);
    void (*create_immed)(struct wl_client *, struct wl_resource *, uint32_t, int32_t, int32_t, uint32_t, uint32_t);
};

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
    struct wl_resource *params = wl_resource_create(client, &zwp_linux_buffer_params_v1_interface, 1, id);
    wl_resource_set_implementation(params, &params_impl, NULL, NULL);
}

static void dmabuf_get_default_feedback(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
    struct wl_resource *fb = wl_resource_create(client, &zwp_linux_dmabuf_feedback_v1_interface, 4, id);
    wl_resource_set_implementation(fb, NULL, NULL, NULL);
    struct {
        uint32_t format;
        uint32_t pad;
        uint64_t modifier;
    } entry = {WL_SHM_FORMAT_ARGB8888, 0, mock_modifier};
    int fd = memfd_create("tbl", MFD_CLOEXEC);
    write(fd, &entry, sizeof(entry));
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
    .destroy = NULL,
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
}

void run_mock_server(const char *socket, uint32_t maj, uint32_t min, uint64_t modifier) {
    mock_dev = makedev(maj, min);
    mock_modifier = modifier;
    mock_display = wl_display_create();
    wl_display_add_socket(mock_display, socket);
    wl_global_create(mock_display, &wl_output_interface, 2, NULL, bind_output);
    wl_global_create(mock_display, &zwp_linux_dmabuf_v1_interface, 4, NULL, bind_dmabuf);
    wl_global_create(mock_display, &zwlr_screencopy_manager_v1_interface, 3, NULL, bind_screencopy_manager);
    wl_display_run(mock_display);
    wl_display_destroy(mock_display);
}

