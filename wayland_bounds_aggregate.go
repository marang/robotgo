package robotgo

type waylandOutputBounds struct {
	x, y int
	w, h int
}

func aggregateWaylandOutputBounds(bounds []waylandOutputBounds) (Rect, bool) {
	if len(bounds) == 0 {
		return Rect{}, false
	}
	maxInt64 := int64(^uint64(0) >> 1)
	edge := func(origin, size int) (int64, bool) {
		if size <= 0 {
			return 0, false
		}
		o := int64(origin)
		s := int64(size)
		if o > maxInt64-s {
			return 0, false
		}
		return o + s, true
	}
	minX := int64(bounds[0].x)
	minY := int64(bounds[0].y)
	maxX, okX := edge(bounds[0].x, bounds[0].w)
	maxY, okY := edge(bounds[0].y, bounds[0].h)
	if !okX || !okY {
		return Rect{}, false
	}
	for i := 1; i < len(bounds); i++ {
		b := bounds[i]
		x := int64(b.x)
		y := int64(b.y)
		right, okRight := edge(b.x, b.w)
		bottom, okBottom := edge(b.y, b.h)
		if !okRight || !okBottom {
			return Rect{}, false
		}
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if right > maxX {
			maxX = right
		}
		if bottom > maxY {
			maxY = bottom
		}
	}
	if (minX < 0 && maxX > maxInt64+minX) ||
		(minY < 0 && maxY > maxInt64+minY) {
		return Rect{}, false
	}
	width := maxX - minX
	height := maxY - minY
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	if minX < minInt || minY < minInt ||
		maxX > maxInt || maxY > maxInt ||
		width <= 0 || width > maxInt || height <= 0 || height > maxInt {
		return Rect{}, false
	}
	return Rect{
		Point{X: int(minX), Y: int(minY)},
		Size{W: int(width), H: int(height)},
	}, true
}
