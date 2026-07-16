// Copyright (c) 2016-2025 AtomAI, All rights reserved.
// 
// See the COPYRIGHT file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0> 
//
// This file may not be copied, modified, or distributed
// except according to those terms.

#include "../base/deadbeef_rand_c.h"
#include "../base/microsleep.h"
#include "keypress.h"
#include "keycode_c.h"

#include <ctype.h> /* For isupper() */
#include <stdint.h>
#include <string.h>

enum RobotGoKeyStatus {
        ROBOTGO_KEY_OK = 0,
        ROBOTGO_KEY_NO_DISPLAY = 1,
        ROBOTGO_KEY_UNMAPPED = 2,
        ROBOTGO_KEY_INJECTION_FAILED = 3,
        ROBOTGO_KEY_UNSUPPORTED = 4,
        ROBOTGO_KEY_INVALID = 5,
        ROBOTGO_KEY_STATE_CONFLICT = 6,
        ROBOTGO_KEY_OWNERSHIP_CONFLICT = 7
};

#if defined(IS_MACOSX)
	#include <ApplicationServices/ApplicationServices.h>
	#import <IOKit/hidsystem/IOHIDLib.h>
	#import <IOKit/hidsystem/ev_keymap.h>
#elif defined(IS_LINUX)
#if !defined(DISPLAY_SERVER_WAYLAND)
        #include <X11/XKBlib.h>
        #include <X11/extensions/XTest.h>
        #include <X11/keysym.h>
#endif
        #include <stdlib.h>
        #include "../base/os.h"
        // #include "../base/xdisplay_c.h"
        #ifdef ROBOTGO_USE_WAYLAND
        #include <wayland-client.h>
        #include <wayland-client-protocol.h>
        #include <xkbcommon/xkbcommon.h>
        #include <errno.h>
        #include <poll.h>
        #include "../virtual-keyboard-unstable-v1-client-protocol.h"

        static int WL_KEY_EVENT(MMKeyCode key, bool is_press);
        static int WL_KEY_EVENT_WAIT(MMKeyCode key, bool is_press);
#endif
#endif

/* Convenience wrappers around ugly APIs. */
#if defined(IS_WINDOWS)
    #include "../base/pubs.h"
	HWND GetHwndByPid(DWORD dwProcessId);

	HWND getHwnd(uintptr pid, int8_t isPid) { 
		HWND hwnd = (HWND) pid;
		if (isPid == 0) { 
			hwnd = GetHwndByPid(pid);
		}
		return hwnd;	
	}

	void WIN32_KEY_EVENT_WAIT(MMKeyCode key, DWORD flags, uintptr pid) {
		win32KeyEvent(key, flags, pid, 0); 
		Sleep(DEADBEEF_RANDRANGE(0, 1));
	}
