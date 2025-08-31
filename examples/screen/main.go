// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package main

import (
	"fmt"
	"strconv"

	"github.com/marang/robotgo"
	// "marang/robotgo"
)

func bitmap() error {
	fmt.Println("display server:", robotgo.DetectDisplayServer())
	bit, err := robotgo.CaptureScreen()
	if err != nil {
		fmt.Println("CaptureScreen error:", err)
		return err
	}
	defer robotgo.FreeBitmap(bit)
	fmt.Println("abitMap...", bit)

	gbit := robotgo.ToBitmap(bit)
	fmt.Println("bitmap...", gbit.Width)

	gbitMap, err := robotgo.CaptureGo()
	if err != nil {
		fmt.Println("CaptureGo error:", err)
	} else {
		fmt.Println("Go CaptureScreen...", gbitMap.Width)
	}
	// fmt.Println("...", gbitmap.Width, gbitmap.BytesPerPixel)
	if err := robotgo.SaveCapture("saveCapture.png", 10, 20, 100, 100); err != nil {
		fmt.Println(err)
	}

	img, err := robotgo.CaptureImg()
	fmt.Println("error: ", err)
	if err := robotgo.Save(img, "save.png"); err != nil {
		fmt.Println(err)
	}

	num := robotgo.DisplaysNum()
	for i := 0; i < num; i++ {
		robotgo.DisplayID = i
		img1, _ := robotgo.CaptureImg()
		path1 := "save_" + strconv.Itoa(i)
		if err := robotgo.Save(img1, path1+".png"); err != nil {
			fmt.Println(err)
		}
		if err := robotgo.SaveJpeg(img1, path1+".jpeg", 50); err != nil {
			fmt.Println(err)
		}

		img2, _ := robotgo.CaptureImg(10, 10, 20, 20)
		path2 := "test_" + strconv.Itoa(i)
		if err := robotgo.Save(img2, path2+".png"); err != nil {
			fmt.Println(err)
		}
		if err := robotgo.SaveJpeg(img2, path2+".jpeg", 50); err != nil {
			fmt.Println(err)
		}

		x, y, w, h := robotgo.GetDisplayBounds(i)
		img3, err := robotgo.CaptureImg(x, y, w, h)
		fmt.Println("Capture error: ", err)
		if err := robotgo.Save(img3, path2+"_1.png"); err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func color() {
	// gets the pixel color at 100, 200.
	color, err := robotgo.GetPixelColor(100, 200)
	if err != nil {
		fmt.Println("GetPixelColor error:", err)
	} else {
		fmt.Println("color----", color, "-----------------")
	}

	clo, err := robotgo.GetPxColor(100, 200)
	if err != nil {
		fmt.Println("GetPxColor error:", err)
	} else {
		fmt.Println("color...", clo)
		clostr := robotgo.PadHex(clo)
		fmt.Println("color...", clostr)
	}

	rgb := robotgo.RgbToHex(255, 100, 200)
	rgbstr := robotgo.PadHex(robotgo.U32ToHex(rgb))
	fmt.Println("rgb...", rgbstr)

	hex := robotgo.HexToRgb(uint32(rgb))
	fmt.Println("hex...", hex)
	hexh := robotgo.PadHex(robotgo.U8ToHex(hex))
	fmt.Println("HexToRgb...", hexh)

	// gets the pixel color at 10, 20.
	color2, err := robotgo.GetPixelColor(10, 20)
	if err != nil {
		fmt.Println("GetPixelColor error:", err)
	} else {
		fmt.Println("color---", color2)
	}
}

func screen() {
	////////////////////////////////////////////////////////////////////////////////
	// Read the screen
	////////////////////////////////////////////////////////////////////////////////

	if err := bitmap(); err != nil && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
		// On Wayland, exit early when capture isn't available.
		return
	}

	// gets the screen width and height
	sx, sy := robotgo.GetScreenSize()
	fmt.Println("get screen size: ", sx, sy)
	for i := 0; i < robotgo.DisplaysNum(); i++ {
		s1 := robotgo.ScaleF(i)
		fmt.Println("ScaleF: ", s1)
	}
	sx, sy = robotgo.GetScaleSize()
	fmt.Println("get screen scale size: ", sx, sy)

	color()
}

func main() {
	screen()
}
