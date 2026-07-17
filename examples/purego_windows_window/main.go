package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/marang/robotgo"
)

func main() {
	pid := flag.Int("pid", 0, "inspect a process window by PID")
	handle := flag.Int("handle", 0, "inspect a window by HWND")
	activate := flag.Bool("activate", false, "activate the selected window")
	minimize := flag.Bool("minimize", false, "minimize, then restore, the selected window")
	flag.Parse()

	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "this example requires Windows")
		os.Exit(2)
	}
	capability := robotgo.GetRuntimeCapabilities().Window
	fmt.Printf("window backend: available=%v backend=%s reason=%s\n",
		capability.Available, capability.Backend, capability.Reason)
	if !capability.Available {
		os.Exit(1)
	}

	target, isHandle := selectedTarget(*pid, *handle)
	if target == 0 {
		target = robotgo.GetHandle()
		isHandle = true
	}
	if target == 0 {
		fmt.Fprintln(os.Stderr, "no active window is available")
		os.Exit(1)
	}

	args := []int(nil)
	if isHandle {
		args = []int{1}
	}
	titleArgs := append([]int{target}, args...)
	title, err := robotgo.GetTitleE(titleArgs...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "title:", err)
		os.Exit(1)
	}
	x, y, width, height := robotgo.GetBounds(target, args...)
	fmt.Printf("target=%d handle=%v title=%q bounds=(%d,%d %dx%d)\n",
		target, isHandle, title, x, y, width, height)

	resolved := robotgo.GetHandByPid(target, args...)
	if *activate {
		if err := robotgo.SetActiveE(resolved); err != nil {
			fmt.Fprintln(os.Stderr, "activate:", err)
			os.Exit(1)
		}
	}
	if *minimize {
		stateArgs := []interface{}{true}
		if isHandle {
			stateArgs = append(stateArgs, 1)
		}
		if err := robotgo.MinWindowE(target, stateArgs...); err != nil {
			fmt.Fprintln(os.Stderr, "minimize:", err)
			os.Exit(1)
		}
		time.Sleep(time.Second)
		stateArgs[0] = false
		if err := robotgo.MinWindowE(target, stateArgs...); err != nil {
			fmt.Fprintln(os.Stderr, "restore:", err)
			os.Exit(1)
		}
	}
}

func selectedTarget(pid, handle int) (target int, isHandle bool) {
	if pid > 0 && handle > 0 {
		fmt.Fprintln(os.Stderr, "use either -pid or -handle, not both")
		os.Exit(2)
	}
	if handle > 0 {
		return handle, true
	}
	return pid, false
}
