package main

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"

	"github.com/marang/robotgo"
)

func main() {
	if runtime.GOOS != "linux" {
		fmt.Println("GetLinuxCapabilities is Linux-specific")
		return
	}

	caps := robotgo.GetLinuxCapabilities()
	data, err := json.MarshalIndent(caps, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("linux capabilities:")
	fmt.Println(string(data))

	fmt.Printf("display server: %s\n", caps.DisplayServer)
	fmt.Printf("compositor: %s\n", caps.Compositor)
	fmt.Printf("capture backend: %s (available=%v, fallback=%v)\n", caps.Capture.Backend, caps.Capture.Available, caps.Capture.Fallback)
	fmt.Printf("bounds backend: %s (available=%v, fallback=%v)\n", caps.Bounds.Backend, caps.Bounds.Available, caps.Bounds.Fallback)
	fmt.Printf("window backend: %s (available=%v)\n", caps.Window.Backend, caps.Window.Available)
}
