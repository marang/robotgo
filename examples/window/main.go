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

func alert() {
	// show Alert Window
	abool := robotgo.Alert("hello", "robotgo")
	if abool {
		fmt.Println("ok@@@", "ok")
	}
	robotgo.Alert("hello", "robotgo", "Ok", "Cancel")
}

func get() {
	// get the current process id
	pid := robotgo.GetPid()
	fmt.Println("pid----", pid)

	// get current Window Active
	mdata := robotgo.GetActive()

	// get current Window Handle
	hwnd := robotgo.GetHandle()
	fmt.Println("hwnd---", hwnd)

	// get current Window title
	title := robotgo.GetTitle()
	fmt.Println("title-----", title)

	// set Window Active
	robotgo.SetActive(mdata)
}

func findIds() {
	// find the process id by the process name
	fpid, err := robotgo.FindIds("Google")
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(fpid) > 0 {
		if err := robotgo.KeyTap("a", fpid[0]); err != nil {
			fmt.Println(err)
		}
		robotgo.TypeStr("Hi galaxy!", fpid[0])

		if err := robotgo.KeyToggle("a", fpid[0], "cmd"); err != nil {
			fmt.Println(err)
		}
		if err := robotgo.KeyToggle("a", fpid[0], "cmd", "up"); err != nil {
			fmt.Println(err)
		}
	}

	fmt.Println("pids...", fpid)
	if len(fpid) > 0 {
		err = robotgo.ActivePid(fpid[0])
		if err != nil {
			fmt.Println(err)
		}

		tl := robotgo.GetTitle(fpid[0])
		fmt.Println("pid[0] title is: ", tl)

		x, y, w, h := robotgo.GetBounds(fpid[0])
		fmt.Println("GetBounds is: ", x, y, w, h)

		// Windows
		// hwnd := robotgo.FindWindow("google")
		// hwnd := robotgo.GetHWND()
		robotgo.MinWindow(fpid[0])
		robotgo.MaxWindow(fpid[0])
		robotgo.CloseWindow(fpid[0])

		if err := robotgo.Kill(fpid[0]); err != nil {
			fmt.Println(err)
		}
	}
}

func active() {
	if err := robotgo.ActivePid(100); err != nil {
		fmt.Println(err)
	}
	// robotgo.Sleep(2)
	if err := robotgo.ActiveName("code"); err != nil {
		fmt.Println(err)
	}
	robotgo.Sleep(1)
	if err := robotgo.ActiveName("chrome"); err != nil {
		fmt.Println(err)
	}
}

func findName() {
	// find the process name by the process id
	name, err := robotgo.FindName(100)
	if err == nil {
		fmt.Println("name: ", name)
	}

	// find the all process name
	names, err := robotgo.FindNames()
	if err == nil {
		fmt.Println("name: ", names)
	}

	p, err := robotgo.FindPath(100)
	if err == nil {
		fmt.Println("path: ", p)
	}
}

func ps() {
	// determine whether the process exists
	isExist, err := robotgo.PidExists(100)
	if err == nil && isExist {
		fmt.Println("pid exists is", isExist)

		if err := robotgo.Kill(100); err != nil {
			fmt.Println(err)
		}
	}

	// get the all process id
	pids, err := robotgo.Pids()
	if err == nil {
		fmt.Println("pids: ", pids)
	}

	// get the all process struct
	ps, err := robotgo.Process()
	if err == nil {
		fmt.Println("process: ", ps)
	}
}

func window() {
	////////////////////////////////////////////////////////////////////////////////
	// Window Handle
	////////////////////////////////////////////////////////////////////////////////

	alert()
	//
	get()

	findIds()
	active()

	findName()
	//
	ps()

	// close current Window
	robotgo.CloseWindow()
}

func main() {
	window()
}
