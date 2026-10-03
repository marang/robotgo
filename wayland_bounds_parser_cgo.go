//go:build cgo

package robotgo

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

var (
	reWaylandOutputID  = regexp.MustCompile(`name:\s*([0-9]+)\s*$`)
	rePosXY            = regexp.MustCompile(`x:\s*(-?[0-9]+),\s*y:\s*(-?[0-9]+)`)
	reLogicalXY        = regexp.MustCompile(`logical_x:\s*(-?[0-9]+),\s*logical_y:\s*(-?[0-9]+)`)
	reLogicalWH        = regexp.MustCompile(`logical_width:\s*([0-9]+),\s*logical_height:\s*([0-9]+)`)
	reModeWH           = regexp.MustCompile(`width:\s*([0-9]+)\s*px,\s*height:\s*([0-9]+)\s*px`)
	reXDGOutputID      = regexp.MustCompile(`output:\s*([0-9]+)\s*$`)
	reWaylandScale     = regexp.MustCompile(`scale:\s*([0-9]+)`)
	reWaylandTransform = regexp.MustCompile(
		`transform:\s*([[:alnum:]_-]+)`,
	)
)

type waylandWLModeState struct {
	w, h int
}

func parseWaylandInfoBounds(raw string) (Rect, bool) {
	aggregate, _, ok := parseWaylandInfoGeometry(raw)
	return aggregate, ok
}

func parseWaylandInfoGeometry(raw string) (Rect, Rect, bool) {
	bounds, ok := parseWaylandInfoOutputBounds(raw)
	if !ok {
		return Rect{}, Rect{}, false
	}
	aggregate, ok := aggregateWaylandOutputBounds(bounds)
	if !ok {
		return Rect{}, Rect{}, false
	}
	primary, ok := primaryWaylandOutputBounds(bounds)
	if !ok {
		return Rect{}, Rect{}, false
	}
	return aggregate, primary, true
}

