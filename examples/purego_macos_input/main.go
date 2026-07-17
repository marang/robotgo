// Command purego_macos_input demonstrates permission-aware Pure-Go keyboard
// automation on macOS without injecting input unless -act is supplied.
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"

	"github.com/marang/robotgo"
)

func main() {
	act := flag.Bool("act", false, "type into the currently focused application")
	text := flag.String("text", "RobotGo Pure-Go Quartz input", "text used with -act")
	flag.Parse()

	if runtime.GOOS != "darwin" {
		log.Fatal("this example requires macOS")
	}
	defer func() {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			log.Printf("release Pure-Go input resources: %v", err)
		}
	}()

	capability := robotgo.GetRuntimeCapabilities().Keyboard
	fmt.Printf(
		"keyboard backend: %s; available: %t; reason: %s\n",
		capability.Backend, capability.Available, capability.Reason,
	)
	if !*act {
		fmt.Println("inspection only; pass -act to type into the focused application")
		return
	}
	if err := robotgo.KeyboardReady(); err != nil {
		log.Fatalf("keyboard automation is unavailable: %v", err)
	}
	if err := robotgo.TypeStrE(*text); err != nil {
		log.Fatal(err)
	}
	fmt.Println("text sent through exact UTF-16 Quartz events")
}
