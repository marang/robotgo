//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

#include "keypress_c.h"

#include <sys/socket.h>

int robotgo_wayland_test_input_utf(const char *str) {
	return input_utf((char *)str);
}

int robotgo_wayland_test_type_codepoints(const uint32_t *values, size_t length,
                                         uint64_t delay_ms) {
	return robotgo_wayland_type_codepoints(values, length, delay_ms);
}

int robotgo_wayland_test_toggle_key(uint32_t keysym, int down,
                                    uint32_t flags) {
	return toggleKeyCode((MMKeyCode)keysym, down != 0,
	                     (MMKeyFlags)flags, 0);
}

int robotgo_wayland_test_keyboard_ready(void) {
	return robotgo_wayland_keyboard_ready();
}

int robotgo_wayland_test_keyboard_last_error(void) {
	return robotgo_wayland_keyboard_last_error();
}

int robotgo_wayland_test_roundtrip(void) {
	if (wk_display == NULL) {
		return -1;
	}
	return wl_display_roundtrip(wk_display);
}

int robotgo_wayland_test_disconnect_transport(void) {
	if (wk_display == NULL) {
		return -1;
	}
	return shutdown(wl_display_get_fd(wk_display), SHUT_RDWR);
}

void robotgo_wayland_test_keyboard_close(void) {
	robotgo_wayland_keyboard_close();
}
