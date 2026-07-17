package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marang/robotgo"
)

func main() {
	maximize := flag.Bool("maximize", false, "maximize the active Hyprland window")
	restore := flag.Bool("restore", false, "restore the active Hyprland window")
	flag.Parse()

	if *maximize && *restore {
		fmt.Fprintln(os.Stderr, "-maximize and -restore are mutually exclusive")
		os.Exit(2)
	}

	if *maximize || *restore {
		if err := robotgo.MaxWindowE(0, *maximize); err != nil {
			fmt.Fprintln(os.Stderr, "change active-window state:", err)
			os.Exit(1)
		}
	}

	maximized, err := robotgo.IsMaximizedE()
	if err != nil {
		fmt.Fprintln(os.Stderr, "query active-window state:", err)
		os.Exit(1)
	}
	fmt.Printf("active window maximized: %t\n", maximized)
}