#elif defined(IS_LINUX)
        #if !defined(DISPLAY_SERVER_WAYLAND)
        Display *XGetMainDisplay(void);
        unsigned long XGetMainDisplayGeneration(void);

        enum {
                ROBOTGO_X11_CORE_MODIFIERS = 8,
                ROBOTGO_X11_MAX_MODIFIERS = 7,
                ROBOTGO_X11_KEYCODE_COUNT = 256,
                ROBOTGO_X11_ASCII_FIRST = 0x20,
                ROBOTGO_X11_ASCII_LAST = 0x7e,
                ROBOTGO_X11_ASCII_COUNT = ROBOTGO_X11_ASCII_LAST - ROBOTGO_X11_ASCII_FIRST + 1,
                ROBOTGO_X11_MAX_TOGGLE_RECORDS = 64
        };

        typedef struct RobotGoX11ResolvedKey {
                KeyCode keycode;
                KeyCode modifiers[ROBOTGO_X11_MAX_MODIFIERS];
                unsigned int modifier_count;
        } RobotGoX11ResolvedKey;

        typedef struct RobotGoX11TextSnapshot {
                XkbStateRec state;
                XModifierKeymap *modifier_map;
                KeyCode injectable_codes[ROBOTGO_X11_CORE_MODIFIERS];
                unsigned int injectable_mask;
                unsigned int safe_active_mask;
        } RobotGoX11TextSnapshot;

        typedef struct RobotGoX11ToggleRecord {
                bool active;
                MMKeyCode logical_key;
                MMKeyFlags flags;
                RobotGoX11ResolvedKey resolved;
        } RobotGoX11ToggleRecord;

        static RobotGoX11ToggleRecord robotgo_x11_toggle_records[ROBOTGO_X11_MAX_TOGGLE_RECORDS];
        static unsigned int robotgo_x11_owned_keycodes[ROBOTGO_X11_KEYCODE_COUNT];
        static unsigned long robotgo_x11_keyboard_generation = 0;

        static void X_RESET_KEYBOARD_OWNERSHIP(unsigned long generation) {
                memset(robotgo_x11_toggle_records, 0,
                       sizeof(robotgo_x11_toggle_records));
                memset(robotgo_x11_owned_keycodes, 0,
                       sizeof(robotgo_x11_owned_keycodes));
                robotgo_x11_keyboard_generation = generation;
        }

        static void X_SYNC_KEYBOARD_GENERATION(void) {
                const unsigned long generation = XGetMainDisplayGeneration();
                if (robotgo_x11_keyboard_generation != generation) {
                        X_RESET_KEYBOARD_OWNERSHIP(generation);
                }
        }

        static int X_KEY_CODE(Display *display, MMKeyCode key, KeyCode *code) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                if (code == NULL) {
                        return ROBOTGO_KEY_INVALID;
                }
                *code = XKeysymToKeycode(display, key);
                return *code == 0 ? ROBOTGO_KEY_UNMAPPED : ROBOTGO_KEY_OK;
        }

        static int X_KEYCODE_EVENT(Display *display, KeyCode code, bool is_press) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                if (code == 0) {
                        return ROBOTGO_KEY_UNMAPPED;
                }
                if (!XTestFakeKeyEvent(display, code, is_press, CurrentTime)) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }
                XSync(display, false);
                return ROBOTGO_KEY_OK;
        }

        static bool X_KEYCODE_PRESSED(const char keys[32], KeyCode code) {
                return code != 0 &&
                        (((unsigned char)keys[(unsigned int)code / 8u]) &
                         (1u << ((unsigned int)code % 8u))) != 0;
        }

        static int X_QUERY_PRESSED_KEYCODES(Display *display, char keys[32]) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                memset(keys, 0, 32);
                return XQueryKeymap(display, keys) ? ROBOTGO_KEY_OK : ROBOTGO_KEY_INJECTION_FAILED;
        }

        static int X_RELEASE_ALL_OWNED_KEYS(Display *display) {
                if (display == NULL) {
                        X_RESET_KEYBOARD_OWNERSHIP(XGetMainDisplayGeneration());
                        return ROBOTGO_KEY_NO_DISPLAY;
                }

                bool modifier_codes[ROBOTGO_X11_KEYCODE_COUNT] = {false};
                for (unsigned int record_index = 0;
                     record_index < ROBOTGO_X11_MAX_TOGGLE_RECORDS; record_index++) {
                        RobotGoX11ToggleRecord *record =
                                &robotgo_x11_toggle_records[record_index];
                        if (!record->active) {
                                continue;
                        }
                        for (unsigned int modifier = 0;
                             modifier < record->resolved.modifier_count; modifier++) {
                                KeyCode code = record->resolved.modifiers[modifier];
                                if (code != 0) {
                                        modifier_codes[(unsigned int)code] = true;
                                }
                        }
                }

                int first_error = ROBOTGO_KEY_OK;
                XGrabServer(display);
                /* Release ordinary keys before modifiers so observers never see
                 * a held main key with RobotGo's modifier already removed. */
                for (unsigned int modifiers_phase = 0; modifiers_phase < 2;
                     modifiers_phase++) {
                        for (unsigned int code = 1;
                             code < ROBOTGO_X11_KEYCODE_COUNT; code++) {
                                if (robotgo_x11_owned_keycodes[code] == 0 ||
                                    modifier_codes[code] != (modifiers_phase != 0)) {
                                        continue;
                                }
                                int status = X_KEYCODE_EVENT(
                                        display, (KeyCode)code, false);
                                if (first_error == ROBOTGO_KEY_OK &&
                                    status != ROBOTGO_KEY_OK) {
                                        first_error = status;
                                }
                        }
                }
                XUngrabServer(display);
                XSync(display, false);
                X_RESET_KEYBOARD_OWNERSHIP(XGetMainDisplayGeneration());
                return first_error;
        }

        static int X_VALIDATE_RESOLVED_KEY(const RobotGoX11ResolvedKey *resolved) {
                bool seen[ROBOTGO_X11_KEYCODE_COUNT] = {false};
                if (resolved == NULL || resolved->keycode == 0 ||
                    resolved->modifier_count > ROBOTGO_X11_MAX_MODIFIERS) {
                        return ROBOTGO_KEY_INVALID;
                }
                seen[(unsigned int)resolved->keycode] = true;
                for (unsigned int index = 0; index < resolved->modifier_count; index++) {
                        KeyCode code = resolved->modifiers[index];
                        if (code == 0 || seen[(unsigned int)code]) {
                                return ROBOTGO_KEY_STATE_CONFLICT;
                        }
                        seen[(unsigned int)code] = true;
                }
                return ROBOTGO_KEY_OK;
        }

        static int X_PREFLIGHT_RESOLVED_KEY(const RobotGoX11ResolvedKey *resolved,
                                            const char keys[32]) {
                int status = X_VALIDATE_RESOLVED_KEY(resolved);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                if (X_KEYCODE_PRESSED(keys, resolved->keycode) ||
                    robotgo_x11_owned_keycodes[(unsigned int)resolved->keycode] != 0) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                for (unsigned int index = 0; index < resolved->modifier_count; index++) {
                        KeyCode code = resolved->modifiers[index];
                        if (X_KEYCODE_PRESSED(keys, code) ||
                            robotgo_x11_owned_keycodes[(unsigned int)code] != 0) {
                                return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                        }
                }
                return ROBOTGO_KEY_OK;
        }

        static bool X_SAFE_MOMENTARY_MODIFIER(KeySym sym, unsigned int modifier_index) {
                switch (sym) {
                case XK_Shift_L:
                case XK_Shift_R:
                        return modifier_index == ShiftMapIndex;
                case XK_Mode_switch:
                case XK_ISO_Level3_Shift:
                case XK_ISO_Level5_Shift:
                        /* Never synthesize a level selector through Control,
                         * Alt/Mod1, or Super/Mod4 shortcut state. */
                        return modifier_index == Mod3MapIndex || modifier_index == Mod5MapIndex;
                default:
                        return false;
                }
        }

        static bool X_SAFE_LOCK_MODIFIER(KeySym sym, unsigned int modifier_index) {
                switch (sym) {
                case XK_Caps_Lock:
                case XK_Shift_Lock:
                        return modifier_index == LockMapIndex;
                case XK_Num_Lock:
                case XK_Scroll_Lock:
                case XK_ISO_Level3_Lock:
                case XK_ISO_Level5_Lock:
                        return true;
                default:
                        return false;
                }
        }

        static KeySym X_MODIFIER_KEYSYM(Display *display, KeyCode code, int group) {
                KeySym sym = XkbKeycodeToKeysym(display, code, group, 0);
                if (sym == NoSymbol && group != 0) {
                        sym = XkbKeycodeToKeysym(display, code, 0, 0);
                }
                return sym;
        }

        static int X_OPEN_TEXT_SNAPSHOT(Display *display, RobotGoX11TextSnapshot *snapshot) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                if (snapshot == NULL) {
                        return ROBOTGO_KEY_INVALID;
                }
                memset(snapshot, 0, sizeof(*snapshot));
                if (XkbGetState(display, XkbUseCoreKbd, &snapshot->state) != Success) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }
                snapshot->modifier_map = XGetModifierMapping(display);
                if (snapshot->modifier_map == NULL) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }

                unsigned int code_masks[ROBOTGO_X11_KEYCODE_COUNT] = {0};
                for (unsigned int modifier = 0; modifier < ROBOTGO_X11_CORE_MODIFIERS; modifier++) {
                        for (int slot = 0; slot < snapshot->modifier_map->max_keypermod; slot++) {
                                KeyCode code = snapshot->modifier_map->modifiermap[
                                        modifier * snapshot->modifier_map->max_keypermod + slot];
                                if (code != 0) {
                                        code_masks[(unsigned int)code] |= 1u << modifier;
                                }
                        }
                }

                for (unsigned int modifier = 0; modifier < ROBOTGO_X11_CORE_MODIFIERS; modifier++) {
                        const unsigned int bit = 1u << modifier;
                        bool has_code = false;
                        bool all_momentary = true;
                        bool all_lock = true;
                        KeyCode injection_code = 0;
                        for (int slot = 0; slot < snapshot->modifier_map->max_keypermod; slot++) {
                                KeyCode code = snapshot->modifier_map->modifiermap[
                                        modifier * snapshot->modifier_map->max_keypermod + slot];
                                if (code == 0) {
                                        continue;
                                }
                                has_code = true;
                                KeySym sym = X_MODIFIER_KEYSYM(display, code, snapshot->state.group);
                                if (code_masks[(unsigned int)code] != bit ||
                                    !X_SAFE_MOMENTARY_MODIFIER(sym, modifier)) {
                                        all_momentary = false;
                                } else if (injection_code == 0) {
                                        injection_code = code;
                                }
                                if (!X_SAFE_LOCK_MODIFIER(sym, modifier)) {
                                        all_lock = false;
                                }
                        }

                        if (has_code && all_momentary) {
                                if (snapshot->state.mods & bit) {
                                        snapshot->safe_active_mask |= bit;
                                } else if (injection_code != 0) {
                                        snapshot->injectable_codes[modifier] = injection_code;
                                        snapshot->injectable_mask |= bit;
                                }
                                continue;
                        }
                        if (has_code && all_lock && (snapshot->state.locked_mods & bit)) {
                                continue;
                        }
                        if (snapshot->state.mods & bit) {
                                XFreeModifiermap(snapshot->modifier_map);
                                snapshot->modifier_map = NULL;
                                return ROBOTGO_KEY_STATE_CONFLICT;
                        }
                }
                return ROBOTGO_KEY_OK;
        }

        static void X_CLOSE_TEXT_SNAPSHOT(RobotGoX11TextSnapshot *snapshot) {
                if (snapshot != NULL && snapshot->modifier_map != NULL) {
                        XFreeModifiermap(snapshot->modifier_map);
                        snapshot->modifier_map = NULL;
                }
        }

        static unsigned int X_BIT_COUNT(unsigned int value) {
                unsigned int count = 0;
                while (value != 0) {
                        count += value & 1u;
                        value >>= 1;
                }
                return count;
        }

        static int X_RESOLVE_TEXT_CHAR(Display *display, const RobotGoX11TextSnapshot *snapshot,
                                       unsigned char value, RobotGoX11ResolvedKey *resolved) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                if (snapshot == NULL || resolved == NULL ||
                    value < ROBOTGO_X11_ASCII_FIRST || value > ROBOTGO_X11_ASCII_LAST) {
                        return ROBOTGO_KEY_UNSUPPORTED;
                }
                memset(resolved, 0, sizeof(*resolved));

                int min_keycode = 0;
                int max_keycode = 0;
                XDisplayKeycodes(display, &min_keycode, &max_keycode);
                const KeySym target = (KeySym)value;
                for (unsigned int wanted = 0; wanted <= ROBOTGO_X11_MAX_MODIFIERS; wanted++) {
                        for (unsigned int injected = 0;
                             injected < (1u << ROBOTGO_X11_CORE_MODIFIERS); injected++) {
                                if ((injected & ~snapshot->injectable_mask) != 0 ||
                                    X_BIT_COUNT(injected) != wanted) {
                                        continue;
                                }
                                unsigned int core_state = XkbBuildCoreState(
                                        snapshot->state.mods | injected, snapshot->state.group);
                                for (int candidate = min_keycode; candidate <= max_keycode; candidate++) {
                                        KeySym produced = NoSymbol;
                                        unsigned int consumed = 0;
                                        if (!XkbLookupKeySym(display, (KeyCode)candidate, core_state,
                                                             &consumed, &produced) || produced != target) {
                                                continue;
                                        }
                                        resolved->keycode = (KeyCode)candidate;
                                        for (unsigned int modifier = 0;
                                             modifier < ROBOTGO_X11_CORE_MODIFIERS; modifier++) {
                                                if (injected & (1u << modifier)) {
                                                        resolved->modifiers[resolved->modifier_count++] =
                                                                snapshot->injectable_codes[modifier];
                                                }
                                        }
                                        return X_VALIDATE_RESOLVED_KEY(resolved);
                                }
                        }
                }
                return snapshot->safe_active_mask != 0 || snapshot->state.locked_mods != 0
                        ? ROBOTGO_KEY_STATE_CONFLICT : ROBOTGO_KEY_UNMAPPED;
        }

        static int X_TAP_RESOLVED_KEY_GRABBED(Display *display,
                                              const RobotGoX11ResolvedKey *resolved,
                                              double hold_ms) {
                int first_error = ROBOTGO_KEY_OK;
                unsigned int pressed = 0;
                bool main_pressed = false;
                for (; pressed < resolved->modifier_count; pressed++) {
                        int status = X_KEYCODE_EVENT(display, resolved->modifiers[pressed], true);
                        if (status != ROBOTGO_KEY_OK) {
                                first_error = status;
                                break;
                        }
                }
                if (first_error == ROBOTGO_KEY_OK) {
                        first_error = X_KEYCODE_EVENT(display, resolved->keycode, true);
                        main_pressed = first_error == ROBOTGO_KEY_OK;
                }
                if (main_pressed) {
                        microsleep(hold_ms);
                        int status = X_KEYCODE_EVENT(display, resolved->keycode, false);
                        if (status != ROBOTGO_KEY_OK) {
                                (void)X_KEYCODE_EVENT(display, resolved->keycode, false);
                                if (first_error == ROBOTGO_KEY_OK) {
                                        first_error = status;
                                }
                        }
                }
                while (pressed > 0) {
                        pressed--;
                        int status = X_KEYCODE_EVENT(display, resolved->modifiers[pressed], false);
                        if (status != ROBOTGO_KEY_OK && first_error == ROBOTGO_KEY_OK) {
                                first_error = status;
                        }
                }
                return first_error;
        }

        static int X_TEXT_CHAR_TRANSACTION(Display *display, unsigned char value, double hold_ms) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                X_SYNC_KEYBOARD_GENERATION();
                XGrabServer(display);
                RobotGoX11TextSnapshot snapshot = {0};
                RobotGoX11ResolvedKey resolved = {0};
                char keys[32] = {0};
                int status = X_OPEN_TEXT_SNAPSHOT(display, &snapshot);
                if (status == ROBOTGO_KEY_OK) {
                        status = X_RESOLVE_TEXT_CHAR(display, &snapshot, value, &resolved);
                }
                if (status == ROBOTGO_KEY_OK) {
                        status = X_QUERY_PRESSED_KEYCODES(display, keys);
                }
                if (status == ROBOTGO_KEY_OK) {
                        status = X_PREFLIGHT_RESOLVED_KEY(&resolved, keys);
                }
                if (status == ROBOTGO_KEY_OK) {
                        status = X_TAP_RESOLVED_KEY_GRABBED(display, &resolved, hold_ms);
                }
                X_CLOSE_TEXT_SNAPSHOT(&snapshot);
                XUngrabServer(display);
                XSync(display, false);
                return status;
        }

        static int X_RESOLVE_KEY_TAP(Display *display, MMKeyCode key, MMKeyFlags flags,
                                     RobotGoX11ResolvedKey *resolved) {
                if (display == NULL) {
                        return ROBOTGO_KEY_NO_DISPLAY;
                }
                if (resolved == NULL) {
                        return ROBOTGO_KEY_INVALID;
                }
                memset(resolved, 0, sizeof(*resolved));
                int status = X_KEY_CODE(display, key, &resolved->keycode);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }

#define ROBOTGO_X11_APPEND_MODIFIER(flag, keysym) \
                if (flags & (flag)) { \
                        KeyCode modifier = 0; \
                        status = X_KEY_CODE(display, (keysym), &modifier); \
                        if (status != ROBOTGO_KEY_OK) { return status; } \
                        resolved->modifiers[resolved->modifier_count++] = modifier; \
                }
                ROBOTGO_X11_APPEND_MODIFIER(MOD_META, K_META)
                ROBOTGO_X11_APPEND_MODIFIER(MOD_ALT, K_ALT)
                ROBOTGO_X11_APPEND_MODIFIER(MOD_CONTROL, K_CONTROL)
                ROBOTGO_X11_APPEND_MODIFIER(MOD_SHIFT, K_SHIFT)
