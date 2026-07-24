package main

import (
	"errors"
	"fmt"

	"github.com/marang/robotgo"
)

func main() {
	pid, err := robotgo.GetPidE()
	if err != nil {
		fmt.Println("active window PID unavailable:", err)
	} else {
		fmt.Println("active window PID:", pid)
	}

	handle, err := robotgo.GetActiveE()
	if err != nil {
		if errors.Is(err, robotgo.ErrNotSupported) {
			fmt.Println("active window handle unsupported by this backend")
			return
		}
		fmt.Println("active window handle unavailable:", err)
		return
	}
	fmt.Printf("active window handle: %v\n", handle)
}
