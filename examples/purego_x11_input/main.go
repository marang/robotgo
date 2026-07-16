package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marang/robotgo"
)

const (
	maximumDemoTextRunes = 256
	abortDelay           = 3 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	act := flag.Bool("act", false, "explicitly allow the requested global desktop actions")
	move := flag.String("move", "", "move the pointer to x,y (requires -act)")
	key := flag.String("key", "", "tap one key in the focused window (requires -act)")
	text := flag.String("text", "", "type up to 256 runes in the focused window (requires -act)")
	settle := flag.Duration("settle", 2*time.Second, "keep X11 scratch mappings alive after keyboard input; increase for delayed targets")
	flag.Parse()
	abortContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()

	keyboardInputStarted := false
	info := robotgo.GetRuntimeBackendInfo()
	capabilities := robotgo.GetRuntimeCapabilities()
	defer func() {
		if keyboardInputStarted {
			fmt.Fprintf(os.Stderr,
				"Keeping X11 keyboard mappings available for %s before verified cleanup; increase -settle for delayed target clients.\n",
				*settle,
			)
			time.Sleep(*settle)
		}
		runErr = errors.Join(runErr, robotgo.CloseMainDisplayE())
	}()
	printCapability("keyboard", capabilities.Keyboard)
	printCapability("mouse", capabilities.Mouse)
	fmt.Printf("runtime: implementation=%s cgo=%t platform=%s/%s display=%s\n",
		info.BuildImplementation, info.CGOEnabled, info.GOOS, info.GOARCH, info.DisplayServer)

	requested := *move != "" || *key != "" || *text != ""
	if !requested {
		fmt.Println("inspection only: no desktop action requested")
		return nil
	}
	if !*act {
		return fmt.Errorf("no action performed: add -act after reviewing the requested global input")
	}
	if (*key != "" || *text != "") && *settle <= 0 {
		return fmt.Errorf("-settle must be positive when keyboard input is requested")
	}
	if err := validateRuntime(info, capabilities, *move, *key, *text); err != nil {
		return fmt.Errorf("cannot run input demo: %w", err)
	}
	if *move != "" {
		if err := robotgo.MouseReady(); err != nil {
			return fmt.Errorf("pointer backend is not ready: %w", err)
		}
	}
	if *key != "" || *text != "" {
		if err := robotgo.KeyboardReady(); err != nil {
			return fmt.Errorf("keyboard backend is not ready: %w", err)
		}
	}

	fmt.Fprintln(os.Stderr, "WARNING: XTEST input is global; key/text input goes to the currently focused window.")
	fmt.Fprintf(os.Stderr, "Press Ctrl+C within %s to abort.\n", abortDelay)
	abortTimer := time.NewTimer(abortDelay)
	defer abortTimer.Stop()
	select {
	case <-abortTimer.C:
	case <-abortContext.Done():
		return errors.New("input demo aborted before global action")
	}

	if *move != "" {
		if abortContext.Err() != nil {
			return errors.New("input demo aborted before pointer action")
		}
		x, y, err := parsePoint(*move)
		if err != nil {
			return err
		}
		if err := robotgo.MoveE(x, y); err != nil {
			return fmt.Errorf("move pointer: %w", err)
		}
	}
	if *key != "" {
		if abortContext.Err() != nil {
			return errors.New("input demo aborted before keyboard action")
		}
		keyboardInputStarted = true
		if err := robotgo.KeyTap(*key); err != nil {
			return fmt.Errorf("tap key: %w", err)
		}
	}
	if *text != "" {
		if abortContext.Err() != nil {
			return errors.New("input demo aborted before text action")
		}
		keyboardInputStarted = true
		if err := robotgo.TypeStrE(*text); err != nil {
			return fmt.Errorf("type text: %w", err)
		}
	}
	return nil
}

func validateRuntime(info robotgo.RuntimeBackendInfo, capabilities robotgo.RuntimeCapabilities, move, key, text string) error {
	if runtime.GOOS != "linux" || info.GOOS != "linux" {
		return fmt.Errorf("Pure-Go X11 input requires Linux")
	}
	if info.CGOEnabled {
		return fmt.Errorf("this example targets a CGO_ENABLED=0 build")
	}
	if info.DisplayServer != robotgo.DisplayServerX11 {
		return fmt.Errorf("an X11-primary session is required; got %q", info.DisplayServer)
	}
	if move != "" && !capabilities.Mouse.Available {
		return fmt.Errorf("mouse backend %q is unavailable: %s", capabilities.Mouse.Backend, capabilities.Mouse.Reason)
	}
	if key != "" || text != "" {
		if !capabilities.Keyboard.Available {
			return fmt.Errorf("keyboard backend %q is unavailable: %s", capabilities.Keyboard.Backend, capabilities.Keyboard.Reason)
		}
	}
	if text != "" && utf8.RuneCountInString(text) > maximumDemoTextRunes {
		return fmt.Errorf("text exceeds the %d-rune demo limit", maximumDemoTextRunes)
	}
	return nil
}

func printCapability(name string, capability robotgo.FeatureCapability) {
	fmt.Printf("%s: available=%t backend=%s reason=%q notes=%q\n",
		name, capability.Available, capability.Backend, capability.Reason, capability.Notes)
}

func parsePoint(value string) (int, int, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("move must be formatted as x,y")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse move x coordinate: %w", err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse move y coordinate: %w", err)
	}
	return x, y, nil
}
