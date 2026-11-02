//go:build cgo

package robotgo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeAXWindowC(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "env", "CC").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve C compiler: %v\n%s", err, output)
	}
	compiler, err := splitNativeScaleCompiler(strings.TrimSpace(string(output)))
	if err != nil || len(compiler) == 0 || compiler[0] == "" {
		t.Fatalf("invalid C compiler command %q: %v", output, err)
	}
	binary := filepath.Join(t.TempDir(), "native-ax-window")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	args := append(slices.Clone(compiler[1:]), "-std=c11", "-Wall", "-Werror", "-DNDEBUG",
		"-DIS_MACOSX", "testdata/native_ax_window_harness.c", "-o", binary)
	output, err = exec.CommandContext(ctx, compiler[0], args...).CombinedOutput()
	if err != nil {
		t.Fatalf("compile AX window harness: %v\n%s", err, output)
	}
	output, err = exec.CommandContext(ctx, binary).CombinedOutput()
	if err != nil {
		t.Fatalf("run AX window harness: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

// The harness executes the private production helpers, not the platform-sized
// legacy headers. Keep their small public-C routing branches tied to that seam.
func TestNativeAXWindowRouting(t *testing.T) {
	for _, test := range []struct{ path, start, end, call string }{
		{"window/goWindow.h", "bool min_window(", "#elif defined(IS_LINUX)", "return robotgo_ax_minimize_pid(pid, state);"},
		{"window/window.h", "bool close_window_by_PId(", "// CloseWindow", "return robotgo_ax_close_pid(pid);"},
		{"window/window.h", "bool close_window_by_Id(MData m_data){", "#elif defined(IS_LINUX)", "return robotgo_ax_close_window(m_data.AxID);"},
		{"native_adapter_cgo.go", "func nativeMinWindow(", "func nativeMaxWindow(", `runtime.GOOS == "darwin" && !nativeDarwinWindowPIDValid(pid)`},
		{"native_adapter_cgo.go", "func nativeCloseWindowByPid(", "func nativeGetMainTitle(", `runtime.GOOS == "darwin" && !nativeDarwinWindowPIDValid(pid)`},
	} {
		data, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		_, body, found := strings.Cut(string(data), test.start)
		if !found {
			t.Fatalf("missing function %q in %s", test.start, test.path)
		}
		body, _, found = strings.Cut(body, test.end)
		if !found || !strings.Contains(body, test.call) {
			t.Errorf("%s: %s must route through tested private AX helper %q", test.path, test.start, test.call)
		}
		if test.path == "native_adapter_cgo.go" && strings.Index(body, test.call) > strings.Index(body, "C.uintptr(pid)") {
			t.Errorf("%s: PID guard must run before narrowing C conversion", test.start)
		}
	}
}

func TestNativeAXWindowPIDRange(t *testing.T) {
	for _, test := range []struct {
		pid  int64
		want bool
	}{
		{-1, false}, {0, false}, {1, true}, {1<<31 - 1, true},
		{1 << 31, false}, {1<<32 + 42, false}, {-(1<<32 - 42), false},
	} {
		if strconv.IntSize == 32 && (test.pid < -1<<31 || test.pid > 1<<31-1) {
			continue
		}
		if got := nativeDarwinWindowPIDValid(int(test.pid)); got != test.want {
			t.Errorf("Darwin PID %d valid = %v, want %v", test.pid, got, test.want)
		}
	}
}
