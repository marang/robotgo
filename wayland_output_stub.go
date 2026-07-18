//go:build cgo || !linux

package robotgo

func pureGoWaylandScreenSizeE() (int, int, error, bool) {
	return 0, 0, nil, false
}

func pureGoWaylandScreenRectE(...int) (Rect, error, bool) {
	return Rect{}, nil, false
}

func pureGoWaylandDisplaysNumE() (int, error, bool) {
	return 0, nil, false
}
