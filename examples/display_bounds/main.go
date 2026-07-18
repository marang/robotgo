package main

import (
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	desktop, err := robotgo.GetScreenRectE()
	if err != nil {
		fmt.Println("desktop bounds unavailable:", err)
		return
	}
	width, height, err := robotgo.GetScreenSizeE()
	if err != nil {
		fmt.Println("screen size unavailable:", err)
		return
	}
	fmt.Printf(
		"desktop: origin=(%d,%d) size=%dx%d (GetScreenSize=%dx%d)\n",
		desktop.X,
		desktop.Y,
		desktop.W,
		desktop.H,
		width,
		height,
	)

	count, err := robotgo.DisplaysNumE()
	if err != nil {
		fmt.Println("display enumeration unavailable:", err)
		return
	}
	fmt.Printf("outputs: %d, platform main ID: %d\n", count, robotgo.GetMainId())
	for displayID := 0; displayID < count; displayID++ {
		x, y, width, height, err := robotgo.GetDisplayBoundsE(displayID)
		if err != nil {
			fmt.Printf("output[%d] unavailable: %v\n", displayID, err)
			continue
		}
		fmt.Printf(
			"output[%d]: origin=(%d,%d) size=%dx%d\n",
			displayID,
			x,
			y,
			width,
			height,
		)
	}
}