#undef ROBOTGO_X11_APPEND_MODIFIER
                return X_VALIDATE_RESOLVED_KEY(resolved);
        }

        static RobotGoX11ToggleRecord *X_FIND_TOGGLE_RECORD(MMKeyCode key, MMKeyFlags flags) {
                for (unsigned int index = 0; index < ROBOTGO_X11_MAX_TOGGLE_RECORDS; index++) {
                        RobotGoX11ToggleRecord *record = &robotgo_x11_toggle_records[index];
                        if (record->active && record->logical_key == key && record->flags == flags) {
                                return record;
                        }
                }
                return NULL;
        }

        static RobotGoX11ToggleRecord *X_ALLOCATE_TOGGLE_RECORD(void) {
                for (unsigned int index = 0; index < ROBOTGO_X11_MAX_TOGGLE_RECORDS; index++) {
                        if (!robotgo_x11_toggle_records[index].active) {
                                return &robotgo_x11_toggle_records[index];
                        }
                }
                return NULL;
        }

        static int X_TOGGLE_KEY_DOWN_GRABBED(Display *display, MMKeyCode key,
                                             MMKeyFlags flags) {
                if (X_FIND_TOGGLE_RECORD(key, flags) != NULL) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                RobotGoX11ToggleRecord *record = X_ALLOCATE_TOGGLE_RECORD();
                if (record == NULL) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }
                RobotGoX11ResolvedKey resolved = {0};
                int status = X_RESOLVE_KEY_TAP(display, key, flags, &resolved);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                char keys[32] = {0};
                status = X_QUERY_PRESSED_KEYCODES(display, keys);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                if (X_KEYCODE_PRESSED(keys, resolved.keycode) ||
                    robotgo_x11_owned_keycodes[(unsigned int)resolved.keycode] != 0) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                for (unsigned int index = 0; index < resolved.modifier_count; index++) {
                        KeyCode code = resolved.modifiers[index];
                        bool pressed = X_KEYCODE_PRESSED(keys, code);
                        bool owned = robotgo_x11_owned_keycodes[(unsigned int)code] != 0;
                        if (pressed != owned) {
                                return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                        }
                }

                unsigned int acquired = 0;
                for (; acquired < resolved.modifier_count; acquired++) {
                        KeyCode code = resolved.modifiers[acquired];
                        if (robotgo_x11_owned_keycodes[(unsigned int)code] == 0) {
                                status = X_KEYCODE_EVENT(display, code, true);
                                if (status != ROBOTGO_KEY_OK) {
                                        break;
                                }
                        }
                        robotgo_x11_owned_keycodes[(unsigned int)code]++;
                }
                if (status == ROBOTGO_KEY_OK) {
                        status = X_KEYCODE_EVENT(display, resolved.keycode, true);
                        if (status == ROBOTGO_KEY_OK) {
                                robotgo_x11_owned_keycodes[(unsigned int)resolved.keycode] = 1;
                        }
                }
                if (status != ROBOTGO_KEY_OK) {
                        while (acquired > 0) {
                                KeyCode code = resolved.modifiers[--acquired];
                                unsigned int *owners = &robotgo_x11_owned_keycodes[(unsigned int)code];
                                if (*owners > 0) {
                                        (*owners)--;
                                        if (*owners == 0) {
                                                (void)X_KEYCODE_EVENT(display, code, false);
                                        }
                                }
                        }
                        return status;
                }

                memset(record, 0, sizeof(*record));
                record->active = true;
                record->logical_key = key;
                record->flags = flags;
                record->resolved = resolved;
                return ROBOTGO_KEY_OK;
        }

        static bool X_TOGGLE_RECORD_EMPTY(const RobotGoX11ToggleRecord *record) {
                if (record->resolved.keycode != 0) {
                        return false;
                }
                for (unsigned int index = 0; index < record->resolved.modifier_count; index++) {
                        if (record->resolved.modifiers[index] != 0) {
                                return false;
                        }
                }
                return true;
        }

        static int X_RELEASE_OWNED_KEYCODE(Display *display, KeyCode *slot,
                                           const char keys[32]) {
                if (slot == NULL || *slot == 0) {
                        return ROBOTGO_KEY_OK;
                }
                KeyCode code = *slot;
                unsigned int *owners = &robotgo_x11_owned_keycodes[(unsigned int)code];
                if (*owners == 0) {
                        *slot = 0;
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                if (!X_KEYCODE_PRESSED(keys, code)) {
                        (*owners)--;
                        *slot = 0;
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                /* A keycode can be shared across records in either role. For
                 * example Shift may be the main key of one record and a
                 * modifier of another. Only the final RobotGo owner may emit
                 * the physical release. */
                if (*owners > 1) {
                        (*owners)--;
                        *slot = 0;
                        return ROBOTGO_KEY_OK;
                }
                int status = X_KEYCODE_EVENT(display, code, false);
                if (status == ROBOTGO_KEY_OK) {
                        (*owners)--;
                        *slot = 0;
                }
                return status;
        }

        static int X_TOGGLE_KEY_UP_GRABBED(Display *display, MMKeyCode key,
                                           MMKeyFlags flags) {
                RobotGoX11ToggleRecord *record = X_FIND_TOGGLE_RECORD(key, flags);
                if (record == NULL) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                char keys[32] = {0};
                int status = X_QUERY_PRESSED_KEYCODES(display, keys);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                int first_error = X_RELEASE_OWNED_KEYCODE(
                        display, &record->resolved.keycode, keys);
                for (unsigned int index = record->resolved.modifier_count; index > 0; index--) {
                        int release_status = X_RELEASE_OWNED_KEYCODE(
                                display, &record->resolved.modifiers[index - 1], keys);
                        if (first_error == ROBOTGO_KEY_OK && release_status != ROBOTGO_KEY_OK) {
                                first_error = release_status;
                        }
                }
                if (X_TOGGLE_RECORD_EMPTY(record)) {
                        memset(record, 0, sizeof(*record));
                }
                return first_error;
        }
        #endif
        
/** Wayland virtual keyboard (only when ROBOTGO_USE_WAYLAND) **/
        #ifdef ROBOTGO_USE_WAYLAND
        #define ROBOTGO_WAYLAND_KEYBOARD_DIAG_DEFINED 1
        #include <string.h>
        #include <unistd.h>
        #if defined(__has_include)
                #if __has_include(<sys/memfd.h>)
                        #include <sys/memfd.h>
                #elif __has_include(<linux/memfd.h>)
                        #include <linux/memfd.h>
                        #include <sys/syscall.h>
                        static int rg_memfd_create(const char *name, unsigned int flags) {
                                return (int)syscall(SYS_memfd_create, name, flags);
                        }
                        #define memfd_create rg_memfd_create
                #endif
        #else
                #include <linux/memfd.h>
                #include <sys/syscall.h>
                static int rg_memfd_create(const char *name, unsigned int flags) {
                        return (int)syscall(SYS_memfd_create, name, flags);
                }
                #define memfd_create rg_memfd_create
        #endif
	        struct zwp_virtual_keyboard_manager_v1;
	        struct zwp_virtual_keyboard_v1;
	        typedef struct RobotGoWaylandSeat {
	                uint32_t global_name;
	                struct wl_seat *seat;
	                bool keyboard_capable;
	                bool removed;
	                struct RobotGoWaylandSeat *next;
	        } RobotGoWaylandSeat;
	        enum {
	                ROBOTGO_WAYLAND_MAX_MODIFIERS = 4,
	                ROBOTGO_WAYLAND_MAX_TOGGLE_RECORDS = 64
	        };
	        typedef struct RobotGoWaylandToggleRecord {
	                bool active;
	                MMKeyCode logical_key;
	                MMKeyFlags flags;
	                xkb_keycode_t main_code;
	                MMKeyCode modifier_keys[ROBOTGO_WAYLAND_MAX_MODIFIERS];
	                xkb_keycode_t modifier_codes[ROBOTGO_WAYLAND_MAX_MODIFIERS];
	                unsigned int modifier_count;
	        } RobotGoWaylandToggleRecord;
	        static struct wl_display *wk_display = NULL;
	        static struct wl_registry *wk_registry = NULL;
        static RobotGoWaylandSeat *wk_seats = NULL;
        static RobotGoWaylandSeat *wk_selected_seat = NULL;
        static struct zwp_virtual_keyboard_manager_v1 *wk_vk_manager = NULL;
        static uint32_t wk_vk_manager_name = 0;
        static struct zwp_virtual_keyboard_v1 *wk_vkeyboard = NULL;
        static struct xkb_context *wk_xkb_context = NULL;
        static struct xkb_keymap *wk_keymap = NULL;
        static xkb_mod_mask_t wk_modifiers = 0;
	        static RobotGoWaylandToggleRecord
	                wk_toggle_records[ROBOTGO_WAYLAND_MAX_TOGGLE_RECORDS];
	        static unsigned int *wk_owned_keycodes = NULL;
	        static size_t wk_owned_keycode_count = 0;
        static int wk_last_error = 0;
	        static bool wk_runtime_active = false;
	        static bool wk_topology_dirty = false;

	        static bool robotgo_wayland_keyboard_backend_selected(void) {
#if defined(DISPLAY_SERVER_WAYLAND)
	                /* Go selected this build's only native input backend before
	                 * entering C. Do not re-read mutable display-server
	                 * environment variables in the same transaction. */
	                return true;
#else
	                return detectDisplayServer() == Wayland;
#endif
	        }
        enum {
                RG_WK_OK = 0,
                RG_WK_ERR_DISPLAY = 1,
                RG_WK_ERR_NO_SEAT = 2,
                RG_WK_ERR_NO_MANAGER = 3,
                RG_WK_ERR_CREATE = 4,
                RG_WK_ERR_XKB = 5,
                RG_WK_ERR_KEYMAP = 6,
                RG_WK_ERR_MEMFD = 7,
                RG_WK_ERR_KEYSYM = 8
        };

        static xkb_mod_mask_t mod_mask_for_name(struct xkb_keymap *keymap, const char *name) {
                if (!keymap || !name) {
                        return 0;
                }
                xkb_mod_index_t idx = xkb_keymap_mod_get_index(keymap, name);
                if (idx == XKB_MOD_INVALID) {
                        return 0;
                }
                return ((xkb_mod_mask_t)1) << idx;
        }

        static xkb_mod_mask_t mask_for_key(MMKeyCode key) {
                if (!wk_keymap) {
                        return 0;
                }
                if (key == K_META || key == K_LMETA || key == K_RMETA) {
                        return mod_mask_for_name(wk_keymap, XKB_MOD_NAME_LOGO);
                }
                if (key == K_ALT || key == K_LALT || key == K_RALT) {
                        return mod_mask_for_name(wk_keymap, XKB_MOD_NAME_ALT);
                }
                if (key == K_CONTROL || key == K_LCONTROL || key == K_RCONTROL) {
                        return mod_mask_for_name(wk_keymap, XKB_MOD_NAME_CTRL);
                }
                if (key == K_SHIFT || key == K_LSHIFT || key == K_RSHIFT) {
                        return mod_mask_for_name(wk_keymap, XKB_MOD_NAME_SHIFT);
                }
                return 0;
        }

        static xkb_keycode_t keysym_to_keycode(struct xkb_keymap *keymap, xkb_keysym_t sym) {
                if (!keymap || sym == XKB_KEY_NoSymbol) {
                        return XKB_KEY_NoSymbol;
                }
                xkb_keycode_t min = xkb_keymap_min_keycode(keymap);
                xkb_keycode_t max = xkb_keymap_max_keycode(keymap);
                for (xkb_keycode_t c = min; c <= max; c++) {
                        if (!xkb_keymap_key_repeats(keymap, c) &&
                            xkb_keymap_num_levels_for_key(keymap, c, 0) == 0) {
                                continue;
                        }
                        const xkb_keysym_t *syms = NULL;
                        int n = xkb_keymap_key_get_syms_by_level(keymap, c, 0, 0, &syms);
                        for (int i = 0; i < n; i++) {
                                if (syms[i] == sym) {
                                        return c;
                                }
                        }
                }
                return XKB_KEY_NoSymbol;
        }

	        static const struct wl_seat_listener wk_seat_listener;

	        static void wk_registry_global(void *data, struct wl_registry *registry,
	                                       uint32_t name, const char *interface,
	                                       uint32_t version) {
	                (void)data;
	                if (wk_runtime_active &&
	                    (strcmp(interface, "wl_seat") == 0 ||
	                     strcmp(interface, "zwp_virtual_keyboard_manager_v1") == 0)) {
	                        wk_topology_dirty = true;
	                        return;
	                }
	                if (strcmp(interface, "wl_seat") == 0) {
	                        RobotGoWaylandSeat *candidate = calloc(1, sizeof(*candidate));
	                        if (candidate == NULL) {
	                                return;
	                        }
	                        uint32_t bind_version = version < 5 ? version : 5;
	                        candidate->seat = wl_registry_bind(
	                                registry, name, &wl_seat_interface, bind_version);
	                        if (candidate->seat == NULL) {
	                                free(candidate);
	                                return;
	                        }
	                        candidate->global_name = name;
	                        candidate->next = wk_seats;
	                        wk_seats = candidate;
	                        wl_seat_add_listener(candidate->seat, &wk_seat_listener, candidate);
	                } else if (strcmp(interface, "zwp_virtual_keyboard_manager_v1") == 0 &&
	                           (wk_vk_manager == NULL || name < wk_vk_manager_name)) {
	                        if (wk_vk_manager != NULL) {
	                                zwp_virtual_keyboard_manager_v1_destroy(wk_vk_manager);
	                        }
                        wk_vk_manager = wl_registry_bind(registry, name,
                                &zwp_virtual_keyboard_manager_v1_interface, 1);
	                        wk_vk_manager_name = wk_vk_manager != NULL ? name : 0;
                }
        }

	        static void wk_registry_remove(void *data, struct wl_registry *registry,
	                                       uint32_t name) {
	                (void)data; (void)registry;
	                if (name == wk_vk_manager_name) {
	                        wk_topology_dirty = true;
	                        return;
	                }
	                for (RobotGoWaylandSeat *candidate = wk_seats;
	                     candidate != NULL; candidate = candidate->next) {
	                        if (candidate->global_name != name) {
	                                continue;
	                        }
	                        candidate->removed = true;
	                        wk_topology_dirty = true;
	                        return;
	                }
        }

        static const struct wl_registry_listener wk_registry_listener = {
                wk_registry_global,
                wk_registry_remove
        };

	        static void wk_seat_capabilities(void *data, struct wl_seat *seat,
	                                         enum wl_seat_capability caps) {
	                RobotGoWaylandSeat *candidate = data;
	                if (candidate == NULL || candidate->seat != seat) {
	                        return;
	                }
	                bool capable = (caps & WL_SEAT_CAPABILITY_KEYBOARD) != 0;
	                if (wk_runtime_active &&
	                    candidate->keyboard_capable != capable) {
	                        wk_topology_dirty = true;
	                }
	                candidate->keyboard_capable = capable;
        }

	        static void wk_seat_name(void *data, struct wl_seat *seat, const char *name) {
	                (void)data; (void)seat; (void)name;
	        }

		        static const struct wl_seat_listener wk_seat_listener = {
		                wk_seat_capabilities,
		                wk_seat_name
		        };

	                static void select_wayland_keyboard_seat(void) {
	                        wk_selected_seat = NULL;
	                        for (RobotGoWaylandSeat *candidate = wk_seats;
	                             candidate != NULL; candidate = candidate->next) {
	                                if (candidate->removed ||
	                                    !candidate->keyboard_capable) {
	                                        continue;
	                                }
	                                if (wk_selected_seat == NULL ||
	                                    candidate->global_name < wk_selected_seat->global_name) {
	                                        wk_selected_seat = candidate;
	                                }
	                        }
	                }

	        static void cleanup_wayland_keyboard(void) {
	                if (wk_vkeyboard) {
	                        zwp_virtual_keyboard_v1_destroy(wk_vkeyboard);
	                        wk_vkeyboard = NULL;
	                }
	                if (wk_vk_manager) {
	                        zwp_virtual_keyboard_manager_v1_destroy(wk_vk_manager);
	                        wk_vk_manager = NULL;
	                }
	                wk_vk_manager_name = 0;
	                        while (wk_seats != NULL) {
	                                RobotGoWaylandSeat *candidate = wk_seats;
	                                wk_seats = candidate->next;
	                                if (candidate->seat != NULL) {
	                                        wl_seat_destroy(candidate->seat);
	                                }
	                                free(candidate);
	                        }
	                        wk_selected_seat = NULL;
	                if (wk_registry) {
	                        wl_registry_destroy(wk_registry);
	                        wk_registry = NULL;
	                }
	                if (wk_keymap) {
	                        xkb_keymap_unref(wk_keymap);
	                        wk_keymap = NULL;
	                }
	                free(wk_owned_keycodes);
	                wk_owned_keycodes = NULL;
	                wk_owned_keycode_count = 0;
	                memset(wk_toggle_records, 0, sizeof(wk_toggle_records));
	                if (wk_xkb_context) {
	                        xkb_context_unref(wk_xkb_context);
	                        wk_xkb_context = NULL;
	                }
	                if (wk_display) {
	                        wl_display_disconnect(wk_display);
	                        wk_display = NULL;
	                }
	                wk_modifiers = 0;
	                wk_runtime_active = false;
	                wk_topology_dirty = false;
	        }

	        static int dispatch_wayland_keyboard_events(void) {
	                if (wk_display == NULL) {
	                        return -1;
	                }

	                while (wl_display_prepare_read(wk_display) != 0) {
	                        if (wl_display_dispatch_pending(wk_display) < 0) {
	                                return -1;
	                        }
	                }

	                struct pollfd readable = {
	                        .fd = wl_display_get_fd(wk_display),
	                        .events = POLLIN,
	                        .revents = 0
	                };
	                int ready = poll(&readable, 1, 0);
	                if (ready < 0) {
	                        wl_display_cancel_read(wk_display);
	                        return errno == EINTR ? 0 : -1;
	                }
	                if (ready == 0) {
	                        wl_display_cancel_read(wk_display);
	                        return 0;
	                }
	                if (readable.revents & (POLLERR | POLLHUP | POLLNVAL)) {
	                        wl_display_cancel_read(wk_display);
	                        return -1;
	                }
	                if (!(readable.revents & POLLIN)) {
	                        wl_display_cancel_read(wk_display);
	                        return 0;
	                }
	                if (wl_display_read_events(wk_display) < 0) {
	                        return -1;
	                }
	                return wl_display_dispatch_pending(wk_display) < 0 ? -1 : 0;
	        }

	        static int ensure_wayland_keyboard(void) {
	                if (wk_vkeyboard && wk_keymap && wk_display && wk_selected_seat &&
	                    wk_selected_seat->keyboard_capable) {
	                        wk_last_error = RG_WK_OK;
	                        return 0;
	                }
	                cleanup_wayland_keyboard();
	                if (!wk_display) {
	                        wk_display = wl_display_connect(NULL);
                        if (!wk_display) {
                                wk_last_error = RG_WK_ERR_DISPLAY;
                                return -1;
	                        }
	                        wk_registry = wl_display_get_registry(wk_display);
	                        if (!wk_registry) {
	                                wk_last_error = RG_WK_ERR_DISPLAY;
	                                cleanup_wayland_keyboard();
	                                return -1;
	                        }
	                        wl_registry_add_listener(wk_registry, &wk_registry_listener, NULL);
	                        if (wl_display_roundtrip(wk_display) < 0) {
	                                wk_last_error = RG_WK_ERR_DISPLAY;
	                                cleanup_wayland_keyboard();
	                                return -1;
	                        }
	                                /* Seat bindings and listeners are created from
	                                 * registry callbacks. A second roundtrip is
	                                 * required to receive every seat's capabilities. */
	                                if (wl_display_roundtrip(wk_display) < 0) {
	                                        wk_last_error = RG_WK_ERR_DISPLAY;
	                                        cleanup_wayland_keyboard();
	                                        return -1;
	                                }
	                }
	                select_wayland_keyboard_seat();
	                if (!wk_selected_seat) {
	                        wk_last_error = RG_WK_ERR_NO_SEAT;
	                        cleanup_wayland_keyboard();
	                        return -1;
	                }
	                if (!wk_vk_manager) {
	                        wk_last_error = RG_WK_ERR_NO_MANAGER;
	                        cleanup_wayland_keyboard();
	                        return -1;
                }
	        wk_vkeyboard = zwp_virtual_keyboard_manager_v1_create_virtual_keyboard(
	                wk_vk_manager, wk_selected_seat->seat);
	                if (!wk_vkeyboard) {
	                        wk_last_error = RG_WK_ERR_CREATE;
	                        cleanup_wayland_keyboard();
	                        return -1;
                }
                wk_xkb_context = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
	                if (!wk_xkb_context) {
	                        wk_last_error = RG_WK_ERR_XKB;
	                        cleanup_wayland_keyboard();
	                        return -1;
                }
                wk_keymap = xkb_keymap_new_from_names(wk_xkb_context, NULL, XKB_KEYMAP_COMPILE_NO_FLAGS);
	                if (!wk_keymap) {
	                        wk_last_error = RG_WK_ERR_KEYMAP;
	                        cleanup_wayland_keyboard();
	                        return -1;
                }
	        xkb_keycode_t max_keycode = xkb_keymap_max_keycode(wk_keymap);
	        if ((uint64_t)max_keycode + 1u > SIZE_MAX / sizeof(*wk_owned_keycodes)) {
	                wk_last_error = RG_WK_ERR_KEYMAP;
	                cleanup_wayland_keyboard();
	                return -1;
	        }
	        wk_owned_keycode_count = (size_t)max_keycode + 1u;
	        wk_owned_keycodes = calloc(wk_owned_keycode_count,
	                                   sizeof(*wk_owned_keycodes));
	        if (wk_owned_keycodes == NULL) {
	                wk_last_error = RG_WK_ERR_KEYMAP;
	                cleanup_wayland_keyboard();
	                return -1;
	        }
                char *keymap_str = xkb_keymap_get_as_string(wk_keymap, XKB_KEYMAP_FORMAT_TEXT_V1);
                if (!keymap_str) {
	                        wk_last_error = RG_WK_ERR_KEYMAP;
	                        cleanup_wayland_keyboard();
	                        return -1;
                }
                size_t size = strlen(keymap_str) + 1;
                int fd = memfd_create("wk_keymap", MFD_CLOEXEC);
                if (fd < 0 || write(fd, keymap_str, size) != (ssize_t)size) {
	                        if (fd >= 0) close(fd);
	                        free(keymap_str);
	                        wk_last_error = RG_WK_ERR_MEMFD;
	                        cleanup_wayland_keyboard();
	                        return -1;
	                }
	                zwp_virtual_keyboard_v1_keymap(wk_vkeyboard, XKB_KEYMAP_FORMAT_TEXT_V1, fd, size);
	                close(fd);
                free(keymap_str);
	                wk_runtime_active = true;
                wk_last_error = RG_WK_OK;
                return 0;
        }

	        static int refresh_wayland_keyboard(void) {
	                if (wk_display != NULL && wk_runtime_active) {
	                        if (dispatch_wayland_keyboard_events() < 0) {
	                                wk_last_error = RG_WK_ERR_DISPLAY;
	                                cleanup_wayland_keyboard();
	                                return -1;
	                        }
	                        if (wk_topology_dirty) {
	                                cleanup_wayland_keyboard();
	                        }
	                }
	                return ensure_wayland_keyboard();
	        }

        static int invalidate_wayland_keyboard(void) {
                cleanup_wayland_keyboard();
                wk_last_error = RG_WK_ERR_DISPLAY;
                return ROBOTGO_KEY_INJECTION_FAILED;
        }

        static int flush_wayland_keyboard(void) {
                enum {
                        RG_WK_FLUSH_POLL_MS = 50,
                        RG_WK_FLUSH_ATTEMPTS = 3
                };

                if (wk_display == NULL) {
                        return invalidate_wayland_keyboard();
                }
                if (wl_display_flush(wk_display) >= 0) {
                        return ROBOTGO_KEY_OK;
                }
                if (errno != EAGAIN) {
                        return invalidate_wayland_keyboard();
                }

                struct pollfd writable = {
                        .fd = wl_display_get_fd(wk_display),
                        .events = POLLOUT,
                        .revents = 0
                };
                for (int attempt = 0; attempt < RG_WK_FLUSH_ATTEMPTS; attempt++) {
                        writable.revents = 0;
                        int ready = poll(&writable, 1, RG_WK_FLUSH_POLL_MS);
                        if (ready < 0 && errno == EINTR) {
                                continue;
                        }
                        if (ready <= 0 || (writable.revents & (POLLERR | POLLHUP | POLLNVAL))) {
                                break;
                        }
                        if ((writable.revents & POLLOUT) && wl_display_flush(wk_display) >= 0) {
                                return ROBOTGO_KEY_OK;
                        }
                        if (errno != EAGAIN) {
                                break;
                        }
                }
                return invalidate_wayland_keyboard();
        }

        static int WL_KEY_CODE(MMKeyCode key, xkb_keycode_t *code) {
                if (ensure_wayland_keyboard() != 0) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }
                *code = keysym_to_keycode(wk_keymap, key);
                if (*code == XKB_KEY_NoSymbol) {
                        wk_last_error = RG_WK_ERR_KEYSYM;
                        return ROBOTGO_KEY_UNMAPPED;
                }
                return ROBOTGO_KEY_OK;
        }

        static int WL_KEYCODE_EVENT(MMKeyCode key, xkb_keycode_t code,
                                    bool is_press) {
                if (wk_vkeyboard == NULL || code == XKB_KEY_NoSymbol || code < 8) {
                        return ROBOTGO_KEY_INVALID;
                }
	        int status = ROBOTGO_KEY_OK;
                xkb_mod_mask_t mask = mask_for_key(key);
                if (mask) {
                        if (is_press) {
                                wk_modifiers |= mask;
                        } else {
                                wk_modifiers &= ~mask;
                        }
                        zwp_virtual_keyboard_v1_modifiers(wk_vkeyboard, wk_modifiers, 0, 0, 0);
                }
                uint32_t evdev = (uint32_t)(code - 8);
                zwp_virtual_keyboard_v1_key(wk_vkeyboard, 0, evdev,
                        is_press ? WL_KEYBOARD_KEY_STATE_PRESSED : WL_KEYBOARD_KEY_STATE_RELEASED);
                status = flush_wayland_keyboard();
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                wk_last_error = RG_WK_OK;
                return ROBOTGO_KEY_OK;
        }

        static int WL_KEY_EVENT(MMKeyCode key, bool is_press) {
                xkb_keycode_t code = XKB_KEY_NoSymbol;
                int status = WL_KEY_CODE(key, &code);
                return status == ROBOTGO_KEY_OK
                        ? WL_KEYCODE_EVENT(key, code, is_press) : status;
        }

        static int WL_KEY_EVENT_WAIT(MMKeyCode key, bool is_press) {
                int status = WL_KEY_EVENT(key, is_press);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                microsleep(DEADBEEF_UNIFORM(0.0, 0.5));
                return ROBOTGO_KEY_OK;
        }

        static RobotGoWaylandToggleRecord *WL_FIND_TOGGLE_RECORD(
                MMKeyCode key, MMKeyFlags flags) {
                for (unsigned int index = 0;
                     index < ROBOTGO_WAYLAND_MAX_TOGGLE_RECORDS; index++) {
                        RobotGoWaylandToggleRecord *record = &wk_toggle_records[index];
                        if (record->active && record->logical_key == key &&
                            record->flags == flags) {
                                return record;
                        }
                }
                return NULL;
        }

        static RobotGoWaylandToggleRecord *WL_ALLOCATE_TOGGLE_RECORD(void) {
                for (unsigned int index = 0;
                     index < ROBOTGO_WAYLAND_MAX_TOGGLE_RECORDS; index++) {
                        if (!wk_toggle_records[index].active) {
                                return &wk_toggle_records[index];
                        }
                }
                return NULL;
        }

        static int WL_APPEND_TOGGLE_MODIFIER(
                RobotGoWaylandToggleRecord *resolved, MMKeyCode key) {
                xkb_keycode_t code = XKB_KEY_NoSymbol;
                int status = WL_KEY_CODE(key, &code);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
                if (mask_for_key(key) == 0) {
                        return ROBOTGO_KEY_UNMAPPED;
                }
                if (code == resolved->main_code) {
                        return ROBOTGO_KEY_OK;
                }
                for (unsigned int index = 0;
                     index < resolved->modifier_count; index++) {
                        if (resolved->modifier_codes[index] == code) {
                                return ROBOTGO_KEY_OK;
                        }
                }
                if (resolved->modifier_count >= ROBOTGO_WAYLAND_MAX_MODIFIERS) {
                        return ROBOTGO_KEY_INVALID;
                }
                unsigned int index = resolved->modifier_count++;
                resolved->modifier_keys[index] = key;
                resolved->modifier_codes[index] = code;
                return ROBOTGO_KEY_OK;
        }

        static int WL_RESOLVE_TOGGLE_KEY(MMKeyCode key, MMKeyFlags flags,
                                         RobotGoWaylandToggleRecord *resolved) {
                if (resolved == NULL) {
                        return ROBOTGO_KEY_INVALID;
                }
                memset(resolved, 0, sizeof(*resolved));
                resolved->logical_key = key;
                resolved->flags = flags;
                int status = WL_KEY_CODE(key, &resolved->main_code);
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }

#define ROBOTGO_WAYLAND_APPEND_MODIFIER(flag, keysym) \
                if ((flags) & (flag)) { \
                        status = WL_APPEND_TOGGLE_MODIFIER(resolved, (keysym)); \
                        if (status != ROBOTGO_KEY_OK) { return status; } \
                }
                ROBOTGO_WAYLAND_APPEND_MODIFIER(MOD_META, K_META)
                ROBOTGO_WAYLAND_APPEND_MODIFIER(MOD_ALT, K_ALT)
                ROBOTGO_WAYLAND_APPEND_MODIFIER(MOD_CONTROL, K_CONTROL)
                ROBOTGO_WAYLAND_APPEND_MODIFIER(MOD_SHIFT, K_SHIFT)
#undef ROBOTGO_WAYLAND_APPEND_MODIFIER
                return ROBOTGO_KEY_OK;
        }

        static int WL_VALIDATE_TOGGLE_OWNERSHIP(
                const RobotGoWaylandToggleRecord *resolved) {
                if (resolved == NULL || wk_owned_keycodes == NULL ||
                    (size_t)resolved->main_code >= wk_owned_keycode_count) {
                        return ROBOTGO_KEY_INVALID;
                }
                xkb_mod_mask_t main_mask = mask_for_key(resolved->logical_key);
                bool main_active = main_mask != 0 &&
                        (wk_modifiers & main_mask) != 0;
                bool main_owned = wk_owned_keycodes[resolved->main_code] != 0;
                if (main_active != main_owned || main_owned) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                for (unsigned int index = 0;
                     index < resolved->modifier_count; index++) {
                        xkb_keycode_t code = resolved->modifier_codes[index];
                        if ((size_t)code >= wk_owned_keycode_count) {
                                return ROBOTGO_KEY_INVALID;
                        }
                        xkb_mod_mask_t mask = mask_for_key(
                                resolved->modifier_keys[index]);
                        bool active = (wk_modifiers & mask) != 0;
                        bool owned = wk_owned_keycodes[code] != 0;
                        if (active != owned) {
                                return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                        }
                }
                return ROBOTGO_KEY_OK;
        }

        static int WL_RELEASE_OWNED_KEYCODE(MMKeyCode key,
                                            xkb_keycode_t *slot) {
                if (slot == NULL || *slot == XKB_KEY_NoSymbol) {
                        return ROBOTGO_KEY_OK;
                }
                xkb_keycode_t code = *slot;
                if (wk_owned_keycodes == NULL ||
                    (size_t)code >= wk_owned_keycode_count) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                if (wk_owned_keycodes[code] == 0) {
                        *slot = XKB_KEY_NoSymbol;
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                if (wk_owned_keycodes[code] > 1) {
                        wk_owned_keycodes[code]--;
                        *slot = XKB_KEY_NoSymbol;
                        return ROBOTGO_KEY_OK;
                }
                int status = WL_KEYCODE_EVENT(key, code, false);
                if (status == ROBOTGO_KEY_OK && wk_owned_keycodes != NULL &&
                    (size_t)code < wk_owned_keycode_count) {
                        wk_owned_keycodes[code]--;
                        *slot = XKB_KEY_NoSymbol;
                }
                return status;
        }

        static int WL_TOGGLE_KEY_DOWN(MMKeyCode key, MMKeyFlags flags) {
                if (WL_FIND_TOGGLE_RECORD(key, flags) != NULL) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                RobotGoWaylandToggleRecord *slot = WL_ALLOCATE_TOGGLE_RECORD();
                if (slot == NULL) {
                        return ROBOTGO_KEY_INJECTION_FAILED;
                }
                RobotGoWaylandToggleRecord resolved = {0};
                int status = WL_RESOLVE_TOGGLE_KEY(key, flags, &resolved);
                if (status == ROBOTGO_KEY_OK) {
                        status = WL_VALIDATE_TOGGLE_OWNERSHIP(&resolved);
                }
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }

                unsigned int acquired = 0;
                for (; acquired < resolved.modifier_count; acquired++) {
                        MMKeyCode modifier_key = resolved.modifier_keys[acquired];
                        xkb_keycode_t modifier_code =
                                resolved.modifier_codes[acquired];
                        if (wk_owned_keycodes[modifier_code] == 0) {
                                status = WL_KEYCODE_EVENT(
                                        modifier_key, modifier_code, true);
                                if (status != ROBOTGO_KEY_OK) {
                                        break;
                                }
                        }
                        wk_owned_keycodes[modifier_code]++;
                }
                if (status == ROBOTGO_KEY_OK) {
                        status = WL_KEYCODE_EVENT(
                                resolved.logical_key, resolved.main_code, true);
                        if (status == ROBOTGO_KEY_OK) {
                                wk_owned_keycodes[resolved.main_code]++;
                        }
                }
                if (status != ROBOTGO_KEY_OK) {
                        while (acquired > 0 && wk_owned_keycodes != NULL) {
                                unsigned int index = --acquired;
                                (void)WL_RELEASE_OWNED_KEYCODE(
                                        resolved.modifier_keys[index],
                                        &resolved.modifier_codes[index]);
                        }
                        return status;
                }

                resolved.active = true;
                *slot = resolved;
                return ROBOTGO_KEY_OK;
        }

        static bool WL_TOGGLE_RECORD_EMPTY(
                const RobotGoWaylandToggleRecord *record) {
                if (record->main_code != XKB_KEY_NoSymbol) {
                        return false;
                }
                for (unsigned int index = 0;
                     index < record->modifier_count; index++) {
                        if (record->modifier_codes[index] != XKB_KEY_NoSymbol) {
                                return false;
                        }
                }
                return true;
        }

        static int WL_TOGGLE_KEY_UP(MMKeyCode key, MMKeyFlags flags) {
                RobotGoWaylandToggleRecord *slot =
                        WL_FIND_TOGGLE_RECORD(key, flags);
                if (slot == NULL) {
                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
                }
                int first_error = WL_RELEASE_OWNED_KEYCODE(
                        slot->logical_key, &slot->main_code);
                if (wk_owned_keycodes == NULL) {
                        return first_error;
                }
                for (unsigned int index = slot->modifier_count;
                     index > 0; index--) {
                        int release_status = WL_RELEASE_OWNED_KEYCODE(
                                slot->modifier_keys[index - 1],
                                &slot->modifier_codes[index - 1]);
                        if (first_error == ROBOTGO_KEY_OK &&
                            release_status != ROBOTGO_KEY_OK) {
                                first_error = release_status;
                        }
                        if (wk_owned_keycodes == NULL) {
                                break;
                        }
                }
	        if (WL_TOGGLE_RECORD_EMPTY(slot)) {
	                memset(slot, 0, sizeof(*slot));
	        }
                return first_error;
        }

	        int robotgo_wayland_keyboard_ready(void) {
	                if (!robotgo_wayland_keyboard_backend_selected()) {
                        wk_last_error = RG_WK_OK;
                        return 0;
                }
                return refresh_wayland_keyboard();
        }

        int robotgo_wayland_keyboard_last_error(void) {
                return wk_last_error;
        }

	        int robotgo_wayland_keyboard_backend_enabled(void) {
	                return 1;
	        }

	        void robotgo_wayland_keyboard_close(void) {
	                cleanup_wayland_keyboard();
	        }

	        int robotgo_wayland_keyboard_sync(void) {
	                return wk_display == NULL ? -1 : wl_display_roundtrip(wk_display);
	        }

	        typedef struct RobotGoWaylandResolvedKey {
	                xkb_keycode_t code;
	                xkb_mod_mask_t modifiers;
	        } RobotGoWaylandResolvedKey;

	        static int WL_RESOLVE_KEYSYM(xkb_keysym_t sym,
	                                     RobotGoWaylandResolvedKey *resolved) {
	                if (ensure_wayland_keyboard() != 0 || sym == XKB_KEY_NoSymbol) {
	                        return sym == XKB_KEY_NoSymbol ? ROBOTGO_KEY_UNMAPPED : ROBOTGO_KEY_INJECTION_FAILED;
	                }
	                if (resolved == NULL) {
	                        return ROBOTGO_KEY_INVALID;
	                }
	                resolved->code = XKB_KEY_NoSymbol;
	                resolved->modifiers = 0;

	                xkb_keycode_t min = xkb_keymap_min_keycode(wk_keymap);
	                xkb_keycode_t max = xkb_keymap_max_keycode(wk_keymap);
	                for (xkb_keycode_t c = min;
	                     c <= max && resolved->code == XKB_KEY_NoSymbol; c++) {
	                        xkb_level_index_t levels = xkb_keymap_num_levels_for_key(wk_keymap, c, 0);
                        if (levels == 0) {
                                levels = 1;
                        }

                        for (xkb_level_index_t level = 0; level < levels; level++) {
                                const xkb_keysym_t *syms = NULL;
	                                int n = xkb_keymap_key_get_syms_by_level(wk_keymap, c, 0, level, &syms);
	                                for (int i = 0; i < n; i++) {
	                                        if (syms[i] == sym) {
	                                                resolved->code = c;
	                                                xkb_mod_mask_t level_mods[8] = {0};
                                                int num_mods = xkb_keymap_key_get_mods_for_level(
                                                        wk_keymap, c, 0, level, level_mods, 8);
	                                                if (num_mods > 0) {
	                                                        resolved->modifiers = level_mods[0];
	                                                } else if (level == 1) {
	                                                        resolved->modifiers = mod_mask_for_name(
	                                                                wk_keymap, XKB_MOD_NAME_SHIFT);
	                                                }
                                                break;
                                        }
                                }
	                                if (resolved->code != XKB_KEY_NoSymbol) {
	                                        break;
                                }
                        }
                }

	                if (resolved->code == XKB_KEY_NoSymbol) {
	                        resolved->code = keysym_to_keycode(wk_keymap, sym);
	                }
	                if (resolved->code == XKB_KEY_NoSymbol || resolved->code < 8) {
	                        return ROBOTGO_KEY_UNMAPPED;
	                }
	                return ROBOTGO_KEY_OK;
	        }

	        static int WL_PREFLIGHT_RESOLVED_KEY(
	                const RobotGoWaylandResolvedKey *resolved) {
	                if (resolved == NULL || resolved->code == XKB_KEY_NoSymbol ||
	                    resolved->code < 8 || wk_vkeyboard == NULL ||
	                    wk_owned_keycodes == NULL ||
	                    (size_t)resolved->code >= wk_owned_keycode_count) {
	                        return ROBOTGO_KEY_INVALID;
	                }
	                if (wk_owned_keycodes[resolved->code] != 0) {
	                        return ROBOTGO_KEY_OWNERSHIP_CONFLICT;
	                }
	                /* Exact text may reuse modifiers that are already held, but
	                 * any additional persistent modifier would change the
	                 * requested symbol. Fail before emitting the first event. */
	                if ((wk_modifiers & ~resolved->modifiers) != 0) {
	                        return ROBOTGO_KEY_STATE_CONFLICT;
	                }
	                return ROBOTGO_KEY_OK;
	        }

	        static int WL_SEND_RESOLVED_KEY(const RobotGoWaylandResolvedKey *resolved) {
	                int preflight_status = WL_PREFLIGHT_RESOLVED_KEY(resolved);
	                if (preflight_status != ROBOTGO_KEY_OK) {
	                        return preflight_status;
	                }
	                xkb_mod_mask_t prev_mods = wk_modifiers;
	                xkb_mod_mask_t send_mods = prev_mods | resolved->modifiers;
                if (send_mods != prev_mods) {
                        wk_modifiers = send_mods;
                        zwp_virtual_keyboard_v1_modifiers(wk_vkeyboard, wk_modifiers, 0, 0, 0);
                }

	                uint32_t evdev = (uint32_t)(resolved->code - 8);
                zwp_virtual_keyboard_v1_key(wk_vkeyboard, 0, evdev, WL_KEYBOARD_KEY_STATE_PRESSED);
                zwp_virtual_keyboard_v1_key(wk_vkeyboard, 0, evdev, WL_KEYBOARD_KEY_STATE_RELEASED);

                if (send_mods != prev_mods) {
                        wk_modifiers = prev_mods;
                        zwp_virtual_keyboard_v1_modifiers(wk_vkeyboard, wk_modifiers, 0, 0, 0);
                }

                int status = flush_wayland_keyboard();
                if (status != ROBOTGO_KEY_OK) {
                        return status;
                }
	                wk_last_error = RG_WK_OK;
	                return ROBOTGO_KEY_OK;
	        }

	        static int WL_SEND_KEYSYM(xkb_keysym_t sym) {
	                RobotGoWaylandResolvedKey resolved = {0};
	                int status = WL_RESOLVE_KEYSYM(sym, &resolved);
	                return status == ROBOTGO_KEY_OK
	                        ? WL_SEND_RESOLVED_KEY(&resolved) : status;
	        }

	        int robotgo_wayland_type_codepoints(const uint32_t *values, size_t length,
	                                            uint64_t delay_ms) {
	                if (length == 0) {
	                        return ensure_wayland_keyboard() == 0
	                                ? ROBOTGO_KEY_OK : ROBOTGO_KEY_INJECTION_FAILED;
	                }
	                if (values == NULL ||
	                    length > SIZE_MAX / sizeof(RobotGoWaylandResolvedKey)) {
	                        return ROBOTGO_KEY_INVALID;
	                }
	                if (ensure_wayland_keyboard() != 0) {
	                        return ROBOTGO_KEY_INJECTION_FAILED;
	                }
	                RobotGoWaylandResolvedKey *resolved = calloc(length, sizeof(*resolved));
	                if (resolved == NULL) {
	                        return ROBOTGO_KEY_INJECTION_FAILED;
	                }

	                int status = ROBOTGO_KEY_OK;
	                for (size_t index = 0; index < length; index++) {
	                        xkb_keysym_t sym = xkb_utf32_to_keysym(values[index]);
	                        status = WL_RESOLVE_KEYSYM(sym, &resolved[index]);
	                        if (status == ROBOTGO_KEY_OK) {
	                                status = WL_PREFLIGHT_RESOLVED_KEY(
	                                        &resolved[index]);
	                        }
	                        if (status != ROBOTGO_KEY_OK) {
	                                break;
	                        }
	                }
	                for (size_t index = 0;
	                     status == ROBOTGO_KEY_OK && index < length; index++) {
	                        status = WL_SEND_RESOLVED_KEY(&resolved[index]);
	                        if (status == ROBOTGO_KEY_OK) {
	                                microsleep((double)delay_ms);
	                        }
	                }
	                free(resolved);
	                return status;
	        }

        static size_t utf8_char_len(unsigned char c) {
                if ((c & 0x80) == 0x00) return 1;
                if ((c & 0xE0) == 0xC0) return 2;
                if ((c & 0xF0) == 0xE0) return 3;
                if ((c & 0xF8) == 0xF0) return 4;
                return 1;
        }

        static uint32_t utf8_decode_one(const unsigned char *p, size_t n) {
                if (!p || n == 0) {
                        return 0;
                }
                if (n == 1) {
                        return p[0];
                }
                if (n == 2) {
                        return ((uint32_t)(p[0] & 0x1F) << 6) |
                               (uint32_t)(p[1] & 0x3F);
                }
                if (n == 3) {
                        return ((uint32_t)(p[0] & 0x0F) << 12) |
                               ((uint32_t)(p[1] & 0x3F) << 6) |
                               (uint32_t)(p[2] & 0x3F);
                }
                if (n == 4) {
                        return ((uint32_t)(p[0] & 0x07) << 18) |
                               ((uint32_t)(p[1] & 0x3F) << 12) |
                               ((uint32_t)(p[2] & 0x3F) << 6) |
                               (uint32_t)(p[3] & 0x3F);
                }
                return 0;
        }
        #endif /* ROBOTGO_USE_WAYLAND */

        /* End of Linux-specific keyboard helpers */
        #endif /* defined(IS_LINUX) */

