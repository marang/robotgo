//go:build windows

// Command purego_windows_input demonstrates the non-CGO Windows input backend.
//
// Run a readiness-only check without injecting input:
//
//	CGO_ENABLED=0 go run ./examples/purego_windows_input
//
// Opt in to visible input with -move, -text, or -paste. Use -color to inspect
// the pixel under the pointer:
//
//	CGO_ENABLED=0 go run ./examples/purego_windows_input -move 400,300 -color -paste "Hello"
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/marang/robotgo"
)

func main() {
	move := flag.String("move", "", "optional absolute pointer target as x,y")
	text := flag.String("text", "", "optional text to type into the active application")
	paste := flag.String("paste", "", "optional text to copy and paste into the active application")
	color := flag.Bool("color", false, "print the RGB color under the current pointer")
	flag.Parse()

	info := robotgo.GetRuntimeBackendInfo()
	fmt.Printf("implementation=%s os=%s arch=%s\n", info.BuildImplementation, info.GOOS, info.GOARCH)
	if info.BuildImplementation != robotgo.RuntimeImplementationPureGo {
		fmt.Fprintln(os.Stderr, "re-run with CGO_ENABLED=0 to exercise the Pure-Go backend")
		os.Exit(2)
	}
	if err := robotgo.KeyboardReady(); err != nil {
		fmt.Fprintln(os.Stderr, "keyboard:", err)
		os.Exit(1)
	}
	if err := robotgo.MouseReady(); err != nil {
		fmt.Fprintln(os.Stderr, "mouse:", err)
		os.Exit(1)
	}
	fmt.Println("Pure-Go Windows keyboard and mouse backends are ready")

	if *move != "" {
		x, y, err := coordinates(*move)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := robotgo.MoveE(x, y); err != nil {
			fmt.Fprintln(os.Stderr, "move:", err)
			os.Exit(1)
		}
	}
	if *text != "" {
		if err := robotgo.TypeStrE(*text); err != nil {
			fmt.Fprintln(os.Stderr, "text:", err)
			os.Exit(1)
		}
	}
	if *paste != "" {
		if err := robotgo.PasteStr(*paste); err != nil {
			fmt.Fprintln(os.Stderr, "paste:", err)
			os.Exit(1)
		}
	}
	if *color {
		value, err := robotgo.GetLocationColor()
		if err != nil {
			fmt.Fprintln(os.Stderr, "pointer color:", err)
			os.Exit(1)
		}
		fmt.Println("pointer RGB:", value)
	}
}

func coordinates(value string) (int, int, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("move must be x,y, got %q", value)
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid x coordinate: %w", err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid y coordinate: %w", err)
	}
	return x, y, nil
}
