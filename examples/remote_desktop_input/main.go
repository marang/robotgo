package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/marang/robotgo"
	portalinput "github.com/marang/robotgo/input/portal"
)

func main() {
	connect := flag.Bool("connect", false, "request a keyboard and pointer session")
	demo := flag.Bool("demo", false, "after consent, inject a small pointer movement and the keysym 'a'")
	mapScreen := flag.Bool("screen", false, "also select monitor streams for absolute pointer coordinates")
	touch := flag.Bool("touch", false, "request and demonstrate touchscreen input; implies -screen and requires -demo to inject")
	flag.Parse()

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	capability, err := portalinput.Probe(probeCtx)
	cancelProbe()
	if err != nil {
		if capability.ScreenCastIssue == "" {
			log.Fatalf("RemoteDesktop portal probe failed: %v", err)
		}
		log.Printf("ScreenCast portal probe degraded; relative keyboard and pointer input remain available: %v", err)
	}
	fmt.Printf("RemoteDesktop portal version=%d devices=%d; ScreenCast version=%d sources=%d cursors=%d\n",
		capability.Version, capability.AvailableDevices, capability.ScreenCastVersion,
		capability.AvailableSources, capability.AvailableCursorModes)

	devices := portalinput.DeviceKeyboard | portalinput.DevicePointer
	if *touch {
		devices |= portalinput.DeviceTouchscreen
	}
	if !capability.Supports(devices) {
		log.Fatalf("portal does not advertise all requested devices: requested=%d available=%d", devices, capability.AvailableDevices)
	}
	selectScreen := *mapScreen || *touch
	if selectScreen && !capability.SupportsSources(portalinput.SourceMonitor) {
		log.Fatalf("ScreenCast portal does not advertise monitor sources")
	}
	if selectScreen && !capability.SupportsCursorMode(portalinput.CursorHidden) {
		log.Fatalf("ScreenCast portal does not advertise hidden cursor mode")
	}
	if !*connect && !*demo {
		fmt.Println("probe only; pass -connect to show the consent dialog")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	options := robotgo.RemoteDesktopInputOptions{
		Devices: devices,
	}
	if selectScreen {
		options.Sources = robotgo.RemoteDesktopSourceMonitor
		options.Multiple = true
		options.CursorMode = robotgo.RemoteDesktopCursorHidden
		options.PersistMode = robotgo.RemoteDesktopPersistApp
	}
	if err := robotgo.StartRemoteDesktopInputWithOptions(ctx, options); err != nil {
		log.Fatalf("open RemoteDesktop input session: %v", err)
	}
	defer func() {
		if err := robotgo.CloseRemoteDesktopInput(); err != nil {
			log.Printf("close RemoteDesktop input session: %v", err)
		}
	}()
	if err := robotgo.RemoteDesktopInputReady(devices); err != nil {
		log.Fatalf("RemoteDesktop session is not ready: %v", err)
	}
	fmt.Printf("requested device permission granted: mask=%d\n", devices)
	var streams []robotgo.RemoteDesktopStream
	if selectScreen {
		streams, err = robotgo.RemoteDesktopInputStreams()
		if err != nil {
			log.Fatalf("read selected streams: %v", err)
		}
		for i, stream := range streams {
			fmt.Printf("stream %d: node=%d position=%+v size=%+v mapping=%q\n",
				i, stream.NodeID, stream.Position, stream.Size, stream.MappingID)
		}
		if token := robotgo.RemoteDesktopInputRestoreToken(); token != "" {
			// Store token securely and pass it as options.RestoreToken on the next
			// launch. Tokens are single-use, so replace the stored value each time.
			fmt.Println("single-use restore token granted (value intentionally not logged)")
		}
	}

	if !*demo {
		fmt.Println("session established; no input injected without -demo")
		return
	}
	if selectScreen {
		stream := streams[0]
		x, y := 1, 1
		if stream.HasPosition {
			x += int(stream.Position.X)
			y += int(stream.Position.Y)
		}
		if err := robotgo.MoveE(x, y, 0); err != nil {
			log.Fatalf("move pointer absolutely in selected stream: %v", err)
		}
		if *touch {
			if err := robotgo.RemoteDesktopTouchDown(stream.NodeID, 0, 1, 1); err != nil {
				log.Fatalf("touch down: %v", err)
			}
			if err := robotgo.RemoteDesktopTouchUp(0); err != nil {
				log.Fatalf("touch up: %v", err)
			}
		}
	} else if err := robotgo.MoveRelativeE(20, 0); err != nil {
		log.Fatalf("move pointer relatively: %v", err)
	}
	if err := robotgo.TypeStrE("a"); err != nil {
		log.Fatalf("type text: %v", err)
	}
}
