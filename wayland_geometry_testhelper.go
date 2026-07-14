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
