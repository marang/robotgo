/* Exercise production native Unicode helpers without OS frameworks or input.
 * Checks must run even with caller-supplied release flags. */
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
enum {
    ROBOTGO_KEY_OK = 0,
    ROBOTGO_KEY_INJECTION_FAILED = 3,
    ROBOTGO_KEY_INVALID = 5
};

static uint16_t delivered[8];
static size_t delivered_length;
static uintptr delivered_pid;

#if defined(IS_MACOSX)
typedef uint16_t UniChar;
typedef size_t UniCharCount;
typedef struct Source { bool live; } *CGEventSourceRef;
typedef struct Event {
    bool live, down;
    UniChar units[2];
    UniCharCount length;
} *CGEventRef;
enum { kCGEventSourceStateHIDSystemState = 1 };
static struct Source source;
static struct Event events[2];
static int create_calls, fail_create, fail_source;

static void microsleep(double delay) { assert(delay == 5.0); }
static CGEventSourceRef CGEventSourceCreate(int state) {
    assert(state == kCGEventSourceStateHIDSystemState && !source.live);
    if (fail_source) { return NULL; }
    source.live = true;
    return &source;
}
static CGEventRef CGEventCreateKeyboardEvent(CGEventSourceRef ref, int code, bool down) {
    assert(ref == &source && source.live && code == 0);
    create_calls++;
    if (create_calls == fail_create) { return NULL; }
    struct Event *event = &events[down ? 0 : 1];
    assert(!event->live);
    event->live = true;
    event->down = down;
    return event;
}
static void CGEventKeyboardSetUnicodeString(CGEventRef event, UniCharCount length,
                                            const UniChar *units) {
    assert(event->live && length >= 1 && length <= 2);
    event->length = length;
    memcpy(event->units, units, length * sizeof(*units));
}
static void CFRelease(void *ref) {
    if (ref == &source) {
        assert(source.live);
        source.live = false;
    } else {
        struct Event *event = ref;
        assert((event == &events[0] || event == &events[1]) && event->live);
        event->live = false;
    }
}
static int SendTo(uintptr pid, CGEventRef event) {
    assert(event->live);
    delivered_pid = pid;
    memcpy(delivered + delivered_length, event->units, event->length * sizeof(UniChar));
    delivered_length += event->length;
    CFRelease(event);
    return 0;
}
#elif defined(IS_WINDOWS)
typedef unsigned UINT;
typedef uint32_t DWORD;
typedef void *HWND;
typedef struct { int type; struct {
    uint16_t wVk, wScan;
    DWORD dwFlags, time;
    uintptr_t dwExtraInfo;
} ki; } INPUT;
enum { INPUT_KEYBOARD = 1, KEYEVENTF_KEYUP = 2, KEYEVENTF_UNICODE = 4, WM_CHAR = 0x102 };
static HWND resolved_hwnd = (HWND)(uintptr_t)99;
static int resolve_calls, post_calls, fail_post;
static int8_t resolved_is_pid;
static UINT requested[8], send_calls, send_result[8];
static INPUT sent[8][4];
static int locked;
typedef int SRWLOCK;
static SRWLOCK robotgo_win32_toggle_lock;
static void AcquireSRWLockExclusive(SRWLOCK *lock) {
    assert(lock == &robotgo_win32_toggle_lock && !locked);
    locked = 1;
}
static void ReleaseSRWLockExclusive(SRWLOCK *lock) {
    assert(lock == &robotgo_win32_toggle_lock && locked);
    locked = 0;
}
static HWND getHwnd(uintptr pid, int8_t is_pid) {
    resolve_calls++;
    delivered_pid = pid;
    resolved_is_pid = is_pid;
    return resolved_hwnd;
}
static int PostMessageW(HWND hwnd, UINT message, uintptr_t value, intptr_t extra) {
    assert(hwnd == resolved_hwnd && message == WM_CHAR && extra == 0);
    post_calls++;
    if (post_calls == fail_post) { return 0; }
    delivered[delivered_length++] = (uint16_t)value;
    return 1;
}
static UINT SendInput(UINT count, INPUT *input, int size) {
    assert(count >= 1 && count <= 4 && size == sizeof(INPUT));
    assert(send_calls < 8);
    requested[send_calls] = count;
    memcpy(sent[send_calls], input, count * sizeof(INPUT));
    UINT result = send_result[send_calls++];
    if (result > count) { result = count; }
    for (UINT index = 0; index < result; index++) {
        assert(input[index].type == INPUT_KEYBOARD && input[index].ki.wVk == 0);
        assert(input[index].ki.time == 0 && input[index].ki.dwExtraInfo == 0);
        if (!(input[index].ki.dwFlags & KEYEVENTF_KEYUP)) {
            delivered[delivered_length++] = input[index].ki.wScan;
        }
    }
    return result;
}
#else
#error "Select a mocked native Unicode platform"
#endif

