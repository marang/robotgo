//go:build linux && x11integration && !wayland

package robotgo_test

import (
	"os"
	"testing"

	"github.com/marang/robotgo"
)

const (
	envX11InputBenchmark     = "ROBOTGO_X11_INPUT_BENCHMARK"
	x11BenchmarkDrainEvery   = 64
	x11BenchmarkPointerBaseX = 180
	x11BenchmarkPointerBaseY = 170
)

var (
	benchmarkX11LocationX int
	benchmarkX11LocationY int
)

// BenchmarkX11InputRuntime measures identical public input operations in the
// native CGO and Pure-Go test binaries. Correctness is enforced separately by
// TestX11BackendBehavioralParity; these measurements never define a pass/fail
// timing threshold.
func BenchmarkX11InputRuntime(b *testing.B) {
	if os.Getenv(envX11InputBenchmark) == "" {
		b.Skip("set ROBOTGO_X11_INPUT_BENCHMARK=1 in an isolated X11 session")
	}
	harness := newX11InputHarness(b)
	assertExpectedX11Implementation(b)

	previousConfig := robotgo.GetRuntimeConfig()
	config := previousConfig
	config.MouseDelay = 0
	config.KeyDelay = 0
	config.Scale = false
	if err := robotgo.SetRuntimeConfig(config); err != nil {
		b.Fatalf("SetRuntimeConfig: %v", err)
	}
	b.Cleanup(func() {
		if err := robotgo.SetRuntimeConfig(previousConfig); err != nil {
			b.Errorf("restore RuntimeConfig: %v", err)
		}
	})
	if err := robotgo.MoveE(x11BenchmarkPointerBaseX, x11BenchmarkPointerBaseY); err != nil {
		b.Fatalf("warm X11 pointer backend: %v", err)
	}
	if err := robotgo.KeyPress("enter"); err != nil {
		b.Fatalf("warm X11 keyboard backend: %v", err)
	}
	harness.drainEvents()

	b.Run("Location", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			x, y, err := robotgo.LocationE()
			benchmarkX11LocationX, benchmarkX11LocationY = x, y
			return err
		})
	})
	b.Run("MoveAbsolute", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(iteration int) error {
			return robotgo.MoveE(x11BenchmarkPointerBaseX+iteration%2, x11BenchmarkPointerBaseY)
		})
	})
	b.Run("MoveRelative", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(iteration int) error {
			delta := 1
			if iteration%2 != 0 {
				delta = -1
			}
			return robotgo.MoveRelativeE(delta, 0)
		})
	})
	b.Run("ButtonTogglePair", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			if err := robotgo.Toggle("left", "down"); err != nil {
				return err
			}
			return robotgo.Toggle("left", "up")
		})
	})
	b.Run("ClickLeft", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			return robotgo.ClickE("left")
		})
	})
	b.Run("ScrollVertical1", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			return robotgo.ScrollE(0, 1, 0)
		})
	})
	b.Run("KeyTogglePairEnter", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			if err := robotgo.KeyToggle("enter", "down"); err != nil {
				return err
			}
			return robotgo.KeyToggle("enter", "up")
		})
	})
	b.Run("KeyPressEnter", func(b *testing.B) {
		benchmarkX11Operation(b, harness, func(_ int) error {
			return robotgo.KeyPress("enter")
		})
	})
	b.Run("TypeASCII8", func(b *testing.B) {
		const text = "RobotGo!"
		b.SetBytes(int64(len(text)))
		benchmarkX11Operation(b, harness, func(_ int) error {
			return robotgo.TypeStrE(text, 0, 0, 0)
		})
	})

	benchmarkX11LocationX, benchmarkX11LocationY = harness.queryPointer()
}

func benchmarkX11Operation(b *testing.B, harness *x11InputHarness, operation func(int) error) {
	b.Helper()
	if err := operation(0); err != nil {
		b.Fatalf("benchmark warm-up: %v", err)
	}
	harness.drainEvents()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := range b.N {
		if err := operation(iteration + 1); err != nil {
			b.Fatalf("iteration %d: %v", iteration, err)
		}
		if (iteration+1)%x11BenchmarkDrainEvery == 0 {
			b.StopTimer()
			harness.drainEvents()
			b.StartTimer()
		}
	}
	b.StopTimer()
	harness.drainEvents()
	if x, y := harness.queryPointer(); x < 0 || y < 0 {
		b.Fatalf("invalid X11 pointer location (%d, %d)", x, y)
	}
}
