#pragma once
#include "xdg-shell-client-protocol.h"
#include <wayland-client.h>

int get_bounds_wayland(struct wl_display *display, int *width, int *height);
