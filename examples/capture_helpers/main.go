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
	x := flag.Int("x", 0, "capture/search region x coordinate")
	y := flag.Int("y", 0, "capture/search region y coordinate")
	width := flag.Int("width", 100, "capture/search region width")
	height := flag.Int("height", 100, "capture/search region height")
	colorHex := flag.String("color", "", "optional RGB color to find, for example 33ccff")
	tolerance := flag.Float64("tolerance", 0.01, "color tolerance from 0 through 1")
	flag.Parse()

	serialized, err := robotgo.CaptureBitmapStr(*x, *y, *width, *height)
	if err != nil {
		fmt.Fprintln(os.Stderr, "capture bitmap string:", err)
		os.Exit(1)
	}
	bitmap, err := robotgo.BitmapFromStr(serialized)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode bitmap string:", err)
		os.Exit(1)
	}
	fmt.Printf(
		"backend=%s bitmap=%dx%d serialized-bytes=%d\n",
		robotgo.LastBackend(),
		bitmap.Width,
		bitmap.Height,
		len(serialized),
	)

	matchX, matchY, err := robotgo.FindBitmapStr(serialized, serialized)
	if err != nil {
		fmt.Fprintln(os.Stderr, "find bitmap string:", err)
		os.Exit(1)
	}
	fmt.Printf("serialized bitmap round-trip match=(%d,%d)\n", matchX, matchY)

	if strings.TrimSpace(*colorHex) == "" {
		return
	}
	rawColor := strings.TrimPrefix(strings.TrimSpace(*colorHex), "#")
	if len(rawColor) >= 2 && strings.EqualFold(rawColor[:2], "0x") {
		rawColor = rawColor[2:]
	}
	value, err := strconv.ParseUint(rawColor, 16, 24)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse -color:", err)
		os.Exit(2)
	}
	matchX, matchY, err = robotgo.FindColorCS(
		*x,
		*y,
		*width,
		*height,
		robotgo.CHex(value),
		*tolerance,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "find color:", err)
		os.Exit(1)
	}
	if matchX < 0 {
		fmt.Println("color not found")
		return
	}
	fmt.Printf("color found at absolute screen coordinate (%d,%d)\n", matchX, matchY)
}
