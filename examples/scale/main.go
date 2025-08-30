package main

import (
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	// syscall.NewLazyDLL("user32.dll").NewProc("SetProcessDPIAware").Call()

	width, height := robotgo.GetScaleSize()
	fmt.Println("get scale screen size: ", width, height)

	bitmap := robotgo.CaptureScreen(0, 0, width, height)
	defer robotgo.FreeBitmap(bitmap)
	// bitmap.Save(bitmap, "test.png")
	if err := robotgo.Save(robotgo.ToImage(bitmap), "test.png"); err != nil {
		fmt.Println(err)
	}

	robotgo.Scale = true
	robotgo.Move(10, 10)
	robotgo.MoveSmooth(100, 100)

	fmt.Println(robotgo.Location())

	num := robotgo.DisplaysNum()
	for i := 0; i < num; i++ {
		rect := robotgo.GetScreenRect(i)
		fmt.Println("rect: ", rect)
	}
}
