/* Always execute checks, even when the configured compiler selects release mode. */
#ifdef NDEBUG
#undef NDEBUG
#endif
#include <assert.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

typedef intptr_t intptr;

#if defined(IS_MACOSX)
typedef uint32_t CGDirectDisplayID;
struct CGDisplayMode {
    size_t pixel_width;
    size_t width;
    int live;
};
typedef struct CGDisplayMode *CGDisplayModeRef;

static struct CGDisplayMode mode;
static int fail_copy;
static int main_calls, copy_calls, pixel_calls, width_calls, release_calls;
static CGDirectDisplayID requested_display;

static CGDirectDisplayID CGMainDisplayID(void) {
    main_calls++;
    return 91;
}

static CGDisplayModeRef CGDisplayCopyDisplayMode(CGDirectDisplayID display) {
    assert(!mode.live);
    copy_calls++;
    requested_display = display;
    if (fail_copy) {
        return NULL;
    }
    mode.live = 1;
    return &mode;
}

static size_t CGDisplayModeGetPixelWidth(CGDisplayModeRef ref) {
    assert(ref == &mode && mode.live);
    pixel_calls++;
    return ref->pixel_width;
}

static size_t CGDisplayModeGetWidth(CGDisplayModeRef ref) {
    assert(ref == &mode && mode.live);
    width_calls++;
    return ref->width;
}

static void CGDisplayModeRelease(CGDisplayModeRef ref) {
    assert(ref == &mode && mode.live);
    assert(pixel_calls == 1 && width_calls == 1);
    mode.live = 0;
    release_calls++;
}
#elif defined(IS_WINDOWS)
typedef void *HWND;
struct DesktopDC { int live; };
typedef struct DesktopDC *HDC;
#define LOGPIXELSX 88

static struct DesktopDC dc;
static int fail_get_dc, configured_dpi;
static int get_calls, caps_calls, release_calls;

static HDC GetDC(HWND window) {
    assert(window == NULL && !dc.live);
    get_calls++;
    if (fail_get_dc) {
        return NULL;
    }
    dc.live = 1;
    return &dc;
}

static int GetDeviceCaps(HDC ref, int index) {
    assert(ref == &dc && dc.live && index == LOGPIXELSX);
    caps_calls++;
    return configured_dpi;
}

static int ReleaseDC(HWND window, HDC ref) {
    assert(window == NULL && ref == &dc && dc.live);
    assert(caps_calls == 1);
    dc.live = 0;
    release_calls++;
    return 1;
}
#else
#error "Select a mocked native scale platform"
#endif

/* These are the same definitions used by screen_c.h, not a copied algorithm. */
#include "../screen/scale_c.h"

#if defined(IS_MACOSX)
static void check_scale(int32_t display, size_t pixels, size_t width,
                        int copy_fails, double expected) {
    assert(!mode.live);
    mode.pixel_width = pixels;
    mode.width = width;
    fail_copy = copy_fails;
    main_calls = copy_calls = pixel_calls = width_calls = release_calls = 0;
    assert(sys_scale(display) == expected);
    assert(main_calls == (display == -1));
    assert(copy_calls == 1);
    assert(requested_display == (display == -1 ? 91 : (uint32_t)display));
    assert(pixel_calls == !copy_fails && width_calls == !copy_fails);
    assert(release_calls == !copy_fails && !mode.live);
}

int main(void) {
    assert(scaleX() == 0);
    check_scale(-1, 1920, 1920, 0, 1.0);
    check_scale(77, 3840, 1920, 0, 2.0);
    check_scale(0, 2880, 1920, 0, 1.5);
    check_scale(-2, 3840, 1920, 0, 2.0);
    check_scale(77, 0, 1920, 0, 0.0);
    check_scale(77, 1920, 0, 0, 0.0);
    check_scale(77, 0, 0, 0, 0.0);
    check_scale(-1, 1920, 1920, 1, 0.0);
    check_scale(77, 1920, 1920, 1, 0.0);
    for (int i = 0; i < 1000; i++) {
        check_scale(-1, 3840, 1920, 0, 2.0);
        check_scale(77, 1920, 0, 0, 0.0);
        check_scale(77, 1920, 1920, 1, 0.0);
    }
    puts("macOS scale results and ownership passed");
    return 0;
}
#else
static void reset_dc(int dpi, int get_fails) {
    assert(!dc.live);
    configured_dpi = dpi;
    fail_get_dc = get_fails;
    get_calls = caps_calls = release_calls = 0;
}

static void check_ownership(int get_fails) {
    assert(get_calls == 1);
    assert(caps_calls == !get_fails && release_calls == !get_fails);
    assert(!dc.live);
}

static void check_scale(int dpi, int get_fails) {
    intptr expected_dpi = !get_fails && dpi > 0 ? dpi : 0;
    reset_dc(dpi, get_fails);
    assert(scaleX() == expected_dpi);
    check_ownership(get_fails);

    const int32_t displays[] = {-2, -1, 0, 77};
    for (size_t i = 0; i < sizeof(displays) / sizeof(displays[0]); i++) {
        reset_dc(dpi, get_fails);
        assert(sys_scale(displays[i]) == expected_dpi / 96.0);
        check_ownership(get_fails);
    }
}

int main(void) {
    const int dpis[] = {96, 120, 144, 192, 0, -1};
    for (size_t i = 0; i < sizeof(dpis) / sizeof(dpis[0]); i++) {
        check_scale(dpis[i], 0);
    }
    check_scale(192, 1);
    for (int i = 0; i < 1000; i++) {
        check_scale(144, 0);
        check_scale(0, 0);
        check_scale(192, 1);
    }
    puts("Windows scale results and ownership passed");
    return 0;
}
#endif
