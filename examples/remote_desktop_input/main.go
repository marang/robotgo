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
	demo := flag.Bool("demo", false, "after consent, move the pointer and type the keysym 'a'")
	flag.Parse()

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	capability, err := portalinput.Probe(probeCtx)
	cancelProbe()
	if err != nil {
		log.Fatalf("RemoteDesktop portal probe failed: %v", err)
	}
	fmt.Printf("RemoteDesktop portal version=%d devices=%d\n", capability.Version, capability.AvailableDevices)

	devices := portalinput.DeviceKeyboard | portalinput.DevicePointer
	if !capability.Supports(devices) {
		log.Fatalf("portal does not advertise keyboard and pointer support")
	}
	if !*connect && !*demo {
		fmt.Println("probe only; pass -connect to show the consent dialog")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if err := robotgo.StartRemoteDesktopInput(ctx, robotgo.RemoteDesktopKeyboard|robotgo.RemoteDesktopPointer); err != nil {
		log.Fatalf("open RemoteDesktop input session: %v", err)
	}
	defer func() {
		if err := robotgo.CloseRemoteDesktopInput(); err != nil {
			log.Printf("close RemoteDesktop input session: %v", err)
		}
	}()
	if err := robotgo.RemoteDesktopInputReady(robotgo.RemoteDesktopKeyboard | robotgo.RemoteDesktopPointer); err != nil {
		log.Fatalf("RemoteDesktop session is not ready: %v", err)
	}
	fmt.Println("keyboard and pointer permission granted")

	if !*demo {
		fmt.Println("session established; no input injected without -demo")
		return
	}
	if err := robotgo.MoveRelativeE(20, 0); err != nil {
		log.Fatalf("move pointer: %v", err)
	}
	if err := robotgo.TypeStrE("a"); err != nil {
		log.Fatalf("type text: %v", err)
	}
}
