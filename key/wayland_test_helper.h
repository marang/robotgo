#pragma once

#include <stddef.h>
#include <stdint.h>

int robotgo_wayland_test_input_utf(const char *str);
int robotgo_wayland_test_type_codepoints(const uint32_t *values, size_t length,
                                         uint64_t delay_ms);
int robotgo_wayland_test_toggle_key(uint32_t keysym, int down, uint32_t flags);
int robotgo_wayland_test_keyboard_ready(void);
int robotgo_wayland_test_keyboard_last_error(void);
int robotgo_wayland_test_roundtrip(void);
int robotgo_wayland_test_disconnect_transport(void);
void robotgo_wayland_test_keyboard_close(void);
