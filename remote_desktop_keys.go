package robotgo

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	portalKeysymF1 = 0xffbe
)

var portalNamedKeysyms = map[string]int32{
	"backspace":         0xff08,
	"tab":               0xff09,
	"enter":             0xff0d,
	"esc":               0xff1b,
	"escape":            0xff1b,
	"home":              0xff50,
	"left":              0xff51,
	"up":                0xff52,
	"right":             0xff53,
	"down":              0xff54,
	"pageup":            0xff55,
	"pagedown":          0xff56,
	"end":               0xff57,
	"print":             0xff61,
	"printscreen":       0xff61,
	"insert":            0xff63,
	"menu":              0xff67,
	"num_lock":          0xff7f,
	"num_enter":         0xff8d,
	"num*":              0xffaa,
	"num+":              0xffab,
	"num-":              0xffad,
	"num.":              0xffae,
	"num/":              0xffaf,
	"num0":              0xffb0,
	"num1":              0xffb1,
	"num2":              0xffb2,
	"num3":              0xffb3,
	"num4":              0xffb4,
	"num5":              0xffb5,
	"num6":              0xffb6,
	"num7":              0xffb7,
	"num8":              0xffb8,
	"num9":              0xffb9,
	"num_equal":         0xffbd,
	"num_clear":         0xff0b,
	"shift":             0xffe1,
	"lshift":            0xffe1,
	"rshift":            0xffe2,
	"right_shift":       0xffe2,
	"ctrl":              0xffe3,
	"control":           0xffe3,
	"lctrl":             0xffe3,
	"rctrl":             0xffe4,
	"capslock":          0xffe5,
	"cmd":               0xffeb,
	"command":           0xffeb,
	"lcmd":              0xffeb,
	"rcmd":              0xffec,
	"alt":               0xffe9,
	"lalt":              0xffe9,
	"ralt":              0xffea,
	"space":             0x20,
	"delete":            0xffff,
	"scroll_lock":       0xff14,
	"pause_break":       0xff13,
	"audio_vol_down":    0x1008ff11,
	"audio_mute":        0x1008ff12,
	"audio_vol_up":      0x1008ff13,
	"audio_play":        0x1008ff14,
	"audio_stop":        0x1008ff15,
	"audio_prev":        0x1008ff16,
	"audio_next":        0x1008ff17,
	"audio_pause":       0x1008ff31,
	"audio_rewind":      0x1008ff3e,
	"audio_forward":     0x1008ff97,
	"audio_repeat":      0x1008ff98,
	"audio_random":      0x1008ff99,
	"lights_mon_up":     0x1008ff02,
	"lights_mon_down":   0x1008ff03,
	"lights_kbd_toggle": 0x1008ff04,
	"lights_kbd_down":   0x1008ff05,
	"lights_kbd_up":     0x1008ff06,
}

func portalKeysymForKey(key string) (int32, error) {
	if key == "" || !utf8.ValidString(key) {
		return 0, fmt.Errorf("%w: invalid portal keyboard key %q", ErrNotSupported, key)
	}
	key = strings.ToLower(key)
	if keysym, ok := portalNamedKeysyms[key]; ok {
		return keysym, nil
	}
	if strings.HasPrefix(key, "f") {
		n, err := strconv.Atoi(strings.TrimPrefix(key, "f"))
		if err == nil && n >= 1 && n <= 24 {
			return portalKeysymF1 + int32(n-1), nil
		}
	}
	if utf8.RuneCountInString(key) == 1 {
		value, _ := utf8.DecodeRuneInString(key)
		return portalKeysymForRune(value)
	}
	return 0, fmt.Errorf("%w: portal keyboard key %q", ErrNotSupported, key)
}

func normalizePortalKey(key string, modifiers []string) (string, []string) {
	if value, _ := utf8.DecodeRuneInString(key); value != utf8.RuneError && unicode.IsUpper(value) {
		key = strings.ToLower(key)
		modifiers = append(modifiers, "shift")
	}
	if special := CurrentSpecialTable(); special != nil {
		if base, ok := special[key]; ok {
			key = base
			modifiers = append(modifiers, "shift")
		}
	}
	return key, modifiers
}

func portalKeysymsPure(key string, modifiers []string) (int32, []int32, error) {
	key, modifiers = normalizePortalKey(key, append([]string(nil), modifiers...))
	mainKey, err := portalKeysymForKey(key)
	if err != nil {
		return 0, nil, err
	}
	result := make([]int32, 0, len(modifiers))
	seen := make(map[int32]struct{}, len(modifiers))
	for _, modifier := range modifiers {
		if modifier == "none" {
			continue
		}
		keysym, err := portalKeysymForKey(modifier)
		if err != nil {
			return 0, nil, err
		}
		if _, duplicate := seen[keysym]; duplicate {
			continue
		}
		seen[keysym] = struct{}{}
		result = append(result, keysym)
	}
	return mainKey, result, nil
}
