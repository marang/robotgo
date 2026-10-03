//go:build cgo

// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

/*
Package robotgo Go native cross-platform system automation.

Please make sure Golang, GCC is installed correctly before installing RobotGo;

See Requirements:

	https://github.com/marang/robotgo#requirements

Installation:

With Go module support (Go 1.11+), just import:

	import "github.com/marang/robotgo"

Otherwise, to install the robotgo package, run the command:

	go get -u github.com/marang/robotgo
*/
package robotgo

// GetVersion get the robotgo version
func GetVersion() string {
	return Version
}

// Move move the mouse to (x, y)
//
// Examples:
//
//	robotgo.MouseSleep = 100  // 100 millisecond
//	robotgo.Move(10, 10)
func Move(x, y int, displayId ...int) {
	_ = MoveE(x, y, displayId...)
}

// MoveE moves the mouse to (x, y) and reports backend availability errors.
// Prefer it over Move when the caller must know whether injection succeeded.
func MoveE(x, y int, displayId ...int) error {
	return moveE(x, y, true, displayId...)
}

// MoveImmediateE moves the mouse without applying the configured post-move
// delay. It is intended for callers that provide their own bounded schedule.
func MoveImmediateE(x, y int, displayId ...int) error {
	return moveE(x, y, false, displayId...)
}

// MoveRelative move mouse with relative
func MoveRelative(x, y int) {
	_ = MoveRelativeE(x, y)
}

// Location get the mouse location position return x, y
func Location() (int, int) {
	x, y, _ := LocationE()
	return x, y
}

// Click click the mouse button
//
// robotgo.Click(button string, double bool)
//
// Examples:
//
//	robotgo.Click() // default is left button
//	robotgo.Click("right")
//	robotgo.Click("wheelLeft")
func Click(args ...interface{}) {
	_ = ClickE(args...)
}

// ClickE clicks a mouse button and reports backend availability errors.
func ClickE(args ...interface{}) error {
	return clickE(true, args...)
}

// ClickImmediateE clicks without applying the configured post-click delay.
// It is intended for callers that provide bounded scheduling.
func ClickImmediateE(args ...interface{}) error {
	return clickE(false, args...)
}

// MouseDown send mouse down event
func MouseDown(key ...interface{}) error {
	return Toggle(key...)
}

// MouseUp send mouse up event
func MouseUp(key ...interface{}) error {
	if len(key) <= 0 {
		key = append(key, "left")
	}
	return Toggle(append(key, "up")...)
}

// Scroll scroll the mouse to (x, y)
//
// robotgo.Scroll(x, y, msDelay int)
//
// Examples:
//
//	robotgo.Scroll(10, 10)
func Scroll(x, y int, args ...int) {
	_ = ScrollE(x, y, args...)
}

// ScrollE scrolls the mouse and reports backend availability errors.
func ScrollE(x, y int, args ...int) error {
	return scrollE(x, y, true, args...)
}

// ScrollImmediateE scrolls without applying a configured or per-call
// post-event delay. It is intended for callers that provide bounded scheduling.
func ScrollImmediateE(x, y int) error {
	return scrollE(x, y, false)
}
