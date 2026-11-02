//go:build cgo

package robotgo

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNativeDarwinPIDRange(t *testing.T) {
	for _, test := range []struct {
		name string
		pid  int64
		want bool
	}{
		{"negative", -1, false},
		{"negative-wraps-to-process", -(1 << 32) + 42, false},
		{"global", 0, false},
		{"minimum", 1, true},
		{"ordinary", 42, true},
		{"maximum", 1<<31 - 1, true},
		{"signed-overflow", 1 << 31, false},
		{"wraps-to-global", 1 << 32, false},
		{"wraps-to-process", 1<<32 + 42, false},
		{"go-maximum", int64(^uint(0) >> 1), strconv.IntSize == 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strconv.IntSize == 32 && (test.pid < -1<<31 || test.pid > 1<<31-1) {
				t.Skip("target is not representable as Go int on this architecture")
			}
			pid := int(test.pid)
			if got := nativeDarwinPIDValid(pid, false); got != test.want {
				t.Errorf("window PID %d valid = %v, want %v", pid, got, test.want)
			}
			wantInput := test.want || pid == 0
			if got := nativeDarwinPIDValid(pid, true); got != wantInput {
				t.Errorf("input PID %d valid = %v, want %v", pid, got, wantInput)
			}
			err := validateNativeInputPID(pid)
			if runtime.GOOS == "darwin" && !wantInput {
				if !errors.Is(err, ErrInvalidPID) {
					t.Errorf("invalid Darwin input PID %d error = %v, want ErrInvalidPID", pid, err)
				}
			} else if err != nil {
				t.Errorf("input PID %d on %s must retain its existing behavior: %v", pid, runtime.GOOS, err)
			}
		})
	}
}

func TestNativeDarwinWindowIDRange(t *testing.T) {
	for _, test := range []struct {
		id   int64
		want bool
	}{
		{-1, false}, {0, true}, {1, true}, {1<<31 - 1, true},
		{1 << 31, true}, {1<<32 - 1, true}, {1 << 32, false},
		{1<<32 + 42, false}, {-(1 << 32) + 42, false},
		{int64(^uint(0) >> 1), strconv.IntSize == 32},
	} {
		if strconv.IntSize == 32 && (test.id < -1<<31 || test.id > 1<<31-1) {
			continue
		}
		if got := nativeDarwinWindowIDValid(int(test.id)); got != test.want {
			t.Errorf("CGWindowID %d valid = %v, want %v", test.id, got, test.want)
		}
	}
}

// Source checks complement the range tests without calling a real keyboard or
// Accessibility API. In particular, a C-side guard cannot detect a Go int that
// has already wrapped through the legacy 32-bit C.uintptr on Darwin arm64.
func TestNativeDarwinPIDBridgeRouting(t *testing.T) {
	inputGuard := "if err := validateNativeInputPID(pid); err != nil {\n\t\treturn err\n\t}"
	windowGuard := `if runtime.GOOS == "darwin" && !nativeDarwinPIDValid(pid, false)`
	for _, test := range []struct {
		file, function, guard, before string
	}{
		{"key_native_cgo.go", "unicodeType", inputGuard, "C.uint(str)"},
		{"key_native_cgo.go", "nativeTapKey", inputGuard, "checkKeyCodes(name)"},
		{"key_native_cgo.go", "nativeToggleKey", inputGuard, "checkKeyCodes(name)"},
		{"native_adapter_cgo.go", "SetHandle", `if runtime.GOOS == "darwin" && !nativeDarwinWindowIDValid(hwnd)`, "C.uintptr(hwnd)"},
		{"native_adapter_cgo.go", "SetHandlePid", windowGuard, "lockNativeX11Display()"},
		{"native_adapter_cgo.go", "GetHandByPidC", windowGuard, "lockNativeX11Display()"},
		{"native_adapter_cgo.go", "cgetTitleLocked", windowGuard, "C.get_title_by_pid("},
		{"native_adapter_cgo.go", "internalGetBoundsLocked", windowGuard, "C.get_bounds("},
		{"native_adapter_cgo.go", "internalGetClientLocked", windowGuard, "C.get_client("},
		{"native_adapter_cgo.go", "internalActiveLocked", windowGuard, "C.active_PID("},
		{"key_policy_cgo.go", "keyTaps", inputGuard, "nativeKeyFlags(keyArr)"},
		{"key_policy_cgo.go", "keyTogglesB", inputGuard, "nativeKeyFlags(keyArr)"},
		{"key_text_cgo.go", "UnicodeTypeE", inputGuard, "lockLinuxKeyboard()"},
		{"key_text_cgo.go", "typeStrE", inputGuard, "lockLinuxKeyboard()"},
	} {
		t.Run(test.function, func(t *testing.T) {
			body := nativePIDFunctionBody(t, test.file, test.function)
			guard := strings.Index(body, test.guard)
			boundary := strings.Index(body, test.before)
			if guard < 0 || boundary < 0 || guard >= boundary {
				t.Fatalf("%s must reject invalid Darwin targets before %s", test.function, test.before)
			}
			// Window bridges have different zero-value return types; all must
			// return from the guard rather than merely evaluate the predicate.
			if test.file == "native_adapter_cgo.go" {
				guardBody, _, found := strings.Cut(body[guard:], "\n\t}")
				if !found || !strings.Contains(guardBody, "\n\t\treturn") {
					t.Fatal("invalid Darwin target guard must return before native dispatch")
				}
			}
		})
	}
}

func TestNativeDarwinPIDRoutingLineEndings(t *testing.T) {
	const body = "{\n\tif rejected {\n\t\treturn\n\t}\n}"
	const source = "package fixture\nfunc guarded() " + body + "\n"
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strconv.Quote(newline), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.go")
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(source, "\n", newline)), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := nativePIDFunctionBody(t, path, "guarded"); got != body {
				t.Fatalf("normalized function body = %q, want %q", got, body)
			}
		})
	}
}

func nativePIDFunctionBody(t *testing.T, name, function string) string {
	t.Helper()
	source, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		if declaration, ok := declaration.(*ast.FuncDecl); ok && declaration.Name.Name == function {
			body := string(source[fileSet.Position(declaration.Body.Pos()).Offset:fileSet.Position(declaration.Body.End()).Offset])
			return strings.ReplaceAll(body, "\r\n", "\n")
		}
	}
	t.Fatalf("missing function %s in %s", function, name)
	return ""
}
