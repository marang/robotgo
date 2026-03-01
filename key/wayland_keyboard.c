//go:build linux && wayland
// +build linux,wayland

#include "../base/deadbeef_rand_c.h"
#include "../base/microsleep.h"
#include "keypress.h"
#include "keycode_c.h"

#define _GNU_SOURCE
#include <string.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>

#include "../virtual-keyboard-unstable-v1-client-protocol.h"

#ifndef WL_KEYBOARD_KEY_STATE_RELEASED
#define WL_KEYBOARD_KEY_STATE_RELEASED 0
#define WL_KEYBOARD_KEY_STATE_PRESSED 1
#endif

static struct wl_display *wk_display = NULL;
static struct wl_registry *wk_registry = NULL;
static struct wl_seat *wk_seat = NULL;
static struct wl_keyboard *wk_keyboard = NULL;
static struct zwp_virtual_keyboard_manager_v1 *wk_vk_manager = NULL;
static struct zwp_virtual_keyboard_v1 *wk_vkeyboard = NULL;
static struct xkb_context *wk_xkb_context = NULL;
static struct xkb_keymap *wk_keymap = NULL;
static xkb_mod_mask_t wk_modifiers = 0;

static xkb_mod_mask_t mask_for_key(MMKeyCode key) {
    switch (key) {
    case K_META:
    case K_LMETA:
    case K_RMETA:
        return XKB_MOD_MASK_LOGO;
    case K_ALT:
    case K_LALT:
    case K_RALT:
        return XKB_MOD_MASK_ALT;
    case K_CONTROL:
    case K_LCONTROL:
    case K_RCONTROL:
        return XKB_MOD_MASK_CTRL;
    case K_SHIFT:
    case K_LSHIFT:
    case K_RSHIFT:
        return XKB_MOD_MASK_SHIFT;
    default:
        return 0;
    }
}

static void wk_registry_global(void *data, struct wl_registry *registry,
                               uint32_t name, const char *interface,
                               uint32_t version) {
    (void)data;
    (void)version;
    if (strcmp(interface, "wl_seat") == 0) {
        wk_seat = wl_registry_bind(registry, name, &wl_seat_interface, 1);
    } else if (strcmp(interface, "zwp_virtual_keyboard_manager_v1") == 0) {
        wk_vk_manager = wl_registry_bind(registry, name,
                                         &zwp_virtual_keyboard_manager_v1_interface, 1);
    }
}

static void wk_registry_remove(void *data, struct wl_registry *registry,
                               uint32_t name) {
    (void)data;
    (void)registry;
    (void)name;
}

static const struct wl_registry_listener wk_registry_listener = {
    wk_registry_global,
    wk_registry_remove,
};

static void wk_seat_capabilities(void *data, struct wl_seat *seat,
                                 enum wl_seat_capability caps) {
    (void)data;
    if ((caps & WL_SEAT_CAPABILITY_KEYBOARD) && !wk_keyboard) {
        wk_keyboard = wl_seat_get_keyboard(seat);
    } else if (!(caps & WL_SEAT_CAPABILITY_KEYBOARD) && wk_keyboard) {
        wl_keyboard_destroy(wk_keyboard);
        wk_keyboard = NULL;
    }
}

static void wk_seat_name(void *data, struct wl_seat *seat, const char *name) {
    (void)data;
    (void)seat;
    (void)name;
}

static const struct wl_seat_listener wk_seat_listener = {
    wk_seat_capabilities,
    wk_seat_name,
};

static int ensure_wayland_keyboard(void) {
    if (wk_vkeyboard) {
        return 0;
    }
    if (!wk_display) {
        wk_display = wl_display_connect(NULL);
        if (!wk_display) {
            return -1;
        }
        wk_registry = wl_display_get_registry(wk_display);
        wl_registry_add_listener(wk_registry, &wk_registry_listener, NULL);
        wl_display_roundtrip(wk_display);
        if (wk_seat) {
            wl_seat_add_listener(wk_seat, &wk_seat_listener, NULL);
            wl_display_roundtrip(wk_display);
        }
    }
    if (!wk_vk_manager || !wk_seat) {
        return -1;
    }
    wk_vkeyboard = zwp_virtual_keyboard_manager_v1_create_virtual_keyboard(
        wk_vk_manager, wk_seat);
    wk_xkb_context = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
    wk_keymap = xkb_keymap_new_from_names(wk_xkb_context, NULL,
                                          XKB_KEYMAP_COMPILE_NO_FLAGS);
    const char *keymap_str = xkb_keymap_get_as_string(
        wk_keymap, XKB_KEYMAP_FORMAT_TEXT_V1);
    size_t size = strlen(keymap_str) + 1;
    int fd = memfd_create("wk_keymap", MFD_CLOEXEC);
    if (fd < 0 || write(fd, keymap_str, size) != (ssize_t)size) {
        if (fd >= 0) {
            close(fd);
        }
        return -1;
    }
    zwp_virtual_keyboard_v1_keymap(wk_vkeyboard, XKB_KEYMAP_FORMAT_TEXT_V1, fd,
                                    size);
    return 0;
}

void WL_KEY_EVENT(MMKeyCode key, bool is_press) {
    if (ensure_wayland_keyboard() != 0) {
        return;
    }
    xkb_keycode_t code = keysym_to_keycode(wk_keymap, key);
    if (code == XKB_KEY_NoSymbol) {
        return;
    }
    xkb_mod_mask_t mask = mask_for_key(key);
    if (mask) {
        if (is_press) {
            wk_modifiers |= mask;
        } else {
            wk_modifiers &= ~mask;
        }
        zwp_virtual_keyboard_v1_modifiers(wk_vkeyboard, wk_modifiers, 0, 0, 0);
    }
    uint32_t evdev = (uint32_t)(code - 8);
    zwp_virtual_keyboard_v1_key(wk_vkeyboard, 0, evdev,
                                is_press ? WL_KEYBOARD_KEY_STATE_PRESSED
                                         : WL_KEYBOARD_KEY_STATE_RELEASED);
    wl_display_flush(wk_display);
}

void WL_KEY_EVENT_WAIT(MMKeyCode key, bool is_press) {
    WL_KEY_EVENT(key, is_press);
    microsleep(DEADBEEF_UNIFORM(0.0, 0.5));
}

