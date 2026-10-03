//go:build cgo

package robotgo

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNativeScaleC(t *testing.T) {
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
			binary := filepath.Join(t.TempDir(), "native-scale")
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			// Exercise the fixture's assertion guard on every run, including when
			// callers supply release flags through CC.
			args := append(slices.Clone(compiler[1:]), "-std=c11", "-Wall", "-Werror", "-DNDEBUG",
				"-DIS_"+platform, "testdata/native_scale_harness.c", "-o", binary)
			output, err := exec.CommandContext(ctx, compiler[0], args...).CombinedOutput()
			if err != nil {
				t.Fatalf("compile %s scale harness: %v\n%s", platform, err, output)
			}
			output, err = exec.CommandContext(ctx, binary).CombinedOutput()
			if err != nil {
				t.Fatalf("run %s scale harness: %v\n%s", platform, err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}

// Match Go's compiler-command convention: whitespace-separated arguments with
// optional whole-field single/double quotes, without shell expansion/unescaping.
func splitNativeScaleCompiler(command string) ([]string, error) {
	var args []string
	for {
		command = strings.TrimLeft(command, " \t\r\n")
		if command == "" {
			return args, nil
		}
		if quote := command[0]; quote == '\'' || quote == '"' {
			command = command[1:]
			end := strings.IndexByte(command, quote)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c quote", quote)
			}
			args = append(args, command[:end])
			command = command[end+1:]
			continue
		}
		end := strings.IndexAny(command, " \t\r\n")
		if end < 0 {
			return append(args, command), nil
		}
		args = append(args, command[:end])
		command = command[end:]
	}
}

func TestNativeScaleCompilerCommand(t *testing.T) {
	for _, test := range []struct {
		name, command string
		want          []string
		wantError     bool
	}{
		{"simple", "gcc", []string{"gcc"}, false},
		{"wrapper and flags", "ccache clang -m64", []string{"ccache", "clang", "-m64"}, false},
		{"quoted path", `"C:\Program Files\cc.exe" -m64`, []string{`C:\Program Files\cc.exe`, "-m64"}, false},
		{"single quotes", " '/tool chain/cc' '-DVALUE=two words' ", []string{"/tool chain/cc", "-DVALUE=two words"}, false},
		{"whitespace", "\tcc\r\n-m64\t", []string{"cc", "-m64"}, false},
		{"no shell expansion", "cc $(private) *.c", []string{"cc", "$(private)", "*.c"}, false},
		{"empty", " \t", nil, false},
		{"empty quoted argument", `cc ""`, []string{"cc", ""}, false},
		{"unterminated", `"cc`, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := splitNativeScaleCompiler(test.command)
			if (err != nil) != test.wantError || !slices.Equal(got, test.want) {
				t.Fatalf("split(%q) = %q, %v; want %q, error=%v", test.command, got, err, test.want, test.wantError)
			}
		})
	}
}
