#pragma once
#include <wayland-client.h>

int get_bounds_wayland(struct wl_display *display, int *width, int *height);
int get_screen_rect_wayland(struct wl_display *display, int display_id,
                            int *x, int *y, int *width, int *height);
int get_num_displays_wayland(struct wl_display *display);
int get_main_display_wayland(struct wl_display *display);
