// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package main

import (
	"fmt"

	"github.com/marang/robotgo"
	// "marang/robotgo"
)

func typeStr() {
	// typing "Hello World"
	robotgo.TypeStr("Hello World!", 0, 1)
	robotgo.KeySleep = 100
	robotgo.TypeStr("だんしゃり")

	robotgo.TypeStr("Hi galaxy, hi stars, hi MT.Rainier, hi sea. こんにちは世界.")
	robotgo.TypeStr("So, hi, bye! 你好, 再见!")
	robotgo.Sleep(1)

	robotgo.TypeStr("Hi, Seattle space needle, Golden gate bridge, One world trade center.")
	robotgo.MilliSleep(100)

	ustr := uint32(robotgo.CharCodeAt("So, hi, bye!", 0))
	robotgo.UnicodeType(ustr)

	err := robotgo.PasteStr("paste string")
	fmt.Println("PasteStr: ", err)
}

func keyTap() {
	// press "enter"
	if err := robotgo.KeyTap("enter"); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyTap(robotgo.Enter); err != nil {
		fmt.Println(err)
	}
	robotgo.KeySleep = 200
	if err := robotgo.KeyTap("a"); err != nil {
		fmt.Println(err)
	}
	robotgo.MilliSleep(100)
	if err := robotgo.KeyTap("a", "ctrl"); err != nil {
		fmt.Println(err)
	}

	// hide window
	err := robotgo.KeyTap("h", "cmd")
	if err != nil {
		fmt.Println("robotgo.KeyTap run error is: ", err)
	}

	if err := robotgo.KeyTap("h", "cmd"); err != nil {
		fmt.Println(err)
	}

	// press "i", "alt", "command" Key combination
	if err := robotgo.KeyTap(robotgo.KeyI, robotgo.Alt, robotgo.Cmd); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyTap("i", "alt", "cmd"); err != nil {
		fmt.Println(err)
	}

	arr := []string{"alt", "cmd"}
	if err := robotgo.KeyTap("i", arr); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyTap("i", arr); err != nil {
		fmt.Println(err)
	}

	if err := robotgo.KeyTap("i", "cmd", " alt", "shift"); err != nil {
		fmt.Println(err)
	}

	// close window
	if err := robotgo.KeyTap("w", "cmd"); err != nil {
		fmt.Println(err)
	}

	// minimize window
	if err := robotgo.KeyTap("m", "cmd"); err != nil {
		fmt.Println(err)
	}

	if err := robotgo.KeyTap("f1", "ctrl"); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyTap("a", "control"); err != nil {
		fmt.Println(err)
	}
}

func special() {
	robotgo.TypeStr("{}")
	if err := robotgo.KeyTap("[", "]"); err != nil {
		fmt.Println(err)
	}

	if err := robotgo.KeyToggle("("); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyToggle("(", "up"); err != nil {
		fmt.Println(err)
	}
}

func keyToggle() {
	// robotgo.KeySleep = 150
	if err := robotgo.KeyToggle(robotgo.KeyA); err != nil {
		fmt.Println(err)
	}
	if err := robotgo.KeyToggle("a", "down", "alt"); err != nil {
		fmt.Println(err)
	}
	robotgo.Sleep(1)

	if err := robotgo.KeyToggle("a", "up", "alt", "cmd"); err != nil {
		fmt.Println(err)
	}
	robotgo.MilliSleep(100)
	if err := robotgo.KeyToggle("q", "up", "alt", "cmd", "shift"); err != nil {
		fmt.Println(err)
	}

	err := robotgo.KeyToggle(robotgo.Enter)
	if err != nil {
		fmt.Println("robotgo.KeyToggle run error is: ", err)
	}
}

func cilp() {
	// robotgo.TypeStr("en")

	// write string to clipboard
	e := robotgo.WriteAll("テストする")
	if e != nil {
		fmt.Println("robotgo.WriteAll err is: ", e)
	}

	// read string from clipboard
	text, err := robotgo.ReadAll()
	if err != nil {
		fmt.Println("robotgo.ReadAll err is: ", err)
	}
	fmt.Println("text: ", text)
}

func key() {
	////////////////////////////////////////////////////////////////////////////////
	// Control the keyboard
	////////////////////////////////////////////////////////////////////////////////

	typeStr()
	special()

	keyTap()
	keyToggle()

	cilp()
}

func main() {
	key()
}
