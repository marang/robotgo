//go:build wayland
// +build wayland

package robotgo

import "log"

func GetBounds(pid int, args ...int) (int, int, int, int) {
	log.Println("Wayland GetBounds not implemented")
	return 0, 0, 0, 0
}
