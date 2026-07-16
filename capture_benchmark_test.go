package robotgo

import (
	"image"
	"os"
	"runtime"
	"testing"
)

const (
	envCaptureBenchmark = "ROBOTGO_CAPTURE_BENCHMARK"
	benchmarkCaptureW   = 640
	benchmarkCaptureH   = 480
)

var benchmarkCaptureImage image.Image

// BenchmarkCaptureImgRuntime measures the same public capture path in native
// CGO and Pure-Go builds. It is opt-in because it requires a real desktop and
// may require an OS consent grant.
func BenchmarkCaptureImgRuntime(b *testing.B) {
	if os.Getenv(envCaptureBenchmark) == "" {
		b.Skip("set ROBOTGO_CAPTURE_BENCHMARK=1 in an authorized GUI session")
	}
	warmup, err := CaptureImg(0, 0, benchmarkCaptureW, benchmarkCaptureH)
	if err != nil {
		b.Fatalf("warm up CaptureImg: %v", err)
	}
	if got := warmup.Bounds(); got.Dx() != benchmarkCaptureW || got.Dy() != benchmarkCaptureH {
		b.Fatalf("CaptureImg bounds = %v, want %dx%d", got, benchmarkCaptureW, benchmarkCaptureH)
	}
	benchmarkCaptureImage = warmup
	b.SetBytes(benchmarkCaptureW * benchmarkCaptureH * 4)
	b.ReportAllocs()
	last := warmup
	b.ResetTimer()
	for range b.N {
		img, err := CaptureImg(0, 0, benchmarkCaptureW, benchmarkCaptureH)
		if err != nil {
			b.Fatal(err)
		}
		last = img
	}
	b.StopTimer()
	benchmarkCaptureImage = last
	runtime.KeepAlive(last)
}
