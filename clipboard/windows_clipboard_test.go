package clipboard

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	windowsFixtureMemory = uintptr(17)
	windowsFixtureOwner  = uintptr(29)
)

// This fixture is the only backend used below: no test opens a real clipboard,
// creates a window, or stores clipboard contents on disk.
type windowsClipboardFixture struct {
	t          *testing.T
	calls      []string
	owners     []uintptr
	fail       map[string]error
	after      func(string)
	openFails  int
	openError  error
	read       []uint16
	readSize   uintptr
	written    []uint16
	allocation uintptr
}

func newWindowsClipboardFixture(t *testing.T, data []uint16) *windowsClipboardFixture {
	t.Helper()
	return &windowsClipboardFixture{
		t: t, fail: make(map[string]error), read: data,
		readSize: uintptr(len(data)) * 2,
	}
}

func (f *windowsClipboardFixture) call(name string) error {
	f.t.Helper()
	f.calls = append(f.calls, name)
	if f.after != nil {
		f.after(name)
	}
	return f.fail[name]
}

func (f *windowsClipboardFixture) memory(h uintptr) {
	f.t.Helper()
	if h != windowsFixtureMemory {
		f.t.Fatalf("memory handle = %d, want %d", h, windowsFixtureMemory)
	}
}

func (f *windowsClipboardFixture) ops() windowsClipboardOps {
	return windowsClipboardOps{
		open: func(owner uintptr) error {
			f.owners = append(f.owners, owner)
			if err := f.call("open"); err != nil {
				return err
			}
			if f.openFails > 0 {
				f.openFails--
				return f.openError
			}
			return nil
		},
		close: func() error { return f.call("close") },
		data: func() (uintptr, error) {
			return windowsFixtureMemory, f.call("data")
		},
		size: func(h uintptr) (uintptr, error) {
			f.memory(h)
			return f.readSize, f.call("size")
		},
		lock: func(h uintptr) (unsafe.Pointer, error) {
			f.memory(h)
			if err := f.call("lock"); err != nil {
				return nil, err
			}
			if f.written != nil {
				return unsafe.Pointer(&f.written[0]), nil
			}
			return unsafe.Pointer(&f.read[0]), nil
		},
		unlock: func(h uintptr) error {
			f.memory(h)
			return f.call("unlock")
		},
		alloc: func(size uintptr) (uintptr, error) {
			f.allocation = size
			if err := f.call("alloc"); err != nil {
				return 0, err
			}
			f.written = make([]uint16, size/2)
			return windowsFixtureMemory, nil
		},
		free: func(h uintptr) error {
			f.memory(h)
			return f.call("free")
		},
		empty: func() error { return f.call("empty") },
		set: func(h uintptr) error {
			f.memory(h)
			return f.call("set")
		},
		createOwner: func() (uintptr, error) {
			return windowsFixtureOwner, f.call("createOwner")
		},
		destroyOwner: func(owner uintptr) error {
			if owner != windowsFixtureOwner {
				f.t.Fatalf("destroy owner = %d, want %d", owner, windowsFixtureOwner)
			}
			return f.call("destroyOwner")
		},
	}
}

func (f *windowsClipboardFixture) wantCalls(want ...string) {
	f.t.Helper()
	if !reflect.DeepEqual(f.calls, want) {
		f.t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestWindowsClipboardReadUTF16(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"unicode", "日本語 é \U0001f680"},
		{"larger than legacy bound", strings.Repeat("a", (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append(utf16.Encode([]rune(tc.text)), 0)
			f := newWindowsClipboardFixture(t, data)
			got, err := readWindowsClipboard(context.Background(), f.ops())
			if err != nil || got != tc.text {
				t.Fatalf("read: length = %d, want %d; error = %v", len(got), len(tc.text), err)
			}
			f.wantCalls("open", "data", "size", "lock", "unlock", "close")
			if !reflect.DeepEqual(f.owners, []uintptr{0}) {
				t.Errorf("read owners = %v, want [0]", f.owners)
			}
		})
	}
	t.Run("terminator excludes allocation padding", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, []uint16{'a', 0, 'b', 0})
		got, err := readWindowsClipboard(context.Background(), f.ops())
		if err != nil || got != "a" {
			t.Fatalf("read = %q, %v; want a, nil", got, err)
		}
		f.wantCalls("open", "data", "size", "lock", "unlock", "close")
	})
}

