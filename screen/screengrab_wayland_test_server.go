//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

/*
#cgo pkg-config: wayland-server
#include <stdlib.h>
#include <wayland-server.h>
#include "../wlr-screencopy-unstable-v1-client-protocol.h"

#define ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF 5
#define ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE 6

struct zwlr_screencopy_manager_v1_interface {
    void (*capture_output)(struct wl_client *, struct wl_resource *, uint32_t, struct wl_resource *);
    void (*capture_output_region)(struct wl_client *, struct wl_resource *, uint32_t, struct wl_resource *, int32_t, int32_t, int32_t, int32_t);
    void (*destroy)(struct wl_client *, struct wl_resource *);
};

static struct wl_display *mock_display;

static void handle_capture_output(struct wl_client *client, struct wl_resource *resource, uint32_t id, struct wl_resource *output) {
    struct wl_resource *frame = wl_resource_create(client, &zwlr_screencopy_frame_v1_interface, 3, id);
    wl_resource_set_implementation(frame, NULL, NULL, NULL);
    wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_LINUX_DMABUF, 0, 0, 0);
    wl_resource_post_event(frame, ZWLR_SCREENCOPY_FRAME_V1_BUFFER_DONE);
    wl_display_flush_clients(mock_display);
    wl_display_terminate(mock_display);
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

static void bind_shm(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &wl_shm_interface, 1, id);
    wl_resource_set_implementation(res, NULL, NULL, NULL);
}

static void bind_output(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
    struct wl_resource *res = wl_resource_create(client, &wl_output_interface, 2, id);
    wl_resource_set_implementation(res, NULL, NULL, NULL);
}

void run_mock_server(const char *socket) {
    mock_display = wl_display_create();
    wl_display_add_socket(mock_display, socket);
    wl_global_create(mock_display, &wl_shm_interface, 1, NULL, bind_shm);
    wl_global_create(mock_display, &wl_output_interface, 2, NULL, bind_output);
    wl_global_create(mock_display, &zwlr_screencopy_manager_v1_interface, 3, NULL, bind_screencopy_manager);
    wl_display_run(mock_display);
    wl_display_destroy(mock_display);
}
*/
import "C"
import "unsafe"

func startMockServer(socket string, done chan struct{}) {
	csock := C.CString(socket)
	go func() {
		C.run_mock_server(csock)
		C.free(unsafe.Pointer(csock))
		close(done)
	}()
}
