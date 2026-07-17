package main

import (
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	desktop := robotgo.GetScreenRect()
	width, height := robotgo.GetScreenSize()
	fmt.Printf(
		"desktop: origin=(%d,%d) size=%dx%d (GetScreenSize=%dx%d)\n",
		desktop.X,
		desktop.Y,
		desktop.W,
		desktop.H,
		width,
		height,
	)

	count := robotgo.DisplaysNum()
	mainID := robotgo.GetMainId()
	fmt.Printf("outputs: %d, primary index: %d\n", count, mainID)
	for displayID := 0; displayID < count; displayID++ {
		rect := robotgo.GetScreenRect(displayID)
		fmt.Printf(
			"output[%d]: origin=(%d,%d) size=%dx%d primary=%t\n",
			displayID,
			rect.X,
			rect.Y,
			rect.W,
			rect.H,
			displayID == mainID,
		)
	}
}
