
#include <stdio.h>
#include <stdlib.h>
#include <wayland-client.h>
#include "xdg-shell-client-protocol.h" // Generated from xdg-shell.xml
#include "xdg-shell-client-protocol.h" // Include the generated header

// Global variables for Wayland objects
struct wl_display *display = NULL;
struct wl_compositor *compositor = NULL;
struct xdg_wm_base *wm_base = NULL;

// Callback for registry events
static void registry_handle_global(void *data, struct wl_registry *registry,
                                   uint32_t name, const char *interface, uint32_t version) {
  if (strcmp(interface, "wl_compositor") == 0) {
      compositor = wl_registry_bind(registry, name, &wl_compositor_interface, 1);
  } else if (strcmp(interface, "xdg_wm_base") == 0) {
      wm_base = wl_registry_bind(registry, name, &xdg_wm_base_interface, 1);
  }
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_handle_global,
    .global_remove = NULL,
};

static void xdg_toplevel_handle_configure(void *data,
                                          struct xdg_toplevel *toplevel,
                                          int32_t width, int32_t height,
                                          struct wl_array *states) {
  bound_width = width;
  bound_height = height;
}

static const struct xdg_toplevel_listener xdg_toplevel_listener = {
    .configure = xdg_toplevel_handle_configure,
    .close = NULL,
};

int get_bounds_wayland(int *width, int *height) {
  display = wl_display_connect(NULL);
  if (!display) {
    return -1;
  }

  struct wl_registry *registry = wl_display_get_registry(display);
  wl_registry_add_listener(registry, &registry_listener, NULL);
  wl_display_roundtrip(display);

  if (!compositor || !wm_base) {
    wl_display_disconnect(display);
    return -1;
  }

  struct wl_surface *surface = wl_compositor_create_surface(compositor);
  if (!surface) {
    wl_display_disconnect(display);
    return -1;
  }

  struct xdg_surface *xdg_surface =
      xdg_wm_base_get_xdg_surface(wm_base, surface);
  if (!xdg_surface) {
    wl_surface_destroy(surface);
    wl_display_disconnect(display);
    return -1;
  }

  struct xdg_toplevel *xdg_toplevel = xdg_surface_get_toplevel(xdg_surface);
  xdg_toplevel_add_listener(xdg_toplevel, &xdg_toplevel_listener, NULL);

  wl_surface_commit(surface);
  wl_display_roundtrip(display);

  xdg_toplevel_destroy(xdg_toplevel);
  xdg_surface_destroy(xdg_surface);
  wl_surface_destroy(surface);
  wl_display_disconnect(display);

  if (width) {
    *width = bound_width;
  }
  if (height) {
    *height = bound_height;
  }
  return 0;
};

int main() {
void get_bounds_wayland() {
    // Connect to the Wayland display
    display = wl_display_connect(NULL);
    if (!display) {
        fprintf(stderr, "Failed to connect to Wayland display.\n");
        return EXIT_FAILURE;
        return;
    }

    // Get the registry
    struct wl_registry *registry = wl_display_get_registry(display);
    wl_registry_add_listener(registry, &registry_listener, NULL);

    // Roundtrip to get globals
    wl_display_roundtrip(display);

    if (!compositor || !wm_base) {
        fprintf(stderr, "Required Wayland globals not found.\n");
        return EXIT_FAILURE;
        return;
    }

    // Create a surface
    struct wl_surface *surface = wl_compositor_create_surface(compositor);
    if (!surface) {
        fprintf(stderr, "Failed to create surface.\n");
        return EXIT_FAILURE;
        return;
    }

    // Create an xdg_surface
    struct xdg_surface *xdg_surface = xdg_wm_base_get_xdg_surface(wm_base, surface);
    if (!xdg_surface) {
        fprintf(stderr, "Failed to create xdg_surface.\n");
        return EXIT_FAILURE;
        return;
    }

    // Create an xdg_toplevel
    struct xdg_toplevel *xdg_toplevel = xdg_surface_get_toplevel(xdg_surface);
    xdg_toplevel_add_listener(xdg_toplevel, &xdg_toplevel_listener, NULL);

    // Commit the surface
    wl_surface_commit(surface);

    // Event loop
    while (wl_display_dispatch(display) != -1) {
        // Process Wayland events
    }

    // Cleanup
    xdg_toplevel_destroy(xdg_toplevel);
    xdg_surface_destroy(xdg_surface);
    wl_surface_destroy(surface);
    wl_display_disconnect(display);

    return EXIT_SUCCESS;
}
