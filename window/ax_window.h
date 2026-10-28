/* Private macOS window mutations. Include after the platform AX/CF types. */
#ifndef ROBOTGO_AX_WINDOW_H_
#define ROBOTGO_AX_WINDOW_H_

#if defined(IS_MACOSX)
#include <stdint.h>

static inline bool robotgo_ax_has_role(AXUIElementRef element, CFStringRef expected) {
    if (element == NULL || CFGetTypeID(element) != AXUIElementGetTypeID()) {
        return false;
    }
    CFTypeRef role = NULL;
    AXError status = AXUIElementCopyAttributeValue(element, kAXRoleAttribute, &role);
    bool matches = status == kAXErrorSuccess && role != NULL &&
        CFGetTypeID(role) == CFStringGetTypeID() && CFEqual(role, expected);
    if (role != NULL) { CFRelease(role); }
    return matches;
}

/* Return an owned focused/main window, never an arbitrary AXWindows entry.
 * Keep set_handle_pid's application-element meaning unchanged for its other
 * callers. Check the unsigned input before narrowing it to Darwin's pid_t. */
static inline AXUIElementRef robotgo_ax_copy_pid_window(uintptr pid) {
    if (pid == 0 || pid > INT32_MAX) { return NULL; }
    pid_t requested = (pid_t)pid;
    AXUIElementRef application = AXUIElementCreateApplication(requested);
    if (application == NULL) { return NULL; }
    const CFStringRef attributes[] = { kAXFocusedWindowAttribute, kAXMainWindowAttribute };
    AXUIElementRef result = NULL;
    for (unsigned i = 0; i < sizeof(attributes) / sizeof(attributes[0]); i++) {
        CFTypeRef candidate = NULL;
        AXError status = AXUIElementCopyAttributeValue(application, attributes[i], &candidate);
        pid_t owner = 0;
        if (status == kAXErrorSuccess &&
            robotgo_ax_has_role((AXUIElementRef)candidate, kAXWindowRole) &&
            AXUIElementGetPid((AXUIElementRef)candidate, &owner) == kAXErrorSuccess &&
            owner == requested) {
            result = (AXUIElementRef)candidate; /* Transfer the Copy ownership. */
            break;
        }
        if (candidate != NULL) { CFRelease(candidate); }
    }
    CFRelease(application);
    return result;
}

static inline bool robotgo_ax_minimize_pid(uintptr pid, bool state) {
    AXUIElementRef window = robotgo_ax_copy_pid_window(pid);
    if (window == NULL) { return false; }
    AXError status = AXUIElementSetAttributeValue(window, kAXMinimizedAttribute,
        state ? kCFBooleanTrue : kCFBooleanFalse);
    CFRelease(window);
    return status == kAXErrorSuccess;
}

/* window is borrowed, including when supplied through a selected MData.
 * Never release it, resolve another window, or activate the application. */
static inline bool robotgo_ax_close_window(AXUIElementRef window) {
    if (!robotgo_ax_has_role(window, kAXWindowRole)) { return false; }
    pid_t owner = 0;
    if (AXUIElementGetPid(window, &owner) != kAXErrorSuccess || owner <= 0) {
        return false;
    }
    CFTypeRef button = NULL;
    AXError status = AXUIElementCopyAttributeValue(window, kAXCloseButtonAttribute, &button);
    pid_t button_owner = 0;
    bool closed = status == kAXErrorSuccess &&
        robotgo_ax_has_role((AXUIElementRef)button, kAXButtonRole) &&
        AXUIElementGetPid((AXUIElementRef)button, &button_owner) == kAXErrorSuccess &&
        button_owner == owner &&
        AXUIElementPerformAction((AXUIElementRef)button, kAXPressAction) == kAXErrorSuccess;
    if (button != NULL) { CFRelease(button); }
    return closed;
}

static inline bool robotgo_ax_close_pid(uintptr pid) {
    AXUIElementRef window = robotgo_ax_copy_pid_window(pid);
    if (window == NULL) { return false; }
    bool closed = robotgo_ax_close_window(window);
    CFRelease(window);
    return closed;
}
#endif
#endif
