package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/marang/robotgo"
)

func main() {
	runtimeInfo := robotgo.GetRuntimeBackendInfo()
	fmt.Printf("RobotGo build: implementation=%s cgo=%t platform=%s/%s display=%s\n",
		runtimeInfo.BuildImplementation, runtimeInfo.CGOEnabled,
		runtimeInfo.GOOS, runtimeInfo.GOARCH, runtimeInfo.DisplayServer)

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
	fmt.Printf("remote desktop portal: %s (available=%v, reason=%s)\n", caps.RemoteDesktop.Backend, caps.RemoteDesktop.Available, caps.RemoteDesktop.Reason)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	remoteDesktop, remoteErr := robotgo.GetRemoteDesktopInputStatus(ctx)
	cancel()
	fmt.Printf("remote desktop diagnostic: portal-v%d screencast-v%d permission=%s streams=%d reason=%s\n",
		remoteDesktop.PortalVersion, remoteDesktop.ScreenCastVersion,
		remoteDesktop.Permission, len(remoteDesktop.Streams), remoteDesktop.Reason)
	if remoteErr != nil {
		fmt.Printf("remote desktop probe error: %v\n", remoteErr)
	}
	fmt.Printf("window backend: %s (available=%v)\n", caps.Window.Backend, caps.Window.Available)
}