#ifndef ROBOTGO_WAYLAND_KEYBOARD_DIAG_DEFINED
static inline int robotgo_wayland_keyboard_ready(void) { return 0; }
static inline int robotgo_wayland_keyboard_last_error(void) { return 0; }
static inline int robotgo_wayland_keyboard_backend_enabled(void) { return 0; }
static inline void robotgo_wayland_keyboard_close(void) { }
static inline int robotgo_wayland_keyboard_sync(void) { return -1; }
static inline int robotgo_wayland_type_codepoints(const uint32_t *values,
                                                  size_t length,
                                                  uint64_t delay_ms) {
	(void)values;
	(void)length;
	(void)delay_ms;
	return ROBOTGO_KEY_UNSUPPORTED;
}
#endif

#if defined(IS_MACOSX)
	int SendTo(uintptr pid, CGEventRef event) {
		if (pid != 0) {
			CGEventPostToPid(pid, event);
		} else {
			CGEventPost(kCGHIDEventTap, event);
		}
		
		CFRelease(event);
		return 0;
	}

	static io_connect_t _getAuxiliaryKeyDriver(void) {
		static mach_port_t sEventDrvrRef = 0;
		mach_port_t masterPort, service, iter;
		kern_return_t kr;

		if (!sEventDrvrRef) {
			kr = IOMasterPort(bootstrap_port, &masterPort);
			assert(KERN_SUCCESS == kr);
			kr = IOServiceGetMatchingServices(masterPort, IOServiceMatching(kIOHIDSystemClass), &iter);
			assert(KERN_SUCCESS == kr);

			service = IOIteratorNext(iter);
			assert(service);

			kr = IOServiceOpen(service, mach_task_self(), kIOHIDParamConnectType, &sEventDrvrRef);
			assert(KERN_SUCCESS == kr);

			IOObjectRelease(service);
			IOObjectRelease(iter);
		}
		return sEventDrvrRef;
	}