func TestWindowsClipboardUTF16Bounds(t *testing.T) {
	maxInt := uintptr(^uint(0) >> 1)
	for _, size := range []uintptr{0, 1, 3, maxInt + 1, ^uintptr(0)} {
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			f := newWindowsClipboardFixture(t, []uint16{0})
			f.readSize = size
			got, err := readWindowsClipboard(context.Background(), f.ops())
			if err == nil || got != "" {
				t.Fatalf("size %d: read = %q, %v; want empty and error", size, got, err)
			}
			f.wantCalls("open", "data", "size", "close")
		})
	}
	if got, err := windowsUTF16Units(maxInt - 1); err != nil || got != int((maxInt-1)/2) {
		t.Errorf("largest representable even size: units = %d, error = %v", got, err)
	}
	t.Run("unterminated", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, []uint16{'a', 'b'})
		got, err := readWindowsClipboard(context.Background(), f.ops())
		if err == nil || got != "" {
			t.Fatalf("read = %q, %v; want empty and error", got, err)
		}
		f.wantCalls("open", "data", "size", "lock", "unlock", "close")
	})
	t.Run("does not scan beyond reported allocation", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, []uint16{'a', 0})
		f.readSize = 2
		if got, err := readWindowsClipboard(context.Background(), f.ops()); err == nil || got != "" {
			t.Fatalf("read = %q, %v; terminator outside allocation must not be accepted", got, err)
		}
		f.wantCalls("open", "data", "size", "lock", "unlock", "close")
	})
}

func TestWindowsClipboardReadFailuresAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		operation string
		calls     []string
	}{
		{"data", []string{"open", "data", "close"}},
		{"size", []string{"open", "data", "size", "close"}},
		{"lock", []string{"open", "data", "size", "lock", "close"}},
		{"unlock", []string{"open", "data", "size", "lock", "unlock", "close"}},
		{"close", []string{"open", "data", "size", "lock", "unlock", "close"}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			f := newWindowsClipboardFixture(t, []uint16{'a', 0})
			failure := errors.New("fixture " + tc.operation)
			f.fail[tc.operation] = failure
			got, err := readWindowsClipboard(context.Background(), f.ops())
			if got != "" || !errors.Is(err, failure) {
				t.Fatalf("read = %q, %v; want empty and original error", got, err)
			}
			f.wantCalls(tc.calls...)
		})
	}
	t.Run("missing text remains error", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		missing := errors.New("fixture missing Unicode text")
		ops := f.ops()
		ops.data = func() (uintptr, error) { return 0, errors.Join(f.call("data"), missing) }
		if got, err := readWindowsClipboard(context.Background(), ops); got != "" || !errors.Is(err, missing) {
			t.Fatalf("read = %q, %v; want missing-text error", got, err)
		}
		f.wantCalls("open", "data", "close")
	})
	t.Run("joined operation and close failures", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		dataErr, closeErr := errors.New("data"), errors.New("close")
		f.fail["data"], f.fail["close"] = dataErr, closeErr
		_, err := readWindowsClipboard(context.Background(), f.ops())
		if !errors.Is(err, dataErr) || !errors.Is(err, closeErr) {
			t.Fatalf("error = %v; want data and close identities", err)
		}
		f.wantCalls("open", "data", "close")
	})
	t.Run("joined unlock and close failures", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, []uint16{0})
		unlockErr, closeErr := errors.New("unlock"), errors.New("close")
		f.fail["unlock"], f.fail["close"] = unlockErr, closeErr
		_, err := readWindowsClipboard(context.Background(), f.ops())
		if !errors.Is(err, unlockErr) || !errors.Is(err, closeErr) {
			t.Fatalf("error = %v; want unlock and close identities", err)
		}
		f.wantCalls("open", "data", "size", "lock", "unlock", "close")
	})
}

