package main

import (
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	// syscall.NewLazyDLL("user32.dll").NewProc("SetProcessDPIAware").Call()

	fmt.Println("DPI scale factor:", robotgo.SysScale())
	width, height := robotgo.GetScaleSize()
	fmt.Println("get scale screen size: ", width, height)

	bitmap, err := robotgo.CaptureScreen(0, 0, width, height)
	if err != nil {
		fmt.Println("CaptureScreen error:", err)
		return
	}
	defer robotgo.FreeBitmap(bitmap)
	// bitmap.Save(bitmap, "test.png")
	if err := robotgo.Save(robotgo.ToImage(bitmap), "test.png"); err != nil {
		fmt.Println(err)
	}

	config := robotgo.GetRuntimeConfig()
	config.Scale = true
	if err := robotgo.SetRuntimeConfig(config); err != nil {
		fmt.Println("SetRuntimeConfig error:", err)
		return
	}
	robotgo.Move(10, 10)
	robotgo.MoveSmooth(100, 100)

	fmt.Println(robotgo.Location())

	num := robotgo.DisplaysNum()
	for i := 0; i < num; i++ {
		rect := robotgo.GetScreenRect(i)
		fmt.Println("rect: ", rect)
	}
}