#elif defined(IS_WINDOWS)

	void win32KeyEvent(int key, MMKeyFlags flags, uintptr pid, int8_t isPid) {
		int scan = MapVirtualKey(key & 0xff, MAPVK_VK_TO_VSC);

		/* Set the scan code for extended keys */
		switch (key){
			case VK_RCONTROL:
			case VK_SNAPSHOT: /* Print Screen */
			case VK_RMENU: /* Right Alt / Alt Gr */
			case VK_PAUSE: /* Pause / Break */
			case VK_HOME:
			case VK_UP:
			case VK_PRIOR: /* Page up */
			case VK_LEFT:
			case VK_RIGHT:
			case VK_END:
			case VK_DOWN:
			case VK_NEXT: /* 'Page Down' */
			case VK_INSERT:
			case VK_DELETE:
			case VK_LWIN:
			case VK_RWIN:
			case VK_APPS: /* Application */
			case VK_VOLUME_MUTE:
			case VK_VOLUME_DOWN:
			case VK_VOLUME_UP:
			case VK_MEDIA_NEXT_TRACK:
			case VK_MEDIA_PREV_TRACK:
			case VK_MEDIA_STOP:
			case VK_MEDIA_PLAY_PAUSE:
			case VK_BROWSER_BACK:
			case VK_BROWSER_FORWARD:
			case VK_BROWSER_REFRESH:
			case VK_BROWSER_STOP:
			case VK_BROWSER_SEARCH:
			case VK_BROWSER_FAVORITES:
			case VK_BROWSER_HOME:
			case VK_LAUNCH_MAIL:
			{
				flags |= KEYEVENTF_EXTENDEDKEY;
				break;
			}
		}

		// todo: test this
		if (pid != 0) {
			HWND hwnd = getHwnd(pid, isPid);

			int down = (flags == 0 ? WM_KEYDOWN : WM_KEYUP);
			// SendMessage(hwnd, down, key, 0);
			PostMessageW(hwnd, down, key, 0);
			return;
		}

		/* Set the scan code for keyup */
		// if ( flags & KEYEVENTF_KEYUP ) {
		// 	scan |= 0x80;
		// }
		// keybd_event(key, scan, flags, 0);
		
		INPUT keyInput;

		keyInput.type = INPUT_KEYBOARD;
		keyInput.ki.wVk = key;
		keyInput.ki.wScan = scan;
		keyInput.ki.dwFlags = flags;
		keyInput.ki.time = 0;
		keyInput.ki.dwExtraInfo = 0;
		SendInput(1, &keyInput, sizeof(keyInput));
	}
