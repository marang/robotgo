//go:build cgo

package robotgo

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNativeUnicodeC(t *testing.T) {
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
	for _, platform := range []string{"MACOSX", "WINDOWS"} {
		t.Run(platform, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			binary := filepath.Join(t.TempDir(), "native-unicode")
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			args := append(slices.Clone(compiler[1:]), "-std=c11", "-Wall", "-Werror", "-DNDEBUG",
				"-DIS_"+platform, "testdata/native_unicode_harness.c", "-o", binary)
			output, err := exec.CommandContext(ctx, compiler[0], args...).CombinedOutput()
			if err != nil {
				t.Fatalf("compile %s Unicode harness: %v\n%s", platform, err, output)
			}
			output, err = exec.CommandContext(ctx, binary).CombinedOutput()
			if err != nil {
				t.Fatalf("run %s Unicode harness: %v\n%s", platform, err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}
