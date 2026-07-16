//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

#include <stdint.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <wayland-server-core.h>
#include <wayland-server-protocol.h>

#include "../../virtual-keyboard-unstable-v1-client-protocol.h"

struct zwp_virtual_keyboard_manager_v1_interface {
	/* The callback arguments follow the protocol request order: seat, new_id. */
	void (*create_virtual_keyboard)(struct wl_client *client, struct wl_resource *resource, struct wl_resource *seat, uint32_t id);
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

static _Atomic uint32_t mk_key_events = 0;
static _Atomic uint32_t mk_mod_events = 0;
static _Atomic uint32_t mk_last_mods = 0;
static _Atomic uint32_t mk_last_key = 0;
static _Atomic uint32_t mk_last_state = 0;
static _Atomic uint32_t mk_expected_keys = 0;
static _Atomic uint32_t mk_seat_resources_destroyed = 0;
static _Atomic uint32_t mk_keyboard_resources_destroyed = 0;
static _Atomic uint32_t mk_selected_seat = UINT32_MAX;
static _Atomic uint32_t mk_control_generation = 0;
static _Atomic uint32_t mk_control_applied_generation = 0;
static _Atomic uint32_t mk_control_kind = 0;
static _Atomic uint32_t mk_control_seat = 0;
static _Atomic uint32_t mk_control_capabilities = 0;
static _Atomic int mk_running = 0;
static _Atomic int mk_ready = 0;

enum mock_keyboard_control_kind {
	MK_CONTROL_NONE = 0,
	MK_CONTROL_SEAT_CAPABILITIES = 1,
	MK_CONTROL_REMOVE_SEAT_GLOBAL = 2,
};

#define MK_MAX_SEATS 4
struct mock_seat_config {
	uint32_t index;
	uint32_t capabilities;
	struct wl_global *global;
	struct wl_resource *resource;
	int announced;
};

static struct wl_display *mk_display = NULL;
static struct wl_event_loop *mk_loop = NULL;
static struct mock_seat_config mk_seats[MK_MAX_SEATS];
static struct wl_global *mk_vk_global = NULL;

uint32_t mock_keyboard_key_events(void) { return atomic_load(&mk_key_events); }
uint32_t mock_keyboard_mod_events(void) { return atomic_load(&mk_mod_events); }
uint32_t mock_keyboard_last_mods(void) { return atomic_load(&mk_last_mods); }
uint32_t mock_keyboard_last_key(void) { return atomic_load(&mk_last_key); }
uint32_t mock_keyboard_last_state(void) { return atomic_load(&mk_last_state); }
int mock_keyboard_server_ready(void) { return atomic_load(&mk_ready); }
uint32_t mock_keyboard_seat_resources_destroyed(void) { return atomic_load(&mk_seat_resources_destroyed); }
uint32_t mock_keyboard_keyboard_resources_destroyed(void) { return atomic_load(&mk_keyboard_resources_destroyed); }
uint32_t mock_keyboard_selected_seat(void) { return atomic_load(&mk_selected_seat); }
uint32_t mock_keyboard_control_applied_generation(void) {
	return atomic_load(&mk_control_applied_generation);
}

uint32_t mock_keyboard_set_seat_capabilities(uint32_t seat, uint32_t capabilities) {
	atomic_store(&mk_control_seat, seat);
	atomic_store(&mk_control_capabilities, capabilities);
	atomic_store(&mk_control_kind, MK_CONTROL_SEAT_CAPABILITIES);
	return atomic_fetch_add(&mk_control_generation, 1) + 1;
}

uint32_t mock_keyboard_remove_seat_global(uint32_t seat) {
	atomic_store(&mk_control_seat, seat);
	atomic_store(&mk_control_kind, MK_CONTROL_REMOVE_SEAT_GLOBAL);
	return atomic_fetch_add(&mk_control_generation, 1) + 1;
}

void stop_mock_keyboard_server(void) { atomic_store(&mk_running, 0); }

static void seat_get_pointer(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
	(void)client;
	(void)resource;
	(void)id;
}

static void keyboard_resource_destroyed(struct wl_resource *resource) {
	(void)resource;
	atomic_fetch_add(&mk_keyboard_resources_destroyed, 1);
}

static void keyboard_release(struct wl_client *client, struct wl_resource *resource) {
	(void)client;
	wl_resource_destroy(resource);
}

static const struct wl_keyboard_interface keyboard_impl = {
	.release = keyboard_release,
};

static void seat_get_keyboard(struct wl_client *client, struct wl_resource *resource, uint32_t id) {
	struct wl_resource *kbd = wl_resource_create(
		client, &wl_keyboard_interface, wl_resource_get_version(resource), id);
	if (!kbd) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(kbd, &keyboard_impl, NULL, keyboard_resource_destroyed);
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

static void seat_resource_destroyed(struct wl_resource *resource) {
	struct mock_seat_config *config = wl_resource_get_user_data(resource);
	if (config && config->resource == resource) {
		config->resource = NULL;
	}
	atomic_fetch_add(&mk_seat_resources_destroyed, 1);
}

static void bind_seat(struct wl_client *client, void *data, uint32_t version, uint32_t id) {
	struct mock_seat_config *config = data;
	struct wl_resource *res = wl_resource_create(client, &wl_seat_interface, version < 5 ? version : 5, id);
	if (!res) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(res, &seat_impl, config, seat_resource_destroyed);
	if (config) {
		config->resource = res;
	}
	wl_seat_send_capabilities(res, config ? config->capabilities : 0);
	if (wl_resource_get_version(res) >= WL_SEAT_NAME_SINCE_VERSION) {
		wl_seat_send_name(res, config && config->index == 0
			? "robotgo-mock-seat-0" : "robotgo-mock-seat-other");
	}
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
	atomic_store(&mk_last_key, key);
	atomic_store(&mk_last_state, state);
	uint32_t key_events = atomic_fetch_add(&mk_key_events, 1) + 1;
	uint32_t expected_keys = atomic_load(&mk_expected_keys);
	if (expected_keys > 0 && key_events >= expected_keys) {
		atomic_store(&mk_running, 0);
	}
}

static void vk_modifiers(struct wl_client *client, struct wl_resource *resource, uint32_t depressed, uint32_t latched, uint32_t locked, uint32_t group) {
	(void)client;
	(void)resource;
	(void)latched;
	(void)locked;
	(void)group;
	atomic_store(&mk_last_mods, depressed);
	atomic_fetch_add(&mk_mod_events, 1);
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

static void manager_create_virtual_keyboard(struct wl_client *client, struct wl_resource *resource, struct wl_resource *seat, uint32_t id) {
	(void)resource;
	struct mock_seat_config *config = seat ? wl_resource_get_user_data(seat) : NULL;
	atomic_store(&mk_selected_seat, config ? config->index : UINT32_MAX);
	struct wl_resource *vk = wl_resource_create(client, &zwp_virtual_keyboard_v1_interface, 1, id);
	if (!vk) {
		wl_client_post_no_memory(client);
		return;
	}
	wl_resource_set_implementation(vk, &vk_impl, NULL, NULL);
}

static const struct zwp_virtual_keyboard_manager_v1_interface vk_manager_impl = {
	.create_virtual_keyboard = manager_create_virtual_keyboard,
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

static uint32_t apply_mock_keyboard_control(void) {
	uint32_t generation = atomic_load(&mk_control_generation);
	if (generation == atomic_load(&mk_control_applied_generation)) {
		return 0;
	}

	uint32_t seat = atomic_load(&mk_control_seat);
	uint32_t kind = atomic_load(&mk_control_kind);
	if (seat < MK_MAX_SEATS) {
		struct mock_seat_config *config = &mk_seats[seat];
		if (kind == MK_CONTROL_SEAT_CAPABILITIES) {
			config->capabilities = atomic_load(&mk_control_capabilities);
			if (config->resource) {
				wl_seat_send_capabilities(config->resource, config->capabilities);
			}
		} else if (kind == MK_CONTROL_REMOVE_SEAT_GLOBAL &&
			config->global && config->announced) {
			wl_global_remove(config->global);
			config->announced = 0;
		}
	}
	return generation;
}

void run_mock_keyboard_server_with_seats(const char *socket, uint32_t expected_keys,
		uint32_t timeout_ms, uint32_t seat_count, uint32_t keyboard_seat_mask) {
	atomic_store(&mk_key_events, 0);
	atomic_store(&mk_mod_events, 0);
	atomic_store(&mk_last_mods, 0);
	atomic_store(&mk_last_key, 0);
	atomic_store(&mk_last_state, 0);
	atomic_store(&mk_expected_keys, expected_keys);
	atomic_store(&mk_seat_resources_destroyed, 0);
	atomic_store(&mk_keyboard_resources_destroyed, 0);
	atomic_store(&mk_selected_seat, UINT32_MAX);
	atomic_store(&mk_control_generation, 0);
	atomic_store(&mk_control_applied_generation, 0);
	atomic_store(&mk_control_kind, MK_CONTROL_NONE);
	atomic_store(&mk_ready, 0);
	atomic_store(&mk_running, 1);

	mk_display = wl_display_create();
	if (!mk_display) {
		atomic_store(&mk_running, 0);
		return;
	}
	mk_loop = wl_display_get_event_loop(mk_display);
	if (seat_count > MK_MAX_SEATS) {
		seat_count = MK_MAX_SEATS;
	}
	for (uint32_t index = 0; index < seat_count; index++) {
		mk_seats[index].index = index;
		mk_seats[index].capabilities = (keyboard_seat_mask & (1u << index))
			? WL_SEAT_CAPABILITY_KEYBOARD : 0;
		mk_seats[index].resource = NULL;
		mk_seats[index].global = wl_global_create(
			mk_display, &wl_seat_interface, 5, &mk_seats[index], bind_seat);
		mk_seats[index].announced = mk_seats[index].global != NULL;
		if (!mk_seats[index].global) {
			atomic_store(&mk_running, 0);
		}
	}
	mk_vk_global = wl_global_create(mk_display, &zwp_virtual_keyboard_manager_v1_interface, 1, NULL, bind_vk_manager);
	if (!mk_vk_global) {
		atomic_store(&mk_running, 0);
	}

	if (wl_display_add_socket(mk_display, socket) < 0) {
		atomic_store(&mk_running, 0);
	} else if (atomic_load(&mk_running)) {
		atomic_store(&mk_ready, 1);
	}

	struct timespec started = {0};
	int has_monotonic_start = clock_gettime(CLOCK_MONOTONIC, &started) == 0;
	while (atomic_load(&mk_running)) {
		uint32_t control_generation = apply_mock_keyboard_control();
		wl_event_loop_dispatch(mk_loop, 10);
		wl_display_flush_clients(mk_display);
		if (control_generation != 0) {
			atomic_store(&mk_control_applied_generation, control_generation);
		}
		if (timeout_ms > 0 && has_monotonic_start) {
			struct timespec now = {0};
			if (clock_gettime(CLOCK_MONOTONIC, &now) == 0) {
				uint64_t elapsed_ms =
					(uint64_t)(now.tv_sec - started.tv_sec) * 1000u;
				if (now.tv_nsec >= started.tv_nsec) {
					elapsed_ms += (uint64_t)(now.tv_nsec - started.tv_nsec) / 1000000u;
				} else {
					elapsed_ms -= 1000u;
					elapsed_ms += (uint64_t)(1000000000L + now.tv_nsec - started.tv_nsec) / 1000000u;
				}
				if (elapsed_ms >= timeout_ms) {
					break;
				}
			}
		}
	}

	wl_display_destroy_clients(mk_display);
	wl_display_destroy(mk_display);
	mk_display = NULL;
	mk_loop = NULL;
	for (uint32_t index = 0; index < MK_MAX_SEATS; index++) {
		mk_seats[index].global = NULL;
		mk_seats[index].resource = NULL;
		mk_seats[index].announced = 0;
	}
	mk_vk_global = NULL;
	atomic_store(&mk_ready, 0);
	atomic_store(&mk_running, 0);
}

void run_mock_keyboard_server(const char *socket, uint32_t expected_keys, uint32_t timeout_ms) {
	run_mock_keyboard_server_with_seats(socket, expected_keys, timeout_ms, 1, 1);
}