#endif

int toggleKeyCode(MMKeyCode code, const bool down, MMKeyFlags flags, uintptr pid) {
#if defined(IS_MACOSX)
	/* The media keys all have 1000 added to them to help us detect them. */
	if (code >= 1000) {
		code = code - 1000; /* Get the real keycode. */
		NXEventData event;
		kern_return_t kr;

		IOGPoint loc = { 0, 0 };
		UInt32 evtInfo = code << 16 | (down?NX_KEYDOWN:NX_KEYUP) << 8;

		bzero(&event, sizeof(NXEventData));
		event.compound.subType = NX_SUBTYPE_AUX_CONTROL_BUTTONS;
		event.compound.misc.L[0] = evtInfo;

		kr = IOHIDPostEvent(_getAuxiliaryKeyDriver(), 
								NX_SYSDEFINED, loc, &event, kNXEventDataVersion, 0, FALSE);
		assert(KERN_SUCCESS == kr);
	} else {
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef keyEvent = CGEventCreateKeyboardEvent(source, (CGKeyCode)code, down);
		assert(keyEvent != NULL);

		CGEventSetType(keyEvent, down ? kCGEventKeyDown : kCGEventKeyUp);
		if (flags != 0) {
			CGEventSetFlags(keyEvent, (CGEventFlags) flags);
		}
		
		SendTo(pid, keyEvent);
		CFRelease(source);
	}
	return ROBOTGO_KEY_OK;
#elif defined(IS_WINDOWS)
	const DWORD dwFlags = down ? 0 : KEYEVENTF_KEYUP;

	/* Parse modifier keys. */
	if (flags & MOD_META) { WIN32_KEY_EVENT_WAIT(K_META, dwFlags, pid); }
	if (flags & MOD_ALT) { WIN32_KEY_EVENT_WAIT(K_ALT, dwFlags, pid); }
	if (flags & MOD_CONTROL) { WIN32_KEY_EVENT_WAIT(K_CONTROL, dwFlags, pid); }
	if (flags & MOD_SHIFT) { WIN32_KEY_EVENT_WAIT(K_SHIFT, dwFlags, pid); }

	win32KeyEvent(code, dwFlags, pid, 0);
	return ROBOTGO_KEY_OK;
#elif defined(IS_LINUX)
	#ifdef ROBOTGO_USE_WAYLAND
	        if (robotgo_wayland_keyboard_backend_selected()) {
	                return down
	                        ? WL_TOGGLE_KEY_DOWN(code, flags)
	                        : WL_TOGGLE_KEY_UP(code, flags);
	        }
	#endif
#if !defined(DISPLAY_SERVER_WAYLAND)
        {
                Display *display = XGetMainDisplay();
	                if (display == NULL) {
	                        return ROBOTGO_KEY_NO_DISPLAY;
	                }
                        X_SYNC_KEYBOARD_GENERATION();
                        XGrabServer(display);
                        int status = down
                                ? X_TOGGLE_KEY_DOWN_GRABBED(display, code, flags)
                                : X_TOGGLE_KEY_UP_GRABBED(display, code, flags);
                        XUngrabServer(display);
                        XSync(display, false);
                        return status;
	        }
#else
        return ROBOTGO_KEY_UNSUPPORTED;
#endif
#endif
	return ROBOTGO_KEY_UNSUPPORTED;
}

