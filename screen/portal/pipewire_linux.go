//go:build linux && cgo && pipewire

package portal

/*
#cgo CFLAGS: -I/usr/include/pipewire-0.3 -I/usr/include/spa-0.2 -D_REENTRANT
#cgo LDFLAGS: -lpipewire-0.3
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pipewire/pipewire.h>
#include <spa/param/video/format-utils.h>

#define ROBOTGO_PW_MAX_FRAME_BYTES (512u * 1024u * 1024u)

struct robotgo_pw_capture {
	struct pw_thread_loop *loop;
	struct pw_context *context;
	struct pw_core *core;
	struct pw_stream *stream;
	struct spa_hook listener;
	struct spa_video_info_raw format;
	uint8_t *pixels;
	size_t pixels_size;
	uint32_t width;
	uint32_t height;
	uint32_t frame_width;
	uint32_t frame_height;
	uint32_t transform;
	uint64_t generation;
	uint64_t delivered;
	int failed;
	char error[256];
};

static void robotgo_pw_set_error(struct robotgo_pw_capture *capture, const char *message) {
	if (capture->failed) return;
	capture->failed = 1;
	snprintf(capture->error, sizeof(capture->error), "%s", message ? message : "PipeWire stream failed");
	pw_thread_loop_signal(capture->loop, false);
}

static void robotgo_pw_state_changed(void *userdata, enum pw_stream_state old,
		enum pw_stream_state state, const char *error) {
	(void)old;
	struct robotgo_pw_capture *capture = userdata;
	if (state == PW_STREAM_STATE_ERROR || state == PW_STREAM_STATE_UNCONNECTED)
		robotgo_pw_set_error(capture, error ? error : "PipeWire stream disconnected");
	else
		pw_thread_loop_signal(capture->loop, false);
}

static void robotgo_pw_param_changed(void *userdata, uint32_t id, const struct spa_pod *param) {
	struct robotgo_pw_capture *capture = userdata;
	if (param == NULL || id != SPA_PARAM_Format) return;
	if (spa_format_video_raw_parse(param, &capture->format) < 0) {
		robotgo_pw_set_error(capture, "PipeWire returned an invalid raw video format");
		return;
	}
	capture->width = capture->format.size.width;
	capture->height = capture->format.size.height;
	pw_thread_loop_signal(capture->loop, false);
}

static int robotgo_pw_pixel_size(enum spa_video_format format) {
	switch (format) {
	case SPA_VIDEO_FORMAT_BGRx:
	case SPA_VIDEO_FORMAT_BGRA:
	case SPA_VIDEO_FORMAT_RGBx:
	case SPA_VIDEO_FORMAT_RGBA:
		return 4;
	case SPA_VIDEO_FORMAT_BGR:
	case SPA_VIDEO_FORMAT_RGB:
		return 3;
	default:
		return 0;
	}
}

static void robotgo_pw_convert_row(uint8_t *dst, const uint8_t *src, uint32_t width,
		enum spa_video_format format) {
	for (uint32_t x = 0; x < width; x++) {
		uint8_t r, g, b, a = 255;
		switch (format) {
		case SPA_VIDEO_FORMAT_BGRx: b=src[0]; g=src[1]; r=src[2]; break;
		case SPA_VIDEO_FORMAT_BGRA: b=src[0]; g=src[1]; r=src[2]; a=src[3]; break;
		case SPA_VIDEO_FORMAT_RGBx: r=src[0]; g=src[1]; b=src[2]; break;
		case SPA_VIDEO_FORMAT_RGBA: r=src[0]; g=src[1]; b=src[2]; a=src[3]; break;
		case SPA_VIDEO_FORMAT_BGR: b=src[0]; g=src[1]; r=src[2]; break;
		default: r=src[0]; g=src[1]; b=src[2]; break;
		}
		dst[0]=r; dst[1]=g; dst[2]=b; dst[3]=a;
		src += robotgo_pw_pixel_size(format);
		dst += 4;
	}
}

static void robotgo_pw_process(void *userdata) {
	struct robotgo_pw_capture *capture = userdata;
	struct pw_buffer *buffer;
	while ((buffer = pw_stream_dequeue_buffer(capture->stream)) != NULL) {
		struct spa_buffer *spa = buffer->buffer;
		if (spa->n_datas < 1 || spa->datas[0].chunk == NULL || capture->width == 0 || capture->height == 0) {
			pw_stream_queue_buffer(capture->stream, buffer);
			continue;
		}
		struct spa_data *data = &spa->datas[0];
		if (data->data == NULL) {
			pw_stream_queue_buffer(capture->stream, buffer);
			if (data->type == SPA_DATA_DmaBuf)
				robotgo_pw_set_error(capture, "PipeWire returned a non-mappable DMA-BUF frame");
			else
				robotgo_pw_set_error(capture, "PipeWire returned an unmapped frame buffer");
			return;
		}
		struct spa_chunk *chunk = data->chunk;
		int bpp = robotgo_pw_pixel_size(capture->format.format);
		int32_t stride = chunk->stride != 0 ? chunk->stride : (int32_t)(capture->width * bpp);
		uint64_t stride_size = stride < 0 ? (uint64_t)(-(int64_t)stride) : (uint64_t)stride;
		uint32_t crop_x = 0, crop_y = 0, frame_width = capture->width, frame_height = capture->height;
		struct spa_meta_region *crop = spa_buffer_find_meta_data(spa, SPA_META_VideoCrop, sizeof(*crop));
		if (crop != NULL && spa_meta_region_is_valid(crop) && crop->region.position.x >= 0 && crop->region.position.y >= 0 &&
			(uint64_t)crop->region.position.x + crop->region.size.width <= capture->width &&
			(uint64_t)crop->region.position.y + crop->region.size.height <= capture->height) {
			crop_x = (uint32_t)crop->region.position.x;
			crop_y = (uint32_t)crop->region.position.y;
			frame_width = crop->region.size.width;
			frame_height = crop->region.size.height;
		}
		struct spa_meta_videotransform *transform = spa_buffer_find_meta_data(spa, SPA_META_VideoTransform, sizeof(*transform));
		uint32_t frame_transform = transform != NULL && transform->transform <= SPA_META_TRANSFORMATION_Flipped270 ?
			transform->transform : SPA_META_TRANSFORMATION_None;
		size_t source_row_size = (size_t)capture->width * (size_t)bpp;
		size_t output_size = (size_t)frame_width * (size_t)frame_height * 4u;
		uint64_t required = (uint64_t)chunk->offset + (uint64_t)(capture->height - 1) * stride_size + source_row_size;
		uint64_t chunk_end = (uint64_t)chunk->offset + (uint64_t)chunk->size;
		if (bpp == 0 || output_size / 4u / frame_width != frame_height || output_size > ROBOTGO_PW_MAX_FRAME_BYTES ||
			(data->maxsize != 0 && required > data->maxsize) ||
			(chunk->size != 0 && required > chunk_end) ||
			source_row_size > stride_size) {
			pw_stream_queue_buffer(capture->stream, buffer);
			robotgo_pw_set_error(capture, "PipeWire returned an unsupported or truncated frame");
			return;
		}
		uint8_t *pixels = realloc(capture->pixels, output_size);
		if (pixels == NULL) {
			pw_stream_queue_buffer(capture->stream, buffer);
			robotgo_pw_set_error(capture, "allocate PipeWire frame");
			return;
		}
		capture->pixels = pixels;
		capture->pixels_size = output_size;
		const uint8_t *base = (const uint8_t*)data->data + chunk->offset;
		if (stride < 0) base += (capture->height - 1) * stride_size;
		base += (ptrdiff_t)crop_y * stride + (size_t)crop_x * (size_t)bpp;
		for (uint32_t y = 0; y < frame_height; y++) {
			robotgo_pw_convert_row(pixels + (size_t)y * frame_width * 4u,
				base + (ptrdiff_t)y * stride, frame_width, capture->format.format);
		}
		capture->frame_width = frame_width;
		capture->frame_height = frame_height;
		capture->transform = frame_transform;
		capture->generation++;
		pw_stream_queue_buffer(capture->stream, buffer);
		pw_thread_loop_signal(capture->loop, false);
	}
}

static const struct pw_stream_events robotgo_pw_events = {
	PW_VERSION_STREAM_EVENTS,
	.state_changed = robotgo_pw_state_changed,
	.param_changed = robotgo_pw_param_changed,
	.process = robotgo_pw_process,
};

static char *robotgo_pw_error(const char *prefix) {
	char buffer[320];
	snprintf(buffer, sizeof(buffer), "%s: %s", prefix, strerror(errno));
	return strdup(buffer);
}

static void robotgo_pw_capture_free(struct robotgo_pw_capture *capture);

static struct robotgo_pw_capture *robotgo_pw_capture_new(int fd, uint32_t node_id,
		uint64_t serial, char **error_out) {
	*error_out = NULL;
	pw_init(NULL, NULL);
	struct robotgo_pw_capture *capture = calloc(1, sizeof(*capture));
	if (capture == NULL) { close(fd); *error_out = robotgo_pw_error("allocate PipeWire capture"); return NULL; }
	capture->loop = pw_thread_loop_new("robotgo-pipewire", NULL);
	if (capture->loop == NULL) { close(fd); *error_out = robotgo_pw_error("create PipeWire loop"); free(capture); return NULL; }
	if (pw_thread_loop_start(capture->loop) < 0) {
		close(fd);
		*error_out = robotgo_pw_error("start PipeWire loop");
		pw_thread_loop_destroy(capture->loop);
		free(capture);
		return NULL;
	}
	pw_thread_loop_lock(capture->loop);
	capture->context = pw_context_new(pw_thread_loop_get_loop(capture->loop), NULL, 0);
	if (capture->context == NULL) { close(fd); *error_out = robotgo_pw_error("create PipeWire context"); pw_thread_loop_unlock(capture->loop); robotgo_pw_capture_free(capture); return NULL; }
	capture->core = pw_context_connect_fd(capture->context, fd, NULL, 0);
	if (capture->core == NULL) { *error_out = robotgo_pw_error("connect PipeWire remote"); pw_thread_loop_unlock(capture->loop); robotgo_pw_capture_free(capture); return NULL; }
	struct pw_properties *props = pw_properties_new(PW_KEY_MEDIA_TYPE, "Video", PW_KEY_MEDIA_CATEGORY, "Capture", PW_KEY_MEDIA_ROLE, "Screen", NULL);
	if (props == NULL) { *error_out = robotgo_pw_error("create PipeWire stream properties"); pw_thread_loop_unlock(capture->loop); robotgo_pw_capture_free(capture); return NULL; }
	if (serial != 0) {
		char target[32]; snprintf(target, sizeof(target), "%llu", (unsigned long long)serial);
		pw_properties_set(props, PW_KEY_TARGET_OBJECT, target);
	}
	capture->stream = pw_stream_new(capture->core, "robotgo-screen-capture", props);
	if (capture->stream == NULL) { *error_out = robotgo_pw_error("create PipeWire stream"); pw_thread_loop_unlock(capture->loop); robotgo_pw_capture_free(capture); return NULL; }
	pw_stream_add_listener(capture->stream, &capture->listener, &robotgo_pw_events, capture);
	uint8_t pod_buffer[1024];
	struct spa_pod_builder builder = SPA_POD_BUILDER_INIT(pod_buffer, sizeof(pod_buffer));
	const struct spa_pod *params[1];
	params[0] = spa_pod_builder_add_object(&builder,
		SPA_TYPE_OBJECT_Format, SPA_PARAM_EnumFormat,
		SPA_FORMAT_mediaType, SPA_POD_Id(SPA_MEDIA_TYPE_video),
		SPA_FORMAT_mediaSubtype, SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
		SPA_FORMAT_VIDEO_format, SPA_POD_CHOICE_ENUM_Id(7,
			SPA_VIDEO_FORMAT_BGRx, SPA_VIDEO_FORMAT_BGRx, SPA_VIDEO_FORMAT_BGRA,
			SPA_VIDEO_FORMAT_RGBx, SPA_VIDEO_FORMAT_RGBA, SPA_VIDEO_FORMAT_BGR, SPA_VIDEO_FORMAT_RGB),
		SPA_FORMAT_VIDEO_size, SPA_POD_CHOICE_RANGE_Rectangle(
			&SPA_RECTANGLE(1920, 1080), &SPA_RECTANGLE(1, 1), &SPA_RECTANGLE(16384, 16384)),
		SPA_FORMAT_VIDEO_framerate, SPA_POD_CHOICE_RANGE_Fraction(
			&SPA_FRACTION(30, 1), &SPA_FRACTION(0, 1), &SPA_FRACTION(240, 1)));
	uint32_t target = serial != 0 ? PW_ID_ANY : node_id;
	int result = pw_stream_connect(capture->stream, PW_DIRECTION_INPUT, target,
		PW_STREAM_FLAG_AUTOCONNECT | PW_STREAM_FLAG_MAP_BUFFERS, params, 1);
	pw_thread_loop_unlock(capture->loop);
	if (result < 0) { errno = -result; *error_out = robotgo_pw_error("connect PipeWire stream"); robotgo_pw_capture_free(capture); return NULL; }
	return capture;
}

static int robotgo_pw_capture_frame(struct robotgo_pw_capture *capture, int timeout_ms,
		uint8_t **pixels, size_t *size, uint32_t *width, uint32_t *height,
		uint32_t *transform, char **error_out) {
	*error_out = NULL; *pixels = NULL; *size = 0;
	pw_thread_loop_lock(capture->loop);
	struct timespec timeout;
	int time_result = pw_thread_loop_get_time(capture->loop, &timeout, (int64_t)timeout_ms * 1000000LL);
	if (time_result < 0) {
		errno = -time_result;
		*error_out = robotgo_pw_error("create PipeWire frame deadline");
		pw_thread_loop_unlock(capture->loop);
		return -1;
	}
	while (!capture->failed && capture->generation <= capture->delivered) {
		int result = pw_thread_loop_timed_wait_full(capture->loop, &timeout);
		if (result == -ETIMEDOUT) { pw_thread_loop_unlock(capture->loop); return 1; }
		if (result < 0) { errno = -result; *error_out = robotgo_pw_error("wait for PipeWire frame"); pw_thread_loop_unlock(capture->loop); return -1; }
	}
	if (capture->failed) { *error_out = strdup(capture->error); pw_thread_loop_unlock(capture->loop); return -1; }
	uint8_t *copy = malloc(capture->pixels_size);
	if (copy == NULL) { *error_out = robotgo_pw_error("copy PipeWire frame"); pw_thread_loop_unlock(capture->loop); return -1; }
	memcpy(copy, capture->pixels, capture->pixels_size);
	*pixels = copy; *size = capture->pixels_size; *width = capture->frame_width; *height = capture->frame_height; *transform = capture->transform;
	capture->delivered = capture->generation;
	pw_thread_loop_unlock(capture->loop);
	return 0;
}

static void robotgo_pw_capture_free(struct robotgo_pw_capture *capture) {
	if (capture == NULL) return;
	if (capture->loop != NULL) {
		pw_thread_loop_lock(capture->loop);
		if (capture->stream != NULL) { spa_hook_remove(&capture->listener); pw_stream_destroy(capture->stream); capture->stream = NULL; }
		if (capture->core != NULL) { pw_core_disconnect(capture->core); capture->core = NULL; }
		if (capture->context != NULL) { pw_context_destroy(capture->context); capture->context = NULL; }
		pw_thread_loop_unlock(capture->loop);
		pw_thread_loop_stop(capture->loop);
		pw_thread_loop_destroy(capture->loop);
	}
	free(capture->pixels);
	free(capture);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	pipeWireFrameTimeout = 5 * time.Second
	pipeWirePollInterval = 100 * time.Millisecond
)

type cgoPipeWireFrameSource struct {
	mu      sync.Mutex
	stream  *C.struct_robotgo_pw_capture
	closing atomic.Bool
}

func pipeWireCaptureCompiled() bool { return true }

func newPipeWireFrameSource(session ScreenCast, stream ScreenCastStream) (pipeWireFrameSource, error) {
	file, err := session.PipeWireFile()
	if err != nil {
		return nil, err
	}
	fd, err := unix.Dup(int(file.Fd()))
	closeErr := file.Close()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("duplicate PipeWire consumer fd: %w", err), closeErr)
	}
	if closeErr != nil {
		_ = unix.Close(fd)
		return nil, closeErr
	}
	unix.CloseOnExec(fd)
	var cError *C.char
	capture := C.robotgo_pw_capture_new(C.int(fd), C.uint32_t(stream.NodeID), C.uint64_t(stream.PipeWireSerial), &cError)
	if capture == nil {
		message := "create PipeWire capture"
		if cError != nil {
			message = C.GoString(cError)
			C.free(unsafe.Pointer(cError))
		}
		return nil, fmt.Errorf("%w: %s", ErrPipeWireUnavailable, message)
	}
	return &cgoPipeWireFrameSource{stream: capture}, nil
}

func (s *cgoPipeWireFrameSource) frame(ctx context.Context) (*image.RGBA, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream == nil || s.closing.Load() {
		return nil, ErrScreenCastClosed
	}
	deadline := time.Now().Add(pipeWireFrameTimeout)
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
	}
	for {
		if s.closing.Load() {
			return nil, ErrScreenCastClosed
		}
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		wait := min(remaining, pipeWirePollInterval)
		var pixels *C.uint8_t
		var size C.size_t
		var width, height, transform C.uint32_t
		var cError *C.char
		result := C.robotgo_pw_capture_frame(s.stream, C.int((wait+time.Millisecond-1)/time.Millisecond), &pixels, &size, &width, &height, &transform, &cError)
		runtime.KeepAlive(s)
		if cError != nil {
			message := C.GoString(cError)
			C.free(unsafe.Pointer(cError))
			return nil, fmt.Errorf("%w: %s", ErrPipeWireUnavailable, message)
		}
		if result > 0 {
			continue
		}
		if result < 0 {
			return nil, ErrPipeWireUnavailable
		}
		defer C.free(unsafe.Pointer(pixels))
		w, h := int(width), int(height)
		if w <= 0 || h <= 0 || uint64(size) != uint64(w)*uint64(h)*4 {
			return nil, fmt.Errorf("%w: invalid frame dimensions %dx%d size=%d", ErrPipeWireUnavailable, w, h, uint64(size))
		}
		data := C.GoBytes(unsafe.Pointer(pixels), C.int(size))
		return transformPipeWireFrame(&image.RGBA{Pix: data, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}, uint32(transform))
	}
}

func transformPipeWireFrame(source *image.RGBA, transform uint32) (*image.RGBA, error) {
	if transform > uint32(C.SPA_META_TRANSFORMATION_Flipped270) {
		return nil, fmt.Errorf("%w: invalid video transform %d", ErrPipeWireUnavailable, transform)
	}
	if transform == uint32(C.SPA_META_TRANSFORMATION_None) {
		return source, nil
	}
	rotation := transform
	flipped := transform >= uint32(C.SPA_META_TRANSFORMATION_Flipped)
	if flipped {
		rotation -= uint32(C.SPA_META_TRANSFORMATION_Flipped)
	}
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	resultWidth, resultHeight := w, h
	if rotation == uint32(C.SPA_META_TRANSFORMATION_90) || rotation == uint32(C.SPA_META_TRANSFORMATION_270) {
		resultWidth, resultHeight = h, w
	}
	result := image.NewRGBA(image.Rect(0, 0, resultWidth, resultHeight))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sourceX := x
			if flipped {
				sourceX = w - 1 - sourceX
			}
			destinationX, destinationY := sourceX, y
			switch rotation {
			case uint32(C.SPA_META_TRANSFORMATION_90):
				destinationX, destinationY = y, w-1-sourceX
			case uint32(C.SPA_META_TRANSFORMATION_180):
				destinationX, destinationY = w-1-sourceX, h-1-y
			case uint32(C.SPA_META_TRANSFORMATION_270):
				destinationX, destinationY = h-1-y, sourceX
			}
			sourceOffset := source.PixOffset(x, y)
			destinationOffset := result.PixOffset(destinationX, destinationY)
			copy(result.Pix[destinationOffset:destinationOffset+4], source.Pix[sourceOffset:sourceOffset+4])
		}
	}
	return result, nil
}

func (s *cgoPipeWireFrameSource) close() error {
	s.interrupt()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		C.robotgo_pw_capture_free(s.stream)
		s.stream = nil
	}
	return nil
}

func (s *cgoPipeWireFrameSource) interrupt() { s.closing.Store(true) }
