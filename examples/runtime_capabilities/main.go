package main

import (
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	capabilities := robotgo.GetRuntimeCapabilities()
	fmt.Printf(
		"RobotGo %s/%s (%s)\n",
		capabilities.Runtime.GOOS,
		capabilities.Runtime.GOARCH,
		capabilities.Runtime.BuildImplementation,
	)
	printFeature("capture", capabilities.Capture)
	printFeature("bounds", capabilities.Bounds)
	printFeature("keyboard", capabilities.Keyboard)
	printFeature("mouse", capabilities.Mouse)
	printFeature("remote desktop", capabilities.RemoteDesktop)
	printFeature("window", capabilities.Window)
	printFeature("process", capabilities.Process)
	printFeature("clipboard", capabilities.Clipboard)
	printFeature("hook", capabilities.Hook)
	printFeature("events", capabilities.Events)
}

func printFeature(name string, capability robotgo.FeatureCapability) {
	fmt.Printf(
		"%-15s available=%-5t backend=%-24s reason=%s\n",
		name,
		capability.Available,
		capability.Backend,
		capability.Reason,
	)
	if capability.Notes != "" {
		fmt.Printf("  note: %s\n", capability.Notes)
	}
}
