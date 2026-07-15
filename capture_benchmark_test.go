package robotgo

import (
	"os"
	"testing"
)

const envCaptureBenchmark = "ROBOTGO_CAPTURE_BENCHMARK"

// BenchmarkCaptureImgRuntime measures the same public capture path in native
// CGO and Pure-Go builds. It is opt-in because it requires a real desktop and
// may require an OS consent grant.
func BenchmarkCaptureImgRuntime(b *testing.B) {
	if os.Getenv(envCaptureBenchmark) == "" {
		b.Skip("set ROBOTGO_CAPTURE_BENCHMARK=1 in an authorized GUI session")
	}
	b.ReportAllocs()
	for range b.N {
		if _, err := CaptureImg(0, 0, 640, 480); err != nil {
			b.Fatal(err)
		}
	}
}
