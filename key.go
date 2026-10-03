//go:build cgo

// Copyright 2016 The marang Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package robotgo

// KeyTap taps the keyboard code;
//
// See keys supported:
//
//	https://github.com/marang/robotgo/blob/master/docs/keys.md#keys
//
// Examples:
//
//	robotgo.KeySleep = 100 // 100 millisecond
//	robotgo.KeyTap("a")
//	robotgo.KeyTap("i", "alt", "command")
//
//	arr := []string{"alt", "command"}
//	robotgo.KeyTap("i", arr)
//
//	robotgo.KeyTap("k", pid int)
func KeyTap(key string, args ...interface{}) error {
	key, args = appendShift(key, args...)
	pid, _, keyArr, err := parseKeyArguments(args, false)
	if err != nil {
		return err
	}
	keyArr, err = normalizeKeyModifiers(keyArr)
	if err != nil {
		return err
	}
	if err := validateKeyArgument(key); err != nil {
		return err
	}
	return keyTaps(key, keyArr, pid)
}

// KeyToggle toggles the keyboard, if there not have args default is "down"
//
// See keys:
//
//	https://github.com/marang/robotgo/blob/master/docs/keys.md#keys
//
// Examples:
//
//	robotgo.KeyToggle("a")
//	robotgo.KeyToggle("a", "up")
//
//	robotgo.KeyToggle("a", "up", "alt", "cmd")
//	robotgo.KeyToggle("k", pid int)
func KeyToggle(key string, args ...interface{}) error {
	return keyToggle(key, true, args...)
}

// KeyToggleImmediate changes key state without applying the configured
// post-event delay. It is intended for callers that own a bounded hold.
func KeyToggleImmediate(key string, args ...interface{}) error {
	return keyToggle(key, false, args...)
}

// KeyPress presses and releases a key as one backend transaction. It is
// equivalent to KeyTap.
func KeyPress(key string, args ...interface{}) error {
	return KeyTap(key, args...)
}

// KeyDown press down a key
func KeyDown(key string, args ...interface{}) error {
	return KeyToggle(key, args...)
}

// KeyUp press up a key
func KeyUp(key string, args ...interface{}) error {
	arr := []interface{}{"up"}
	arr = append(arr, args...)
	return KeyToggle(key, arr...)
}

// UnicodeType tap the uint32 unicode
func UnicodeType(str uint32, args ...int) {
	_ = UnicodeTypeE(str, args...)
}

// TypeStr send a string (supported UTF-8)
//
// robotgo.TypeStr(string: "The string to send", int: pid, "milli_sleep time", "x11 option")
//
// Examples:
//
//	robotgo.TypeStr("abc@123, Hi galaxy, こんにちは")
//	robotgo.TypeStr("To be or not to be, this is questions.", pid int)
func TypeStr(str string, args ...int) {
	_ = TypeStrE(str, args...)
}

// TypeStrE sends a UTF-8 string and reports backend or key injection errors.
func TypeStrE(str string, args ...int) error {
	return typeStrE(str, true, args...)
}

// TypeStrImmediateE sends a UTF-8 string without applying the configured
// post-input delay. Explicit per-rune delay arguments remain honored.
func TypeStrImmediateE(str string, args ...int) error {
	return typeStrE(str, false, args...)
}

// TypeStrDelay type string with delayed
// And you can use robotgo.KeySleep = 100 to delayed not this function
func TypeStrDelay(str string, delay int) {
	TypeStr(str)
	MilliSleep(delay)
}

// SetDelay sets the key and mouse delay
// robotgo.SetDelay(100) option the robotgo.KeySleep and robotgo.MouseSleep = d
func SetDelay(d ...int) {
	v := 10
	if len(d) > 0 {
		v = d[0]
	}

	_ = setInputDelays(v, v)
}