#include "../key/unicode_c.h"

static void reset(void) {
    delivered_length = 0;
    delivered_pid = 0;
#if defined(IS_MACOSX)
    assert(!source.live && !events[0].live && !events[1].live);
    create_calls = fail_create = fail_source = 0;
#else
    assert(!locked);
    assert(robotgo_win32_unicode_release_count == 0);
    send_calls = 0;
    resolve_calls = post_calls = fail_post = 0;
    resolved_hwnd = (HWND)(uintptr_t)99;
    for (size_t index = 0; index < 8; index++) { send_result[index] = 4; }
#endif
}

static void check_scalar(unsigned value, uint16_t first, uint16_t second, size_t length) {
    reset();
    assert(ROBOTGO_NATIVE_UNICODE_TYPE(value, 0, 0) == ROBOTGO_KEY_OK);
    assert(delivered_length ==
#if defined(IS_MACOSX)
        2 * length
#else
        length
#endif
    );
    assert(delivered[0] == first && (length == 1 || delivered[1] == second));
#if defined(IS_MACOSX)
    assert(delivered[length] == first);
    assert(length == 1 || delivered[length + 1] == second);
    assert(!source.live && !events[0].live && !events[1].live);
#else
    assert(send_calls == 1 && requested[0] == 2 * length);
    for (size_t index = 0; index < length; index++) {
        assert(sent[0][index].ki.dwFlags == KEYEVENTF_UNICODE);
        assert(sent[0][index + length].ki.dwFlags == (KEYEVENTF_UNICODE | KEYEVENTF_KEYUP));
        assert(sent[0][index].ki.wScan == sent[0][index + length].ki.wScan);
    }
#endif
}

static void check_invalid(void) {
    const unsigned invalid[] = {0xd800, 0xdbff, 0xdc00, 0xdfff, 0x110000, UINT32_MAX};
    for (size_t index = 0; index < sizeof(invalid) / sizeof(invalid[0]); index++) {
        reset();
        assert(ROBOTGO_NATIVE_UNICODE_TYPE(invalid[index], 0, 0) == ROBOTGO_KEY_INVALID);
        assert(delivered_length == 0);
#if defined(IS_MACOSX)
        assert(create_calls == 0);
#else
        assert(send_calls == 0 && post_calls == 0 && resolve_calls == 0 && !locked);
#endif
    }
}

#if defined(IS_MACOSX)
static void check_failures_and_target(void) {
    reset();
    fail_source = 1;
    assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 41, 0) == ROBOTGO_KEY_INJECTION_FAILED);
    assert(create_calls == 0 && delivered_length == 0);
    for (int fail = 1; fail <= 2; fail++) {
        reset();
        fail_create = fail;
        assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 41, 0) == ROBOTGO_KEY_INJECTION_FAILED);
        assert(delivered_length == 0);
        assert(!source.live && !events[0].live && !events[1].live);
    }
    reset();
    assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 41, 0) == ROBOTGO_KEY_OK);
    assert(delivered_pid == 41 && delivered_length == 4);
    assert(events[0].down && !events[1].down);
    reset();
    fail_create = 1;
    toggleUnicode('a', true, 41);
    assert(!source.live && !events[0].live && delivered_length == 0);
    reset();
    toggleUnicode('a', true, 41);
    assert(delivered_pid == 41 && delivered[0] == 'a');
}
#else
static void check_failures_and_target(void) {
    for (int8_t mode = 0; mode <= 1; mode++) {
        reset();
        assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 41, mode) == ROBOTGO_KEY_OK);
        assert(resolve_calls == 1 && delivered_pid == 41 && resolved_is_pid == mode);
        assert(post_calls == 2 && delivered_length == 2 && send_calls == 0);
        assert(delivered[0] == 0xd83d && delivered[1] == 0xde00 && !locked);
    }
    reset();
    resolved_hwnd = NULL;
    assert(ROBOTGO_NATIVE_UNICODE_TYPE('a', 41, 0) == ROBOTGO_KEY_INVALID);
    assert(resolve_calls == 1 && post_calls == 0 && send_calls == 0 && !locked);
    for (int fail = 1; fail <= 2; fail++) {
        reset();
        fail_post = fail;
        assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 41, 0) == ROBOTGO_KEY_INJECTION_FAILED);
        assert(post_calls == fail && delivered_length == (size_t)fail - 1);
        assert(send_calls == 0 && !locked);
    }
}

