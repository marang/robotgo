/* Production AX helpers with no Apple SDK, AX server, windows or desktop. */
#ifdef NDEBUG
#undef NDEBUG
#endif
#include <assert.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

typedef uintptr_t uintptr;
typedef int32_t pid_t;
typedef int AXError;
typedef unsigned CFTypeID;
typedef struct Object *CFTypeRef;
typedef CFTypeRef AXUIElementRef;
typedef CFTypeRef CFStringRef;

enum { kAXErrorSuccess = 0, failure = -1, ax_type = 1, string_type, other_type };
enum { role_attr, focus_attr, main_attr, minimized_attr, close_attr, press_action };
struct Object {
    CFTypeID type;
    int references;
    pid_t pid;
    AXError pid_error, role_error;
    CFTypeRef role;
    const char *text;
    int token;
    bool minimized;
};
static struct Object objects[8], constants[9];
#define kAXRoleAttribute (&constants[0])
#define kAXFocusedWindowAttribute (&constants[1])
#define kAXMainWindowAttribute (&constants[2])
#define kAXMinimizedAttribute (&constants[3])
#define kAXCloseButtonAttribute (&constants[4])
#define kAXPressAction (&constants[5])
#define kAXWindowRole (&constants[6])
#define kAXButtonRole (&constants[7])
#define kCFBooleanTrue (&constants[8])
static struct Object false_constant;
#define kCFBooleanFalse (&false_constant)
#define app (&objects[0])
#define focused_window (&objects[1])
#define main_window (&objects[2])
#define close_button (&objects[3])
#define window_role (&objects[4])
#define button_role (&objects[5])
#define malformed (&objects[6])

static AXUIElementRef focus_value, main_value;
static CFTypeRef button_value;
static AXError focus_error, main_error, button_error, set_error, press_error;
static bool fail_application;
static int app_calls, focus_calls, main_calls, set_calls, press_calls;
static pid_t requested_pid;
static AXUIElementRef mutated;

static CFTypeID AXUIElementGetTypeID(void) { return ax_type; }
static CFTypeID CFStringGetTypeID(void) { return string_type; }
static CFTypeID CFGetTypeID(CFTypeRef value) { assert(value != NULL); return value->type; }
static bool CFEqual(CFTypeRef left, CFTypeRef right) {
    assert(left->type == string_type && right->type == string_type);
    return strcmp(left->text, right->text) == 0;
}
static void CFRelease(CFTypeRef value) {
    bool known = false;
    for (unsigned i = 0; i < sizeof(objects) / sizeof(objects[0]); i++) {
        if (value == &objects[i]) { known = true; }
    }
    assert(known && value->references > 0);
    value->references--;
}
static AXUIElementRef AXUIElementCreateApplication(pid_t pid) {
    app_calls++;
    requested_pid = pid;
    if (fail_application) { return NULL; }
    app->references++;
    return app;
}
static AXError AXUIElementGetPid(AXUIElementRef element, pid_t *pid) {
    assert(element != NULL && element->type == ax_type && element->references > 0);
    *pid = element->pid;
    return element->pid_error;
}
static AXError AXUIElementCopyAttributeValue(AXUIElementRef element,
                                            CFStringRef attribute, CFTypeRef *out) {
    assert(element != NULL && element->type == ax_type && element->references > 0);
    assert(*out == NULL);
    AXError status = kAXErrorSuccess;
    if (element == app) {
        if (attribute == kAXFocusedWindowAttribute) {
            focus_calls++;
            *out = focus_value;
            status = focus_error;
        } else {
            assert(attribute == kAXMainWindowAttribute);
            main_calls++;
            *out = main_value;
            status = main_error;
        }
    } else if (attribute == kAXRoleAttribute) {
        *out = element->role;
        status = element->role_error;
    } else {
        assert(attribute == kAXCloseButtonAttribute);
        assert(element == focused_window || element == main_window);
        *out = button_value;
        status = button_error;
    }
    /* Also exercise defensive cleanup of non-null error outputs. */
    if (*out != NULL) { (*out)->references++; }
    return status;
}
static AXError AXUIElementSetAttributeValue(AXUIElementRef element,
                                           CFStringRef attribute, CFTypeRef value) {
    assert(element->references > 0 && element != app);
    assert(attribute == kAXMinimizedAttribute);
    assert(value == kCFBooleanTrue || value == kCFBooleanFalse);
    set_calls++;
    mutated = element;
    if (set_error == kAXErrorSuccess) { element->minimized = value == kCFBooleanTrue; }
    return set_error;
}
static AXError AXUIElementPerformAction(AXUIElementRef element, CFStringRef action) {
    assert(element == close_button && element->references > 0 && action == kAXPressAction);
    press_calls++;
    mutated = element;
    return press_error;
}

#include "../window/ax_window.h"

