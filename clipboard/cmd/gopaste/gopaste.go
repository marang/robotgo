package main

import (
	"fmt"

	"go.izoone.com/robotgo/clipboard"
)

func main() {
	text, err := clipboard.ReadAll()
	if err != nil {
		panic(err)
	}

	fmt.Print(text)
}