int robotgo_tap_key_code(MMKeyCode code, MMKeyFlags flags, uintptr pid) {
#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
	(void)pid;
	Display *display = XGetMainDisplay();
	if (display == NULL) {
		return ROBOTGO_KEY_NO_DISPLAY;
	}
	X_SYNC_KEYBOARD_GENERATION();
	XGrabServer(display);
	RobotGoX11ResolvedKey resolved = {0};
	int status = X_RESOLVE_KEY_TAP(display, code, flags, &resolved);
	char keys[32] = {0};
	if (status == ROBOTGO_KEY_OK) {
		status = X_QUERY_PRESSED_KEYCODES(display, keys);
	}
	if (status == ROBOTGO_KEY_OK) {
		status = X_PREFLIGHT_RESOLVED_KEY(&resolved, keys);
	}
	if (status == ROBOTGO_KEY_OK) {
		status = X_TAP_RESOLVED_KEY_GRABBED(display, &resolved, 3.0);
	}
	XUngrabServer(display);
	XSync(display, false);
	return status;
#else
	int status = toggleKeyCode(code, true, flags, pid);
	if (status != ROBOTGO_KEY_OK) {
		return status;
	}
	microsleep(3.0);
	return toggleKeyCode(code, false, flags, pid);
#endif
}

