//go:build cgo

package robotgo

// Deprecated: use the DragSmooth(),
//
// Drag drag the mouse to (x, y),
// It's not valid now, use the DragSmooth()
func Drag(x, y int, args ...string) {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, err := runNativeMouseOperation(server, func() error {
		x, y = moveScaleLocked(x, y)
		button := "left"
		if len(args) > 0 {
			button = args[0]
		}
		nativeDragMouse(x, y, button)
		return nil
	})
	if err != nil {
		return
	}
	MilliSleep(currentMouseDelay())
}

// DragSmooth drag the mouse like smooth to (x, y)
//
// Examples:
//
//	robotgo.DragSmooth(10, 10)
func DragSmooth(x, y int, args ...interface{}) {
	dragSmoothWith(x, y, args, Toggle, MoveSmooth, MilliSleep)
}

func dragSmoothWith(x, y int, args []interface{}, toggle func(...interface{}) error, move func(int, int, ...interface{}) bool, wait func(int)) {
	if _, _, _, ok := parseSmoothMoveArguments(args); !ok {
		return
	}
	if err := toggle("left"); err != nil {
		return
	}
	wait(50)
	move(x, y, args...)
	if err := toggle("left", "up"); err != nil {
		return
	}
}

// MoveSmooth move the mouse smooth,
// moves mouse to x, y human like, with the mouse button up.
//
// robotgo.MoveSmooth(x, y int, low, high float64, mouseDelay int)
//
// Examples:
//
//	robotgo.MoveSmooth(10, 10)
//	robotgo.MoveSmooth(10, 10, 1.0, 2.0)
func MoveSmooth(x, y int, args ...interface{}) bool {
	lowDelay, highDelay, mouseDelay, ok := parseSmoothMoveArguments(args)
	if !ok {
		return false
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	var moved bool
	_, err := runNativeMouseOperation(server, func() error {
		x, y = moveScaleLocked(x, y)
		moved = nativeMoveMouseSmooth(x, y, lowDelay, highDelay)
		return nil
	})
	if err != nil {
		return false
	}
	MilliSleep(currentMouseDelay() + mouseDelay)
	return moved
}

// MoveArgs get the mouse relative args
func MoveArgs(x, y int) (int, int) {
	mx, my := Location()
	mx = mx + x
	my = my + y

	return mx, my
}

// MoveSmoothRelative move mouse smooth with relative
func MoveSmoothRelative(x, y int, args ...interface{}) {
	if _, _, _, ok := parseSmoothMoveArguments(args); !ok {
		return
	}
	mx, my := MoveArgs(x, y)
	MoveSmooth(mx, my, args...)
}

// MoveClick move and click the mouse
//
// robotgo.MoveClick(x, y int, button string, double bool)
//
// Examples:
//
//	robotgo.MouseSleep = 100
//	robotgo.MoveClick(10, 10)
func MoveClick(x, y int, args ...interface{}) {
	moveClickWith(x, y, args, MoveE, ClickE, MilliSleep)
}

// Legacy composites deliberately continue after failures. Keep their execution
// distinct from the fail-fast strict alternatives planned for a later slice.
func moveClickWith(x, y int, args []interface{}, move func(int, int, ...int) error, click func(...interface{}) error, wait func(int)) {
	_ = move(x, y)
	wait(50)
	_ = click(args...)
}

// MovesClick move smooth and click the mouse
//
// use the `robotgo.MouseSleep = 100`
func MovesClick(x, y int, args ...interface{}) {
	movesClickWith(x, y, args, MoveSmooth, ClickE, MilliSleep)
}

func movesClickWith(x, y int, args []interface{}, move func(int, int, ...interface{}) bool, click func(...interface{}) error, wait func(int)) {
	move(x, y)
	wait(50)
	_ = click(args...)
}

// ScrollDir scroll the mouse with direction to (x, "up")
// supported: "up", "down", "left", "right"
//
// Examples:
//
//	robotgo.ScrollDir(10, "down")
//	robotgo.ScrollDir(10, "up")
func ScrollDir(x int, direction ...interface{}) {
	d, err := parseScrollDirection(direction)
	if err != nil {
		return
	}

	if d == "down" {
		Scroll(0, -x)
	}
	if d == "up" {
		Scroll(0, x)
	}

	if d == "left" {
		Scroll(x, 0)
	}
	if d == "right" {
		Scroll(-x, 0)
	}
	// MilliSleep(MouseSleep)
}

// ScrollSmooth scroll the mouse smooth,
// default scroll 5 times and sleep 100 millisecond
//
// robotgo.ScrollSmooth(toy, num, sleep, tox)
//
// Examples:
//
//	robotgo.ScrollSmooth(-10)
//	robotgo.ScrollSmooth(-10, 6, 200, -10)
func ScrollSmooth(to int, args ...int) {
	scrollSmoothWith(to, args, ScrollE, MilliSleep, currentMouseDelay)
}

func scrollSmoothWith(to int, args []int, scroll func(int, int, ...int) error, wait func(int), mouseDelay func() int) {
	i := 0
	num := 5
	if len(args) > 0 {
		num = args[0]
	}
	tm := 100
	if len(args) > 1 {
		tm = args[1]
	}
	tox := 0
	if len(args) > 2 {
		tox = args[2]
	}

	for {
		_ = scroll(tox, to)
		wait(tm)
		i++
		if i == num {
			break
		}
	}
	wait(mouseDelay())
}

// ScrollRelative scroll mouse with relative
//
// Examples:
//
//	robotgo.ScrollRelative(10, 10)
func ScrollRelative(x, y int, args ...int) {
	mx, my := MoveArgs(x, y)
	Scroll(mx, my, args...)
}
