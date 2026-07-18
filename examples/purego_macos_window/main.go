package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"runtime"

	"github.com/marang/robotgo"
)

func main() {
	pid := flag.Int("pid", 0, "inspect a window owned by this process ID")
	handle := flag.Uint64("handle", 0, "inspect an explicit CGWindowID")
	action := flag.String(
		"action",
		"",
		"explicit mutation: activate, minimize, restore, or close",
	)
	flag.Parse()

	if runtime.GOOS != "darwin" {
		log.Fatal("this example requires macOS")
	}
	info := robotgo.GetRuntimeBackendInfo()
	if info.BuildImplementation != robotgo.RuntimeImplementationPureGo {
		log.Fatal("run this example with CGO_ENABLED=0")
	}
	capability := robotgo.GetRuntimeCapabilities().Window
	fmt.Printf(
		"window backend=%s available=%v reason=%s\n",
		capability.Backend,
		capability.Available,
		capability.Reason,
	)
	if !capability.Available {
		log.Fatal("Pure-Go macOS window control is unavailable; grant Accessibility access if requested")
	}
	defer func() {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			log.Printf("window backend cleanup: %v", err)
		}
	}()

	target, isHandle, resolved, err := resolveTarget(*pid, *handle)
	if err != nil {
		log.Fatal(err)
	}
	args := []int(nil)
	if isHandle {
		args = []int{1}
	}
	title, err := robotgo.GetTitleE(append([]int{target}, args...)...)
	if err != nil {
		log.Fatal(err)
	}
	x, y, width, height := robotgo.GetBounds(target, args...)
	if width <= 0 || height <= 0 {
		log.Fatalf(
			"window %#x returned invalid bounds (%d, %d, %d, %d)",
			resolved,
			x,
			y,
			width,
			height,
		)
	}
	fmt.Printf(
		"window=%#x title=%q bounds=(%d,%d %dx%d)\n",
		resolved,
		title,
		x,
		y,
		width,
		height,
	)

	if *action == "" {
		fmt.Println("inspection only; pass -action to request an explicit mutation")
		return
	}
	if err := performAction(target, isHandle, *action); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s requested for window %#x\n", *action, resolved)
}

func resolveTarget(pid int, rawHandle uint64) (target int, isHandle bool, resolved int, err error) {
	if pid < 0 {
		return 0, false, 0, errors.New("-pid must not be negative")
	}
	if pid > 0 && rawHandle != 0 {
		return 0, false, 0, errors.New("-pid and -handle are mutually exclusive")
	}
	const maximumCGWindowID = uint64(1<<32 - 1)
	if rawHandle > maximumCGWindowID || rawHandle > uint64(^uint(0)>>1) {
		return 0, false, 0, fmt.Errorf("CGWindowID %#x is outside the supported range", rawHandle)
	}
	if rawHandle != 0 {
		target = int(rawHandle)
		return target, true, target, nil
	}
	if pid > 0 {
		resolved = robotgo.GetHWNDByPid(pid)
		if resolved == 0 {
			return 0, false, 0, fmt.Errorf("no accessible window found for pid %d", pid)
		}
		return resolved, true, resolved, nil
	}
	resolved = robotgo.GetHandle()
	if resolved == 0 {
		return 0, false, 0, errors.New("no accessible active window found")
	}
	return resolved, true, resolved, nil
}

func performAction(target int, isHandle bool, action string) error {
	intArgs := []int(nil)
	stateArgs := []interface{}{true}
	if isHandle {
		intArgs = []int{1}
		stateArgs = append(stateArgs, 1)
	}
	switch action {
	case "activate":
		return robotgo.ActivePid(target, intArgs...)
	case "minimize":
		return robotgo.MinWindowE(target, stateArgs...)
	case "restore":
		stateArgs[0] = false
		return robotgo.MinWindowE(target, stateArgs...)
	case "close":
		return robotgo.CloseWindowE(append([]int{target}, intArgs...)...)
	default:
		return fmt.Errorf(
			"unknown -action %q; use activate, minimize, restore, or close",
			action,
		)
	}
}