func TestWindowsClipboardWriteUTF16AndOwnership(t *testing.T) {
	for _, text := range []string{"", "日本語 é \U0001f680"} {
		t.Run(text, func(t *testing.T) {
			f := newWindowsClipboardFixture(t, nil)
			if err := writeWindowsClipboard(context.Background(), text, f.ops()); err != nil {
				t.Fatal(err)
			}
			want := append(utf16.Encode([]rune(text)), 0)
			if !reflect.DeepEqual(f.written, want) || f.allocation != uintptr(len(want))*2 {
				t.Errorf("allocated %d bytes with %v, want %d bytes with %v", f.allocation, f.written, len(want)*2, want)
			}
			f.wantCalls("alloc", "lock", "unlock", "createOwner", "open", "empty", "set", "close", "destroyOwner")
			if !reflect.DeepEqual(f.owners, []uintptr{windowsFixtureOwner}) {
				t.Errorf("write owners = %v, want private owner", f.owners)
			}
		})
	}
	t.Run("NUL rejected before any backend operation", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		if err := writeWindowsClipboard(context.Background(), "before\x00after", f.ops()); err == nil {
			t.Fatal("embedded NUL accepted")
		}
		f.wantCalls()
	})
}

func TestWindowsClipboardWriteFailuresAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		operation string
		calls     []string
	}{
		{"alloc", []string{"alloc"}},
		{"lock", []string{"alloc", "lock", "free"}},
		{"unlock", []string{"alloc", "lock", "unlock", "free"}},
		{"createOwner", []string{"alloc", "lock", "unlock", "createOwner", "free"}},
		{"empty", []string{"alloc", "lock", "unlock", "createOwner", "open", "empty", "close", "destroyOwner", "free"}},
		{"set", []string{"alloc", "lock", "unlock", "createOwner", "open", "empty", "set", "close", "destroyOwner", "free"}},
		{"close", []string{"alloc", "lock", "unlock", "createOwner", "open", "empty", "set", "close", "destroyOwner"}},
		{"destroyOwner", []string{"alloc", "lock", "unlock", "createOwner", "open", "empty", "set", "close", "destroyOwner"}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			f := newWindowsClipboardFixture(t, nil)
			failure := errors.New("fixture " + tc.operation)
			f.fail[tc.operation] = failure
			if err := writeWindowsClipboard(context.Background(), "fixture", f.ops()); !errors.Is(err, failure) {
				t.Fatalf("error = %v, want original %s error", err, tc.operation)
			}
			f.wantCalls(tc.calls...)
		})
	}
	t.Run("joined publication and cleanup failures", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		for _, name := range []string{"set", "close", "destroyOwner", "free"} {
			f.fail[name] = errors.New("fixture " + name)
		}
		err := writeWindowsClipboard(context.Background(), "fixture", f.ops())
		for name, failure := range f.fail {
			if !errors.Is(err, failure) {
				t.Errorf("error %v lost %s identity", err, name)
			}
		}
		f.wantCalls("alloc", "lock", "unlock", "createOwner", "open", "empty", "set", "close", "destroyOwner", "free")
	})
	t.Run("free failure joins preparation failure", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		lockErr, freeErr := errors.New("lock"), errors.New("free")
		f.fail["lock"], f.fail["free"] = lockErr, freeErr
		err := writeWindowsClipboard(context.Background(), "fixture", f.ops())
		if !errors.Is(err, lockErr) || !errors.Is(err, freeErr) {
			t.Fatalf("error = %v; want lock and free identities", err)
		}
		f.wantCalls("alloc", "lock", "free")
	})
}

