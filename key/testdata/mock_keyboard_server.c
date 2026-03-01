//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <wayland-server-core.h>
#include <wayland-server-protocol.h>

#include "../../virtual-keyboard-unstable-v1-client-protocol.h"

struct zwp_virtual_keyboard_manager_v1_interface {
	void (*create_virtual_keyboard)(struct wl_client *client, struct wl_resource *resource, uint32_t id, struct wl_resource *seat);
	void (*destroy)(struct wl_client *client, struct wl_resource *resource);
};

struct zwp_virtual_keyboard_v1_interface {
	void (*keymap)(struct wl_client *client, struct wl_resource *resource, uint32_t format, int32_t fd, uint32_t size);
	void (*key)(struct wl_client *client, struct wl_resource *resource, uint32_t time, uint32_t key, uint32_t state);
	void (*modifiers)(struct wl_client *client, struct wl_resource *resource, uint32_t depressed, uint32_t latched, uint32_t locked, uint32_t group);
	void (*destroy)(struct wl_client *client, struct wl_resource *resource);
};

struct wl_seat_interface_local {
	void (*get_pointer)(struct wl_client *client, struct wl_resource *resource, uint32_t id);
	void (*get_keyboard)(struct wl_client *client, struct wl_resource *resource, uint32_t id);
	void (*get_touch)(struct wl_client *client, struct wl_resource *resource, uint32_t id);
	void (*release)(struct wl_client *client, struct wl_resource *resource);
};

static volatile uint32_t mk_key_events = 0;
static volatile uint32_t mk_mod_events = 0;
static volatile uint32_t mk_last_mods = 0;
static volatile uint32_t mk_last_key = 0;
static volatile uint32_t mk_last_state = 0;
static volatile uint32_t mk_expected_keys = 0;
static volatile int mk_running = 0;

static struct wl_display *mk_display = NULL;
static struct wl_event_loop *mk_loop = NULL;
static struct wl_global *mk_seat_global = NULL;
static struct wl_global *mk_vk_global = NULL;

uint32_t mock_keyboard_key_events(void) { return mk_key_events; }
uint32_t mock_keyboard_mod_events(void) { return mk_mod_events; }
uint32_t mock_keyboard_last_mods(void) { return mk_last_mods; }
uint32_t mock_keyboard_last_key(void) { return mk_last_key; }
uint32_t mock_keyboard_last_state(void) { return mk_last_state; }

void stop_mock_keyboard_server(void) { mk_running = 0; }

static void seat_get_pointer(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
	(void)client;
	(void)resource;
	(void)id;
}

static void seat_get_keyboard(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
	struct wl_resource *kbd = wl_resource_create(client, &wl_keyboard_interface, 1, id);
	(void)resource;
	if (!kbd) {
		wl_client_post_no_memory(client);
	}
}

static void seat_get_touch(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
	(void)client;
	(void)resource;
	(void)id;
}

static void seat_release(struct wl_client *client, struct wl_resource *resource) {
	(void)client;
	wl_resource_destroy(resource);
}

static const struct wl_seat_interface_local seat_impl = {
	.get_pointer = seat_get_pointer,
	.get_keyboard = seat_get_keyboard,
	.get_touch = seat_get_touch,
	.release = seat_release,
};

static void bind_seat(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
	(void)data;
	struct wl_resource *res = wl_resource_create(client, &wl_seat_interface, version < 5 ? version : 5, id);
	if (!res) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(res, &seat_impl, NULL, NULL);
	wl_seat_send_capabilities(res, WL_SEAT_CAPABILITY_KEYBOARD);
	wl_seat_send_name(res, "robotgo-mock-seat");
}

static void vk_keymap(struct wl_client *client, struct wl_resource *resource, uint32_t format, int32_t fd, uint32_t size) {
	(void)client;
	(void)resource;
	(void)format;
	(void)size;
	if (fd >= 0) {
		close(fd);
	}
}

static void vk_key(struct wl_client *client, struct wl_resource *resource, uint32_t time, uint32_t key, uint32_t state) {
	(void)client;
	(void)resource;
	(void)time;
	mk_last_key = key;
	mk_last_state = state;
	mk_key_events++;
	if (mk_expected_keys > 0 && mk_key_events >= mk_expected_keys) {
		mk_running = 0;
	}
}

static void vk_modifiers(struct wl_client *client, struct wl_resource *resource, uint32_t depressed, uint32_t latched, uint32_t locked, uint32_t group) {
	(void)client;
	(void)resource;
	(void)latched;
	(void)locked;
	(void)group;
	mk_last_mods = depressed;
	mk_mod_events++;
}

static void vk_destroy(struct wl_client *client, struct wl_resource *resource) {
	(void)client;
	wl_resource_destroy(resource);
}

static const struct zwp_virtual_keyboard_v1_interface vk_impl = {
	.keymap = vk_keymap,
	.key = vk_key,
	.modifiers = vk_modifiers,
	.destroy = vk_destroy,
};

static void manager_create_virtual_keyboard(struct wl_client *client, struct wl_resource *resource, uint32_t id, struct wl_resource *seat) {
	(void)resource;
	(void)seat;
	struct wl_resource *vk = wl_resource_create(client, &zwp_virtual_keyboard_v1_interface, 1, id);
	if (!vk) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(vk, &vk_impl, NULL, NULL);
}

static void manager_destroy(struct wl_client *client, struct wl_resource *resource) {
	(void)client;
	wl_resource_destroy(resource);
}

static const struct zwp_virtual_keyboard_manager_v1_interface vk_manager_impl = {
	.create_virtual_keyboard = manager_create_virtual_keyboard,
	.destroy = manager_destroy,
};

static void bind_vk_manager(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
	(void)data;
	struct wl_resource *res = wl_resource_create(client, &zwp_virtual_keyboard_manager_v1_interface, version < 1 ? version : 1, id);
	if (!res) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(res, &vk_manager_impl, NULL, NULL);
}

void run_mock_keyboard_server(const char *socket, uint32_t expected_keys, uint32_t timeout_ms) {
	mk_key_events = 0;
	mk_mod_events = 0;
	mk_last_mods = 0;
	mk_last_key = 0;
	mk_last_state = 0;
	mk_expected_keys = expected_keys;
	mk_running = 1;

	mk_display = wl_display_create();
	if (!mk_display) {
		return;
	}
	mk_loop = wl_display_get_event_loop(mk_display);
	mk_seat_global = wl_global_create(mk_display, &wl_seat_interface, 5, NULL, bind_seat);
	mk_vk_global = wl_global_create(mk_display, &zwp_virtual_keyboard_manager_v1_interface, 1, NULL, bind_vk_manager);
	if (!mk_seat_global || !mk_vk_global) {
		mk_running = 0;
	}

	if (wl_display_add_socket(mk_display, socket) < 0) {
		mk_running = 0;
	}

	uint64_t elapsed = 0;
	while (mk_running) {
		wl_event_loop_dispatch(mk_loop, 10);
		wl_display_flush_clients(mk_display);
		elapsed += 10;
		if (timeout_ms > 0 && elapsed >= timeout_ms) {
			break;
		}
	}

	wl_display_destroy_clients(mk_display);
	wl_display_destroy(mk_display);
	mk_display = NULL;
	mk_loop = NULL;
	mk_seat_global = NULL;
	mk_vk_global = NULL;
	mk_running = 0;
}
