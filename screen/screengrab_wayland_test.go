package screen

/*
#cgo CFLAGS: -std=c11
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

struct wl_interface { const char *name; };

static const struct wl_interface wl_shm_interface = { "wl_shm" };
static const struct wl_interface zwlr_screencopy_manager_v1_interface = { "zwlr_screencopy_manager_v1" };
static const struct wl_interface wl_output_interface = { "wl_output" };

static uint32_t last_version;
void *wl_registry_bind(void *registry, uint32_t name, const struct wl_interface *interface, uint32_t version) {
    (void)registry; (void)name; (void)interface;
    last_version = version;
    return (void*)1;
}

uint32_t get_last_version(void) { return last_version; }

struct wl_list { struct wl_list *prev; struct wl_list *next; };
static inline void wl_list_insert(struct wl_list *list, struct wl_list *elm) {
    (void)list; (void)elm;
}

struct output {
    struct wl_list link;
    void *wl_output;
};

struct capture {
  void *display;
  void *registry;
  void *shm;
  void *manager;
  void *frame;
  void *buffer;
  struct wl_list outputs;
  void *data;
  int width;
  int height;
  int stride;
  int done;
  int failed;
};

void registry_global(void *data, void *registry, uint32_t name, const char *interface, uint32_t version) {
  struct capture *cap = data;
  if (strcmp(interface, wl_shm_interface.name) == 0) {
    uint32_t ver = version < 1 ? version : 1;
    cap->shm = wl_registry_bind(registry, name, &wl_shm_interface, ver);
  } else if (strcmp(interface, zwlr_screencopy_manager_v1_interface.name) == 0) {
    uint32_t ver = version < 3 ? version : 3;
    cap->manager = wl_registry_bind(registry, name, &zwlr_screencopy_manager_v1_interface, ver);
  } else if (strcmp(interface, wl_output_interface.name) == 0) {
    struct output *out = malloc(sizeof(*out));
    if (!out) {
      return;
    }
    uint32_t ver = version < 2 ? version : 2;
    out->wl_output = wl_registry_bind(registry, name, &wl_output_interface, ver);
    wl_list_insert(&cap->outputs, &out->link);
    free(out);
  }
}
*/
import "C"

import (
	"testing"
	"unsafe"
)

func TestRegistryGlobalVersionClamping(t *testing.T) {
	t.Parallel()
	var cap C.struct_capture

	iface := C.CString("zwlr_screencopy_manager_v1")
	C.registry_global(unsafe.Pointer(&cap), nil, 1, iface, 1)
	C.free(unsafe.Pointer(iface))
	if v := C.get_last_version(); v != 1 {
		t.Fatalf("manager version = %d, want 1", v)
	}

	iface = C.CString("wl_output")
	C.registry_global(unsafe.Pointer(&cap), nil, 2, iface, 1)
	C.free(unsafe.Pointer(iface))
	if v := C.get_last_version(); v != 1 {
		t.Fatalf("output version = %d, want 1", v)
	}
}