func TestWindowsClipboardContextAndOpenRetry(t *testing.T) {
	for _, write := range []bool{false, true} {
		name := "read"
		if write {
			name = "write"
		}
		t.Run(name+" pre-canceled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			f := newWindowsClipboardFixture(t, []uint16{0})
			var err error
			if write {
				err = writeWindowsClipboard(ctx, "fixture", f.ops())
			} else {
				_, err = readWindowsClipboard(ctx, f.ops())
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want canceled", err)
			}
			f.wantCalls()
		})
		t.Run(name+" expired deadline", func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			f := newWindowsClipboardFixture(t, []uint16{0})
			var err error
			if write {
				err = writeWindowsClipboard(ctx, "fixture", f.ops())
			} else {
				_, err = readWindowsClipboard(ctx, f.ops())
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want deadline exceeded", err)
			}
			f.wantCalls()
		})
		t.Run(name+" cancel after open", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newWindowsClipboardFixture(t, []uint16{0})
			f.after = func(op string) {
				if op == "open" {
					cancel()
				}
			}
			var err error
			if write {
				err = writeWindowsClipboard(ctx, "fixture", f.ops())
				f.wantCalls("alloc", "lock", "unlock", "createOwner", "open", "close", "destroyOwner", "free")
			} else {
				_, err = readWindowsClipboard(ctx, f.ops())
				f.wantCalls("open", "close")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want canceled", err)
			}
		})
	}
	t.Run("read cancellation after lock unlocks and closes", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := newWindowsClipboardFixture(t, []uint16{0})
		f.after = func(op string) {
			if op == "lock" {
				cancel()
			}
		}
		if got, err := readWindowsClipboard(ctx, f.ops()); got != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("read = %q, %v; want canceled", got, err)
		}
		f.wantCalls("open", "data", "size", "lock", "unlock", "close")
	})
	t.Run("open retries then succeeds", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, []uint16{0})
		f.openFails, f.openError = 2, errors.New("busy")
		if _, err := readWindowsClipboard(context.Background(), f.ops()); err != nil {
			t.Fatal(err)
		}
		f.wantCalls("open", "open", "open", "data", "size", "lock", "unlock", "close")
	})
	t.Run("write retries preserve private owner", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		f.openFails, f.openError = 2, errors.New("busy")
		if err := writeWindowsClipboard(context.Background(), "fixture", f.ops()); err != nil {
			t.Fatal(err)
		}
		f.wantCalls("alloc", "lock", "unlock", "createOwner", "open", "open", "open", "empty", "set", "close", "destroyOwner")
		if !reflect.DeepEqual(f.owners, []uintptr{windowsFixtureOwner, windowsFixtureOwner, windowsFixtureOwner}) {
			t.Errorf("retry owners = %v, want private owner on every attempt", f.owners)
		}
	})
	t.Run("open timeout preserves backend error without closing", func(t *testing.T) {
		f := newWindowsClipboardFixture(t, nil)
		busy := errors.New("busy until bounded open timeout")
		f.fail["open"] = busy
		got, err := readWindowsClipboard(context.Background(), f.ops())
		if got != "" || !errors.Is(err, busy) {
			t.Fatalf("read = %q, %v; want empty and original open error", got, err)
		}
		if len(f.calls) < 2 {
			t.Errorf("open attempts = %d, want retries", len(f.calls))
		}
		for _, call := range f.calls {
			if call != "open" {
				t.Fatalf("unexpected operation %s after failed open", call)
			}
		}
	})
	t.Run("write canceled while opening cleans private resources", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := newWindowsClipboardFixture(t, nil)
		f.fail["open"] = errors.New("busy")
		f.after = func(op string) {
			if op == "open" {
				cancel()
			}
		}
		if err := writeWindowsClipboard(ctx, "fixture", f.ops()); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want canceled", err)
		}
		f.wantCalls("alloc", "lock", "unlock", "createOwner", "open", "destroyOwner", "free")
	})
	t.Run("read canceled while waiting to open never closes unopened clipboard", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := newWindowsClipboardFixture(t, nil)
		f.fail["open"] = errors.New("busy")
		f.after = func(op string) { cancel() }
		if _, err := readWindowsClipboard(ctx, f.ops()); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want canceled", err)
		}
		f.wantCalls("open")
	})
}