int robotgo_x11_release_owned_keys(void) {
#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
	bool has_owned_keys = false;
	for (unsigned int code = 1; code < ROBOTGO_X11_KEYCODE_COUNT; code++) {
		if (robotgo_x11_owned_keycodes[code] != 0) {
			has_owned_keys = true;
			break;
		}
	}
	if (!has_owned_keys) {
		X_RESET_KEYBOARD_OWNERSHIP(XGetMainDisplayGeneration());
		return ROBOTGO_KEY_OK;
	}

	Display *display = XGetMainDisplay();
	if (display == NULL) {
		X_RESET_KEYBOARD_OWNERSHIP(XGetMainDisplayGeneration());
		return ROBOTGO_KEY_NO_DISPLAY;
	}
	X_SYNC_KEYBOARD_GENERATION();
	return X_RELEASE_ALL_OWNED_KEYS(display);
#else
	return ROBOTGO_KEY_OK;
#endif
}

int toggleKey(char c, const bool down, MMKeyFlags flags, uintptr pid) {
	MMKeyCode keyCode = keyCodeForChar(c);

#if !defined(IS_LINUX)
                if (isupper(c) && !(flags & MOD_SHIFT)) {
                        flags |= MOD_SHIFT; /* Not sure if this is safe for all layouts. */
                }
#endif

	#if defined(IS_WINDOWS)
		int modifiers = keyCode >> 8; // Pull out modifers.

		if ((modifiers & 1) != 0) { flags |= MOD_SHIFT; } // Uptdate flags from keycode modifiers.
		if ((modifiers & 2) != 0) { flags |= MOD_CONTROL; }
		if ((modifiers & 4) != 0) { flags |= MOD_ALT; }
		keyCode = keyCode & 0xff; // Mask out modifiers.
	#endif

	return toggleKeyCode(keyCode, down, flags, pid);
}

int robotgo_x11_type_text(const char *text, uint64_t delay_ms) {
#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
	if (text == NULL) {
		return ROBOTGO_KEY_INVALID;
	}
	Display *display = XGetMainDisplay();
	if (display == NULL) {
		return ROBOTGO_KEY_NO_DISPLAY;
	}
	X_SYNC_KEYBOARD_GENERATION();

	const size_t length = strlen(text);
	if (length == 0) {
		return ROBOTGO_KEY_OK;
	}
	bool required[ROBOTGO_X11_ASCII_COUNT] = {false};
	for (size_t index = 0; index < length; index++) {
		unsigned char value = (unsigned char)text[index];
		if (value < ROBOTGO_X11_ASCII_FIRST || value > ROBOTGO_X11_ASCII_LAST) {
			return ROBOTGO_KEY_UNSUPPORTED;
		}
		required[value - ROBOTGO_X11_ASCII_FIRST] = true;
	}

	/* Resolve each required printable ASCII character at most once under one
	 * short snapshot grab. This proves that the complete string is possible and
	 * ownership-safe before the first event without blocking the X server in
	 * proportion to input length. */
	XGrabServer(display);
	RobotGoX11TextSnapshot snapshot = {0};
	RobotGoX11ResolvedKey resolved[ROBOTGO_X11_ASCII_COUNT] = {{0}};
	char keys[32] = {0};
	int status = X_OPEN_TEXT_SNAPSHOT(display, &snapshot);
	if (status == ROBOTGO_KEY_OK) {
		status = X_QUERY_PRESSED_KEYCODES(display, keys);
	}
	for (unsigned int index = 0;
	     status == ROBOTGO_KEY_OK && index < ROBOTGO_X11_ASCII_COUNT; index++) {
		if (!required[index]) {
			continue;
		}
		status = X_RESOLVE_TEXT_CHAR(
			display, &snapshot, (unsigned char)(index + ROBOTGO_X11_ASCII_FIRST),
			&resolved[index]);
		if (status == ROBOTGO_KEY_OK) {
			status = X_PREFLIGHT_RESOLVED_KEY(&resolved[index], keys);
		}
	}
	X_CLOSE_TEXT_SNAPSHOT(&snapshot);
	XUngrabServer(display);
	XSync(display, false);
	if (status != ROBOTGO_KEY_OK) {
		return status;
	}

	/* Never use a preflight keycode after releasing its snapshot. Each character
	 * is re-resolved and injected under its own short grab, so a concurrent
	 * keymap/state change either still produces the requested character or is
	 * reported explicitly. Delays never hold the global server grab. */
	status = ROBOTGO_KEY_OK;
	for (size_t index = 0; index < length; index++) {
		status = X_TEXT_CHAR_TRANSACTION(display, (unsigned char)text[index], 5.0);
		if (status != ROBOTGO_KEY_OK) {
			break;
		}
		microsleep((double)delay_ms);
	}
	return status;
#else
	(void)text;
	(void)delay_ms;
	return ROBOTGO_KEY_UNSUPPORTED;
#endif
}

// void tapKey(char c, MMKeyFlags flags){
// 	toggleKey(c, true, flags);
// 	microsleep(5.0);
// 	toggleKey(c, false, flags);
// }

#if defined(IS_MACOSX)
	void toggleUnicode(UniChar ch, const bool down, uintptr pid) {
		/* This function relies on the convenient CGEventKeyboardSetUnicodeString(), 
		convert characters to a keycode, but does not support adding modifier flags. 
		It is only used in typeString().
		-- if you need modifier keys, use the above functions instead. */
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef keyEvent = CGEventCreateKeyboardEvent(source, 0, down);
		if (keyEvent == NULL) {
			fputs("Could not create keyboard event.\n", stderr);
			return;
		}

		CGEventKeyboardSetUnicodeString(keyEvent, 1, &ch);

		SendTo(pid, keyEvent);
		CFRelease(source);
	}
#else
	#define toggleUniKey(c, down) toggleKey(c, down, MOD_NONE, 0)
#endif

// unicode type
int unicodeType(const unsigned value, uintptr pid, int8_t isPid) {
	#if defined(IS_MACOSX)
		UniChar ch = (UniChar)value; // Convert to unsigned char

		toggleUnicode(ch, true, pid);
		microsleep(5.0);
		toggleUnicode(ch, false, pid);
		return ROBOTGO_KEY_OK;
	#elif defined(IS_WINDOWS)
		if (pid != 0) {
			HWND hwnd = getHwnd(pid, isPid);

			// SendMessage(hwnd, down, value, 0);
			PostMessageW(hwnd, WM_CHAR, value, 0);
			return ROBOTGO_KEY_OK;
		}

		INPUT input[2];
        memset(input, 0, sizeof(input));

        input[0].type = INPUT_KEYBOARD;
  		input[0].ki.wVk = 0;
  		input[0].ki.wScan = value;
  		input[0].ki.dwFlags = 0x4; // KEYEVENTF_UNICODE;

  		input[1].type = INPUT_KEYBOARD;
  		input[1].ki.wVk = 0;
  		input[1].ki.wScan = value;
  		input[1].ki.dwFlags = KEYEVENTF_KEYUP | 0x4; // KEYEVENTF_UNICODE;

		return SendInput(2, input, sizeof(INPUT)) == 2 ? ROBOTGO_KEY_OK : ROBOTGO_KEY_INJECTION_FAILED;
        #elif defined(IS_LINUX)
#ifdef ROBOTGO_USE_WAYLAND
                if (robotgo_wayland_keyboard_backend_selected()) {
                        xkb_keysym_t sym = xkb_utf32_to_keysym(value);
                        if (sym == XKB_KEY_NoSymbol) {
                                return ROBOTGO_KEY_INVALID;
                        }
                        return WL_SEND_KEYSYM(sym);
                }
#endif
#if !defined(DISPLAY_SERVER_WAYLAND)
                if (value > 0x7f) {
                        return ROBOTGO_KEY_UNSUPPORTED;
                }
                return X_TEXT_CHAR_TRANSACTION(XGetMainDisplay(), (unsigned char)value, 5.0);
#else
                return ROBOTGO_KEY_UNSUPPORTED;
#endif
        #endif
	return ROBOTGO_KEY_UNSUPPORTED;
}

#if defined(IS_LINUX)
	int input_utf(const char *utf) {
#ifdef ROBOTGO_USE_WAYLAND
                if (robotgo_wayland_keyboard_backend_selected()) {
                        if (utf == NULL || *utf == '\0') {
                                return ROBOTGO_KEY_INVALID;
                        }

                        size_t utf_len = strlen(utf);
                        size_t first_len = utf8_char_len((unsigned char)*utf);
                        if (utf_len != first_len) {
                                xkb_keysym_t named = xkb_keysym_from_name(
                                        utf, XKB_KEYSYM_CASE_INSENSITIVE);
                                if (named != XKB_KEY_NoSymbol) {
                                        return WL_SEND_KEYSYM(named);
                                }
                        }

                        const unsigned char *p = (const unsigned char *)utf;
                        while (*p) {
                                size_t n = utf8_char_len(*p);
                                char buf[5] = {0};
                                for (size_t i = 0; i < n && p[i] != '\0' && i < sizeof(buf)-1; i++) {
                                        buf[i] = (char)p[i];
                                }

                                uint32_t cp = utf8_decode_one((const unsigned char *)buf, n);
                                xkb_keysym_t sym = xkb_utf32_to_keysym(cp);
                                if (sym == XKB_KEY_NoSymbol) {
                                        return ROBOTGO_KEY_UNMAPPED;
                                }
                                int status = WL_SEND_KEYSYM(sym);
                                if (status != ROBOTGO_KEY_OK) {
                                        return status;
                                }

                                p += n;
                                microsleep(DEADBEEF_UNIFORM(0.0, 0.5));
                        }

                        return ROBOTGO_KEY_OK;
                }
#endif
#if !defined(DISPLAY_SERVER_WAYLAND)
		(void)utf;
		return ROBOTGO_KEY_UNSUPPORTED;
#else
                return ROBOTGO_KEY_UNSUPPORTED;
#endif
        }
#else
        int input_utf(const char *utf){
		(void)utf;
		return ROBOTGO_KEY_OK;
        }
#endif
