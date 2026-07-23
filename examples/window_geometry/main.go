package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marang/robotgo"
)

func main() {
	pid := flag.Int("pid", 0, "process ID to inspect on backends that support PID resolution")
	handle := flag.Int("handle", 0, "native window handle to inspect")
	flag.Parse()

	if *pid != 0 && *handle != 0 {
		fmt.Fprintln(os.Stderr, "-pid and -handle are mutually exclusive")
		os.Exit(2)
	}

	target := *pid
	var targetMode []int
	switch {
	case *handle != 0:
		target = *handle
		targetMode = []int{1}
	case *pid == 0:
		fmt.Println("no target supplied; querying the active compositor window")
	}

	x, y, width, height, err := robotgo.GetBoundsE(target, targetMode...)
	printGeometry("window", x, y, width, height, err)
	x, y, width, height, err = robotgo.GetClientE(target, targetMode...)
	printGeometry("client", x, y, width, height, err)
}

func printGeometry(name string, x, y, width, height int, err error) {
	if err != nil {
		fmt.Printf("%s geometry unavailable: %v\n", name, err)
		return
	}
	fmt.Printf("%s geometry: x=%d y=%d width=%d height=%d\n", name, x, y, width, height)
}