static void check_partial_sends(void) {
    const unsigned scalars[] = {'a', 0x1f600};
    for (size_t scalar = 0; scalar < 2; scalar++) {
        UINT length = scalar + 1;
        for (UINT accepted = 0; accepted < 2 * length; accepted++) {
            reset();
            send_result[0] = accepted;
            assert(ROBOTGO_NATIVE_UNICODE_TYPE(scalars[scalar], 0, 0) == ROBOTGO_KEY_INJECTION_FAILED);
            UINT downs = accepted < length ? accepted : length;
            UINT ups = accepted > length ? accepted - length : 0;
            UINT residual = downs - ups;
            assert(send_calls == 1 + (residual != 0));
            assert(robotgo_win32_unicode_release_count == 0 && !locked);
            if (residual != 0) {
                assert(requested[1] == residual);
                for (UINT index = 0; index < residual; index++) {
                    assert(sent[1][index].ki.wScan == sent[0][index + ups].ki.wScan);
                    assert(sent[1][index].ki.dwFlags == (KEYEVENTF_UNICODE | KEYEVENTF_KEYUP));
                }
            }
        }
    }
}

static void check_retryable_releases(void) {
    /* Two downs accepted, neither matching up accepted, then partial cleanup.
     * Keep the second unit owned and block new text while cleanup still fails. */
    reset();
    send_result[0] = 2;
    send_result[1] = 1;
    assert(ROBOTGO_NATIVE_UNICODE_TYPE(0x1f600, 0, 0) == ROBOTGO_KEY_INJECTION_FAILED);
    assert(send_calls == 2 && requested[1] == 2);
    assert(robotgo_win32_unicode_release_count == 1);
    assert(robotgo_win32_unicode_releases[0].ki.wScan == 0xde00);
    send_result[2] = 0;
    assert(ROBOTGO_NATIVE_UNICODE_TYPE('a', 0, 0) == ROBOTGO_KEY_INJECTION_FAILED);
    assert(send_calls == 3 && requested[2] == 1 && delivered_length == 2);
    assert(robotgo_win32_unicode_release_count == 1);
    send_result[3] = 0;
    assert(ROBOTGO_WIN32_RELEASE_OWNED_UNICODE() == ROBOTGO_KEY_INJECTION_FAILED);
    assert(robotgo_win32_unicode_release_count == 1 && !locked);
    assert(ROBOTGO_WIN32_RELEASE_OWNED_UNICODE() == ROBOTGO_KEY_OK);
    assert(robotgo_win32_unicode_release_count == 0 && send_calls == 5 && !locked);
    assert(ROBOTGO_WIN32_RELEASE_OWNED_UNICODE() == ROBOTGO_KEY_OK && send_calls == 5);

    /* A later text call retries a failed owned release before its own downs. */
    reset();
    send_result[0] = 1;
    send_result[1] = 0;
    assert(ROBOTGO_NATIVE_UNICODE_TYPE('a', 0, 0) == ROBOTGO_KEY_INJECTION_FAILED);
    assert(robotgo_win32_unicode_release_count == 1);
    assert(ROBOTGO_NATIVE_UNICODE_TYPE('b', 0, 0) == ROBOTGO_KEY_OK);
    assert(send_calls == 4 && requested[2] == 1 && requested[3] == 2);
    assert(sent[2][0].ki.wScan == 'a' && sent[2][0].ki.dwFlags & KEYEVENTF_KEYUP);
    assert(sent[3][0].ki.wScan == 'b' && robotgo_win32_unicode_release_count == 0);
}
#endif

int main(void) {
    check_scalar(0, 0, 0, 1);
    check_scalar('a', 'a', 0, 1);
    check_scalar(0x20ac, 0x20ac, 0, 1);
    check_scalar(0xd7ff, 0xd7ff, 0, 1);
    check_scalar(0xe000, 0xe000, 0, 1);
    check_scalar(0xffff, 0xffff, 0, 1);
    check_scalar(0x10000, 0xd800, 0xdc00, 2);
    check_scalar(0x1f600, 0xd83d, 0xde00, 2);
    check_scalar(0x10ffff, 0xdbff, 0xdfff, 2);
    check_invalid();
    check_failures_and_target();
#if defined(IS_WINDOWS)
    check_partial_sends();
    check_retryable_releases();
#endif
    reset();
    puts("native Unicode encoding, failures, targets, and ownership passed");
    return 0;
}
