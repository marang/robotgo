//go:build cgo && linux && wayland && test

package robotgo

/*
#cgo CFLAGS: -DROBOTGO_WAYLAND_TEST
void robotgo_wayland_map_logical_rect_for_test(int cap_w, int cap_h,
                                                int logical_w, int logical_h,
                                                int transform, int *x, int *y,
                                                int *w, int *h);
int robotgo_wayland_select_output_rect_for_test(int req_x, int req_y,
                                                 int req_w, int req_h,
                                                 const int *rects,
                                                 int rect_count);
int robotgo_wayland_resolve_bounds_for_test(const int *values, int count,
                                             int display_id,
                                             int *x, int *y,
                                             int *width, int *height);
int robotgo_wayland_stable_output_name_for_test(const int *values,
                                                 int output_count,
                                                 int display_id);
*/
import "C"

import "unsafe"

func mapWaylandLogicalRectForTest(capWidth, capHeight, logicalWidth, logicalHeight, transform int, rect [4]int) [4]int {
	x, y, width, height := C.int(rect[0]), C.int(rect[1]), C.int(rect[2]), C.int(rect[3])
	C.robotgo_wayland_map_logical_rect_for_test(
		C.int(capWidth), C.int(capHeight), C.int(logicalWidth), C.int(logicalHeight), C.int(transform),
		&x, &y, &width, &height,
	)
	return [4]int{int(x), int(y), int(width), int(height)}
}

func selectWaylandOutputRectForTest(request [4]int, outputs [][4]int) int {
	if len(outputs) == 0 {
		return -1
	}
	values := make([]C.int, 0, len(outputs)*4)
	for _, output := range outputs {
		values = append(values, C.int(output[0]), C.int(output[1]), C.int(output[2]), C.int(output[3]))
	}
	return int(C.robotgo_wayland_select_output_rect_for_test(
		C.int(request[0]), C.int(request[1]), C.int(request[2]), C.int(request[3]),
		(*C.int)(unsafe.Pointer(&values[0])), C.int(len(outputs)),
	))
}

type waylandBoundsOutputForTest struct {
	position    [2]int
	mode        [2]int
	transform   int
	scale       int
	logicalPos  [2]int
	logicalSize [2]int
	hasLogical  bool
	name        int
}

func resolveWaylandBoundsForTest(outputs []waylandBoundsOutputForTest, displayID int) ([4]int, bool) {
	if len(outputs) == 0 {
		return [4]int{}, false
	}
	const valuesPerOutput = 12
	values := make([]C.int, 0, len(outputs)*valuesPerOutput)
	for _, output := range outputs {
		flags := 0
		if output.hasLogical {
			flags = 3
		}
		values = append(
			values,
			C.int(output.position[0]),
			C.int(output.position[1]),
			C.int(output.mode[0]),
			C.int(output.mode[1]),
			C.int(output.transform),
			C.int(output.scale),
			C.int(output.logicalPos[0]),
			C.int(output.logicalPos[1]),
			C.int(output.logicalSize[0]),
			C.int(output.logicalSize[1]),
			C.int(flags),
			C.int(output.name),
		)
	}
	var x, y, width, height C.int
	if C.robotgo_wayland_resolve_bounds_for_test(
		(*C.int)(unsafe.Pointer(&values[0])),
		C.int(len(outputs)),
		C.int(displayID),
		&x,
		&y,
		&width,
		&height,
	) != 0 {
		return [4]int{}, false
	}
	return [4]int{int(x), int(y), int(width), int(height)}, true
}

func stableWaylandOutputNameForTest(outputs [][5]int, displayID int) int {
	if len(outputs) == 0 {
		return -1
	}
	values := make([]C.int, 0, len(outputs)*len(outputs[0]))
	for _, output := range outputs {
		for _, value := range output {
			values = append(values, C.int(value))
		}
	}
	return int(C.robotgo_wayland_stable_output_name_for_test(
		(*C.int)(unsafe.Pointer(&values[0])),
		C.int(len(outputs)),
		C.int(displayID),
	))
}
