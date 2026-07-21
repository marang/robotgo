//go:build cgo && linux && wayland && test

package robotgo

/*
#cgo CFLAGS: -DROBOTGO_WAYLAND_TEST
#include "mouse/wayland_absolute.h"
#include "mouse/wayland_flush.h"
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

static int robotgo_wayland_map_pointer_for_test(
    int global_x, int global_y, int origin_x, int origin_y,
    int width, int height, unsigned int *mapped_x, unsigned int *mapped_y) {
    return robotgo_wayland_map_absolute(
        global_x, global_y, origin_x, origin_y, width, height,
        ROBOTGO_WAYLAND_ABSOLUTE_EXTENT,
        ROBOTGO_WAYLAND_ABSOLUTE_EXTENT,
        mapped_x, mapped_y);
}

struct robotgo_wayland_flush_test_state {
    const int *flush_errnos;
    int flush_count;
    int flush_index;
    const int *wait_results;
    int wait_count;
    int wait_index;
};

static int robotgo_wayland_test_flush(void *context) {
    struct robotgo_wayland_flush_test_state *state = context;
    int index = state->flush_index++;
    int error = index < state->flush_count ? state->flush_errnos[index] : 0;
    if (error == 0) {
        return 0;
    }
    errno = error;
    return -1;
}

static int robotgo_wayland_test_wait(void *context, int timeout_ms) {
    (void)timeout_ms;
    struct robotgo_wayland_flush_test_state *state = context;
    int index = state->wait_index++;
    int result = index < state->wait_count ? state->wait_results[index] : 0;
    if (result < 0) {
        errno = -result;
        return -1;
    }
    return result;
}

static int robotgo_wayland_flush_retry_for_test(
    const int *flush_errnos, int flush_count,
    const int *wait_results, int wait_count,
    int attempts, int *flush_calls, int *wait_calls) {
    struct robotgo_wayland_flush_test_state state = {
        .flush_errnos = flush_errnos,
        .flush_count = flush_count,
        .flush_index = 0,
        .wait_results = wait_results,
        .wait_count = wait_count,
        .wait_index = 0
    };
    int result = robotgo_wayland_flush_with_retry(
        &state, robotgo_wayland_test_flush, robotgo_wayland_test_wait,
        1, attempts);
    *flush_calls = state.flush_index;
    *wait_calls = state.wait_index;
    return result;
}
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

func mapWaylandPointerForTest(point [2]int, bounds [4]int) ([2]uint32, bool) {
	var x, y C.uint
	status := C.robotgo_wayland_map_pointer_for_test(
		C.int(point[0]), C.int(point[1]),
		C.int(bounds[0]), C.int(bounds[1]),
		C.int(bounds[2]), C.int(bounds[3]),
		&x, &y,
	)
	return [2]uint32{uint32(x), uint32(y)}, status == 0
}

func waylandFlushRetryForTest(flushErrnos, waitResults []int, attempts int) (result, flushCalls, waitCalls int) {
	flushValues := make([]C.int, len(flushErrnos))
	for index, value := range flushErrnos {
		flushValues[index] = C.int(value)
	}
	waitValues := make([]C.int, len(waitResults))
	for index, value := range waitResults {
		waitValues[index] = C.int(value)
	}
	var flushPointer, waitPointer *C.int
	if len(flushValues) > 0 {
		flushPointer = &flushValues[0]
	}
	if len(waitValues) > 0 {
		waitPointer = &waitValues[0]
	}
	var cFlushCalls, cWaitCalls C.int
	result = int(C.robotgo_wayland_flush_retry_for_test(
		flushPointer, C.int(len(flushValues)),
		waitPointer, C.int(len(waitValues)),
		C.int(attempts), &cFlushCalls, &cWaitCalls,
	))
	return result, int(cFlushCalls), int(cWaitCalls)
}
