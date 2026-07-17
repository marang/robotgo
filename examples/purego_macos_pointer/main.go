// Command purego_macos_pointer demonstrates permission-aware Pure-Go pointer
// automation on macOS.
package main

import (
	"fmt"
	"log"
	"runtime"

	"github.com/marang/robotgo"
)

func main() {
	if runtime.GOOS != "darwin" {
		log.Fatal("this example requires macOS")
	}
	defer func() {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			log.Printf("release Pure-Go input resources: %v", err)
		}
	}()

	capabilities := robotgo.GetRuntimeCapabilities()
	fmt.Printf("pointer backend: %s\n", capabilities.Mouse.Backend)
	if err := robotgo.MouseReady(); err != nil {
		log.Fatalf("pointer automation is unavailable: %v", err)
	}

	x, y, err := robotgo.LocationE()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := robotgo.MoveE(x, y); err != nil {
			log.Printf("restore pointer location: %v", err)
		}
	}()
	if err := robotgo.MoveE(x+20, y+20); err != nil {
		log.Fatal(err)
	}
	fmt.Println("moved the pointer by 20 logical points; restoring it on exit")
}
