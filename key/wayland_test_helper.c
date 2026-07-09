//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

#include "keypress_c.h"

int robotgo_wayland_test_input_utf(const char *str) {
	return input_utf((char *)str);
}