func parseWaylandInfoOutputBounds(raw string) ([]waylandOutputBounds, bool) {
	type xdgState struct {
		outputID      int
		hasID         bool
		logicalX      int
		logicalY      int
		logicalW      int
		logicalH      int
		hasLogicalPos bool
		hasLogicalWH  bool
	}
	type wlState struct {
		outputID      int
		hasID         bool
		x             int
		y             int
		hasPos        bool
		currentMode   waylandWLModeState
		hasCurrent    bool
		firstMode     waylandWLModeState
		hasFirst      bool
		pendingMode   waylandWLModeState
		hasPending    bool
		expectModeVal bool
		scale         int
		transform     int
	}

	xdgBounds := make(map[int]waylandOutputBounds)
	var logicalBounds []waylandOutputBounds
	var wlBounds []waylandOutputBounds

	inXDG := false
	inWL := false
	var xs xdgState
	var ws wlState

	commitXDG := func() {
		if !xs.hasLogicalWH {
			return
		}
		x := 0
		y := 0
		if xs.hasLogicalPos {
			x = xs.logicalX
			y = xs.logicalY
		}
		bounds := waylandOutputBounds{
			x: x,
			y: y,
			w: xs.logicalW,
			h: xs.logicalH,
		}
		if xs.hasID {
			bounds.name = xs.outputID
			bounds.named = true
		}
		logicalBounds = append(logicalBounds, bounds)
		if xs.hasID {
			xdgBounds[xs.outputID] = bounds
		}
	}

	commitWL := func() {
		if !ws.hasPos {
			return
		}
		w := 0
		h := 0
		if ws.hasCurrent {
			w, h = ws.currentMode.w, ws.currentMode.h
		} else if ws.hasFirst {
			w, h = ws.firstMode.w, ws.firstMode.h
		}
		if w <= 0 || h <= 0 {
			return
		}
		if ws.hasID {
			if b, ok := xdgBounds[ws.outputID]; ok && b.w > 0 && b.h > 0 {
				wlBounds = append(wlBounds, b)
				return
			}
		}
		scale := ws.scale
		if scale <= 0 {
			scale = 1
		}
		w /= scale
		h /= scale
		if waylandTransformRotatesDimensions(ws.transform) {
			w, h = h, w
		}
		if w <= 0 || h <= 0 {
			return
		}
		wlBounds = append(wlBounds, waylandOutputBounds{
			x:     ws.x,
			y:     ws.y,
			w:     w,
			h:     h,
			name:  ws.outputID,
			named: ws.hasID,
		})
	}

	newWLState := func() wlState {
		return wlState{scale: 1, transform: waylandTransformNormal}
	}

	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "interface: '") {
			if inXDG {
				commitXDG()
				inXDG = false
				xs = xdgState{}
			}
			if inWL {
				commitWL()
				inWL = false
				ws = newWLState()
			}
			if strings.HasPrefix(line, "interface: 'wl_output'") {
				inWL = true
				ws = newWLState()
				if m := reWaylandOutputID.FindStringSubmatch(line); len(m) == 2 {
					if id, err := strconv.Atoi(m[1]); err == nil {
						ws.outputID = id
						ws.hasID = true
					}
				}
			}
			continue
		}

		if line == "xdg_output_v1" {
			if inXDG {
				commitXDG()
			}
			inXDG = true
			xs = xdgState{}
			continue
		}

		if inXDG {
			if m := reXDGOutputID.FindStringSubmatch(line); len(m) == 2 {
				if id, err := strconv.Atoi(m[1]); err == nil {
					xs.outputID = id
					xs.hasID = true
				}
				continue
			}
			if m := reLogicalXY.FindStringSubmatch(line); len(m) == 3 {
				x, errX := strconv.Atoi(m[1])
				y, errY := strconv.Atoi(m[2])
				if errX == nil && errY == nil {
					xs.logicalX = x
					xs.logicalY = y
					xs.hasLogicalPos = true
				}
				continue
			}
			if m := reLogicalWH.FindStringSubmatch(line); len(m) == 3 {
				w, errW := strconv.Atoi(m[1])
				h, errH := strconv.Atoi(m[2])
				if errW == nil && errH == nil {
					xs.logicalW = w
					xs.logicalH = h
					xs.hasLogicalWH = true
				}
				continue
			}
		}

		if inWL {
			if m := reWaylandScale.FindStringSubmatch(line); len(m) == 2 {
				if scale, err := strconv.Atoi(m[1]); err == nil && scale > 0 {
					ws.scale = scale
				}
			}
			if m := reWaylandTransform.FindStringSubmatch(line); len(m) == 2 {
				if transform, ok := parseWaylandTransform(m[1]); ok {
					ws.transform = transform
				}
			}
			if m := rePosXY.FindStringSubmatch(line); len(m) == 3 {
				x, errX := strconv.Atoi(m[1])
				y, errY := strconv.Atoi(m[2])
				if errX == nil && errY == nil {
					ws.x = x
					ws.y = y
					ws.hasPos = true
				}
				continue
			}
			if line == "mode:" {
				ws.expectModeVal = true
				ws.hasPending = false
				continue
			}
			if ws.expectModeVal {
				if m := reModeWH.FindStringSubmatch(line); len(m) == 3 {
					w, errW := strconv.Atoi(m[1])
					h, errH := strconv.Atoi(m[2])
					if errW == nil && errH == nil {
						ws.pendingMode = waylandWLModeState{w: w, h: h}
						ws.hasPending = true
						if !ws.hasFirst {
							ws.firstMode = ws.pendingMode
							ws.hasFirst = true
						}
					}
					continue
				}
				if strings.HasPrefix(line, "flags:") {
					if ws.hasPending && strings.Contains(line, "current") {
						ws.currentMode = ws.pendingMode
						ws.hasCurrent = true
					}
					ws.expectModeVal = false
					ws.hasPending = false
					continue
				}
			}
		}
	}
	if sc.Err() != nil {
		return nil, false
	}

	if inXDG {
		commitXDG()
	}
	if inWL {
		commitWL()
	}

	if len(logicalBounds) > 0 &&
		(len(wlBounds) == 0 || len(logicalBounds) == len(wlBounds)) {
		return logicalBounds, true
	}
	return wlBounds, len(wlBounds) > 0
}
