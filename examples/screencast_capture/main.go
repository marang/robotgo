// Command screencast_capture demonstrates reusable Wayland ScreenCast capture.
// Build it with: go run -tags pipewire ./examples/screencast_capture
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marang/robotgo"
)

func main() {
	output := flag.String("output", "screencast.png", "PNG output path")
	stream := flag.Int("stream", 0, "selected portal stream index")
	display := flag.Int("display", -1, "require the selected monitor to match this display ID")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	options := robotgo.ScreenCastCaptureOptions{
		Sources: robotgo.ScreenCastSourceMonitor,
		Cursor:  robotgo.ScreenCastCursorEmbedded,
		Persist: robotgo.ScreenCastPersistApp,
	}
	if err := robotgo.StartScreenCastCapture(ctx, options, *stream); err != nil {
		fmt.Fprintln(os.Stderr, "start ScreenCast capture:", err)
		os.Exit(1)
	}
	defer func() {
		if err := robotgo.CloseScreenCastCapture(); err != nil {
			fmt.Fprintln(os.Stderr, "close ScreenCast capture:", err)
		}
	}()

	frameCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	var (
		frame image.Image
		err   error
	)
	if *display >= 0 {
		frame, err = robotgo.CaptureScreenCastDisplay(frameCtx, *display)
	} else {
		frame, err = robotgo.CaptureScreenCast(frameCtx)
	}
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "capture frame:", err)
		os.Exit(1)
	}
	file, err := os.Create(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create output:", err)
		os.Exit(1)
	}
	if err := png.Encode(file, frame); err != nil {
		_ = file.Close()
		fmt.Fprintln(os.Stderr, "encode PNG:", err)
		os.Exit(1)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "close output:", err)
		os.Exit(1)
	}
	message := fmt.Sprintf("saved %s using one persistent portal session", *output)
	if robotgo.ScreenCastCaptureRestoreToken() != "" {
		message += "; a restore token is available and should be stored securely"
	}
	fmt.Println(message)
}
