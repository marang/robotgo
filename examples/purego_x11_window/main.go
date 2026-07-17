package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/marang/robotgo"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pid := flag.Int("pid", 0, "inspect a top-level X11 window by PID")
	handle := flag.Int("handle", 0, "inspect a window by XID")
	act := flag.Bool("act", false, "explicitly allow requested window-state changes")
	activate := flag.Bool("activate", false, "ask the window manager to activate the target (requires -act)")
	minimize := flag.Bool("minimize", false, "minimize and restore the target (requires -act)")
	maximize := flag.Bool("maximize", false, "maximize and restore the target (requires -act)")
	topmost := flag.Bool("topmost", false, "enable and restore ABOVE state (requires -act)")
	settle := flag.Duration("settle", time.Second, "time to show each temporary state")
	flag.Parse()

	info := robotgo.GetRuntimeBackendInfo()
	capability := robotgo.GetRuntimeCapabilities().Window
	fmt.Printf(
		"window backend: available=%t backend=%s reason=%q notes=%q\n",
		capability.Available,
		capability.Backend,
		capability.Reason,
		capability.Notes,
	)
	if runtime.GOOS != "linux" || info.CGOEnabled ||
		info.DisplayServer != robotgo.DisplayServerX11 {
		return fmt.Errorf(
			"this example requires CGO_ENABLED=0 in an X11-primary Linux session; got %s cgo=%t display=%q",
			runtime.GOOS,
			info.CGOEnabled,
			info.DisplayServer,
		)
	}
	if !capability.Available {
		return fmt.Errorf("Pure-Go X11 window backend is unavailable: %s", capability.Reason)
	}
	if *settle < 0 {
		return errors.New("-settle must not be negative")
	}

	target, isHandle, err := selectedTarget(*pid, *handle)
	if err != nil {
		return err
	}
	if target == 0 {
		target = robotgo.GetHandle()
		isHandle = true
	}
	if target == 0 {
		return errors.New("no active X11 client window is available")
	}
	args := []int(nil)
	if isHandle {
		args = []int{1}
	}
	title, err := robotgo.GetTitleE(append([]int{target}, args...)...)
	if err != nil {
		return fmt.Errorf("inspect title: %w", err)
	}
	x, y, width, height := robotgo.GetBounds(target, args...)
	clientX, clientY, clientWidth, clientHeight := robotgo.GetClient(target, args...)
	fmt.Printf(
		"target=%d handle=%t title=%q bounds=(%d,%d %dx%d) client=(%d,%d %dx%d)\n",
		target,
		isHandle,
		title,
		x,
		y,
		width,
		height,
		clientX,
		clientY,
		clientWidth,
		clientHeight,
	)
	resolved := target
	if !isHandle {
		resolved = robotgo.GetHWNDByPid(target)
	}
	if resolved == 0 {
		return errors.New("the selected target could not be resolved to an XID")
	}
	if robotgo.GetHandle() == resolved {
		minimizedState, minimizedErr := robotgo.IsMinimizedE()
		printState("minimized", minimizedState, minimizedErr)
		maximizedState, maximizedErr := robotgo.IsMaximizedE()
		printState("maximized", maximizedState, maximizedErr)
		topmostState, topmostErr := robotgo.IsTopMostE()
		printState("topmost", topmostState, topmostErr)
	} else {
		fmt.Println("state: not queried (state APIs currently describe the active window)")
	}

	requested := *activate || *minimize || *maximize || *topmost
	if !requested {
		fmt.Println("inspection only: no window-manager request was sent")
		return nil
	}
	if !*act {
		return errors.New("no action performed: add -act after reviewing the requested window changes")
	}
	if *activate || *topmost {
		if err := ensureActive(target, isHandle, resolved); err != nil {
			return fmt.Errorf("activate target: %w", err)
		}
	}
	if *minimize {
		if err := exerciseState(target, isHandle, *settle, robotgo.MinWindowE); err != nil {
			return fmt.Errorf("exercise minimized state: %w", err)
		}
	}
	if *maximize {
		if err := exerciseState(target, isHandle, *settle, robotgo.MaxWindowE); err != nil {
			return fmt.Errorf("exercise maximized state: %w", err)
		}
	}
	if *topmost {
		original, err := robotgo.IsTopMostE()
		if err != nil {
			return fmt.Errorf("query initial topmost state: %w", err)
		}
		if err := robotgo.SetTopMostE(true); err != nil {
			return fmt.Errorf("enable topmost state: %w", err)
		}
		time.Sleep(*settle)
		if err := ensureActive(target, isHandle, resolved); err != nil {
			return fmt.Errorf("reactivate target before restoring topmost state: %w", err)
		}
		if err := robotgo.SetTopMostE(original); err != nil {
			return fmt.Errorf("restore topmost state: %w", err)
		}
	}
	return nil
}

func ensureActive(target int, isHandle bool, resolved int) error {
	if robotgo.GetHandle() == resolved {
		return nil
	}
	args := []int(nil)
	if isHandle {
		args = []int{1}
	}
	if err := robotgo.ActivePid(target, args...); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if robotgo.GetHandle() == resolved {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("window manager did not confirm activation within two seconds")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func selectedTarget(pid, handle int) (target int, isHandle bool, err error) {
	if pid < 0 || handle < 0 {
		return 0, false, errors.New("-pid and -handle must not be negative")
	}
	if pid > 0 && handle > 0 {
		return 0, false, errors.New("use either -pid or -handle, not both")
	}
	if handle > 0 {
		return handle, true, nil
	}
	return pid, false, nil
}

func printState(name string, value bool, err error) {
	if err != nil {
		fmt.Printf("%s: unavailable (%v)\n", name, err)
		return
	}
	fmt.Printf("%s: %t\n", name, value)
}

func exerciseState(
	target int,
	isHandle bool,
	settle time.Duration,
	change func(int, ...interface{}) error,
) error {
	args := []interface{}{true}
	if isHandle {
		args = append(args, 1)
	}
	if err := change(target, args...); err != nil {
		return err
	}
	time.Sleep(settle)
	args[0] = false
	return change(target, args...)
}