static void check_released(void) {
    for (unsigned i = 0; i < sizeof(objects) / sizeof(objects[0]); i++) {
        assert(objects[i].references == 0);
    }
}
static void reset(void) {
    check_released();
    memset(objects, 0, sizeof(objects));
    memset(constants, 0, sizeof(constants));
    for (unsigned i = 0; i < 4; i++) {
        objects[i].type = ax_type;
        objects[i].pid = 42;
        objects[i].role = window_role;
    }
    window_role->type = button_role->type = string_type;
    window_role->text = "AXWindow";
    button_role->text = "AXButton";
    kAXWindowRole->type = kAXButtonRole->type = string_type;
    kAXWindowRole->text = "AXWindow";
    kAXButtonRole->text = "AXButton";
    close_button->role = button_role;
    malformed->type = other_type;
    focus_value = focused_window;
    main_value = main_window;
    button_value = close_button;
    focus_error = main_error = button_error = set_error = press_error = kAXErrorSuccess;
    fail_application = false;
    app_calls = focus_calls = main_calls = set_calls = press_calls = 0;
    mutated = NULL;
    requested_pid = 0;
}
static void test_resolution(void) {
    reset();
    AXUIElementRef window = robotgo_ax_copy_pid_window(42);
    assert(window == focused_window && window->references == 1);
    assert(app->references == 0 && window_role->references == 0);
    assert(focus_calls == 1 && main_calls == 0 && set_calls == 0 && press_calls == 0);
    assert(requested_pid == 42 && window_role != kAXWindowRole);
    CFRelease(window);
    check_released();

    /* Every invalid focused candidate is released before trying main. */
    for (int bad = 0; bad < 9; bad++) {
        reset();
        switch (bad) {
        case 0: focus_value = NULL; break;
        case 1: focus_error = failure; break;
        case 2: focused_window->type = other_type; break;
        case 3: focused_window->role = NULL; break;
        case 4: focused_window->role = malformed; break;
        case 5: focused_window->role = button_role; break;
        case 6: focused_window->role_error = failure; break;
        case 7: focused_window->pid = 99; break;
        case 8: focused_window->pid_error = failure; break;
        }
        window = robotgo_ax_copy_pid_window(42);
        assert(window == main_window && main_calls == 1);
        assert(focused_window->references == 0 && app->references == 0);
        CFRelease(window);
        check_released();
    }
    for (int bad = 0; bad < 4; bad++) {
        reset();
        focus_value = NULL;
        if (bad == 0) { main_value = NULL; }
        if (bad == 1) { main_error = failure; }
        if (bad == 2) { main_window->role = button_role; }
        if (bad == 3) { main_window->pid = 0; }
        assert(robotgo_ax_copy_pid_window(42) == NULL);
        assert(set_calls == 0 && press_calls == 0);
        check_released();
    }
}
static void test_pid_range(void) {
    const uintptr invalid[] = { 0, (uintptr)-1, (uintptr)INT32_MAX + 1, UINTPTR_MAX };
    for (unsigned i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
        reset();
        assert(robotgo_ax_copy_pid_window(invalid[i]) == NULL);
        assert(!robotgo_ax_minimize_pid(invalid[i], true));
        assert(!robotgo_ax_close_pid(invalid[i]));
        assert(app_calls == 0 && set_calls == 0 && press_calls == 0);
        check_released();
    }
    reset();
    fail_application = true;
    assert(robotgo_ax_copy_pid_window(INT32_MAX) == NULL);
    assert(app_calls == 1 && requested_pid == INT32_MAX && focus_calls == 0);
    check_released();
}
static void test_minimize_restore(void) {
    reset();
    assert(robotgo_ax_minimize_pid(42, true));
    assert(mutated == focused_window && focused_window->minimized && set_calls == 1);
    check_released();
    reset();
    focus_value = NULL;
    main_window->minimized = true;
    assert(robotgo_ax_minimize_pid(42, false));
    assert(mutated == main_window && !main_window->minimized && set_calls == 1);
    check_released();
    reset();
    set_error = failure;
    assert(!robotgo_ax_minimize_pid(42, false));
    assert(mutated == focused_window && set_calls == 1 && main_calls == 0);
    check_released();
    reset();
    focus_value = main_value = NULL;
    assert(!robotgo_ax_minimize_pid(42, false) && set_calls == 0);
    check_released();
}
static void test_close(void) {
    reset();
    assert(robotgo_ax_close_pid(42));
    assert(mutated == close_button && press_calls == 1 && main_calls == 0);
    check_released();
    reset();
    focus_value = NULL;
    assert(robotgo_ax_close_pid(42) && main_calls == 1 && press_calls == 1);
    check_released();
    for (int bad = 0; bad < 9; bad++) {
        reset();
        switch (bad) {
        case 0: button_value = NULL; break;
        case 1: button_error = failure; break;
        case 2: close_button->type = other_type; break;
        case 3: close_button->role = NULL; break;
        case 4: close_button->role = window_role; break;
        case 5: close_button->role_error = failure; break;
        case 6: close_button->pid = 99; break;
        case 7: close_button->pid_error = failure; break;
        case 8: press_error = failure; break;
        }
        assert(!robotgo_ax_close_pid(42));
        assert(main_calls == 0 && press_calls == (bad == 8 ? 1 : 0));
        check_released();
    }
    /* A selected window is borrowed and must never be replaced with focus. */
    reset();
    main_window->references = 1;
    assert(robotgo_ax_close_window(main_window));
    assert(main_window->references == 1 && app_calls == 0 && focus_calls == 0);
    CFRelease(main_window);
    check_released();
    for (int bad = 0; bad < 5; bad++) {
        reset();
        main_window->references = 1;
        if (bad == 0) { main_window->type = other_type; }
        if (bad == 1) { main_window->role = button_role; }
        if (bad == 2) { main_window->pid_error = failure; }
        if (bad == 3) { main_window->pid = 0; }
        if (bad == 4) { press_error = failure; }
        assert(!robotgo_ax_close_window(main_window));
        assert(main_window->references == 1 && app_calls == 0 && focus_calls == 0);
        CFRelease(main_window);
        check_released();
    }
    assert(!robotgo_ax_close_window(NULL));
}
int main(void) {
    test_resolution();
    test_pid_range();
    test_minimize_restore();
    test_close();
    puts("mock AX window target and ownership checks passed");
    return 0;
}
