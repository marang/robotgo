//go:build freebsd || linux || netbsd || openbsd || solaris || dragonfly

package clipboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const clipboardTestSynchronizationTimeout = 2 * time.Second

type clipboardContextTestCall struct {
	name string
	run  func(context.Context) error
}

func clipboardContextTestCalls() []clipboardContextTestCall {
	return []clipboardContextTestCall{
		{
			name: "read",
			run: func(ctx context.Context) error {
				_, err := ReadAllContext(ctx)
				return err
			},
		},
		{
			name: "write",
			run: func(ctx context.Context) error {
				return WriteAllContext(ctx, "robotgo clipboard cancellation fixture")
			},
		},
	}
}

func installClipboardTestCommand(t *testing.T, script string) string {
	t.Helper()

	directory := t.TempDir()
	command := filepath.Join(directory, xclip)
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	previousTool := selectedTool
	selectedTool = unixClipboardXclip
	t.Cleanup(func() { selectedTool = previousTool })
	return directory
}

func startCancelableClipboardTestCall(
	t *testing.T,
	call func(context.Context) error,
) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- call(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(clipboardTestSynchronizationTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Error("canceled clipboard call did not stop during test cleanup")
		}
	})
	return cancel, result
}

func requireClipboardTestPath(t *testing.T, path string) {
	t.Helper()

	timer := time.NewTimer(clipboardTestSynchronizationTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("observe clipboard test path %q: %v", path, err)
		}

		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for clipboard test path %q", path)
		case <-ticker.C:
		}
	}
}

func requireCanceledClipboardTestCall(t *testing.T, result <-chan error) {
	t.Helper()

	timer := time.NewTimer(clipboardTestSynchronizationTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("clipboard call error = %v, want cancellation", err)
		}
	case <-timer.C:
		t.Fatal("clipboard call did not return after cancellation")
	}
}

func TestUnixClipboardCommandSelectionsAreStateless(t *testing.T) {
	tests := []struct {
		name      string
		tool      unixClipboardTool
		read      bool
		selection Selection
		wantName  string
		wantArgs  []string
	}{
		{name: "xclip clipboard read", tool: unixClipboardXclip, read: true, wantName: xclip, wantArgs: []string{"-out", "-selection", "clipboard"}},
		{name: "xclip primary write", tool: unixClipboardXclip, selection: SelectionPrimary, wantName: xclip, wantArgs: []string{"-in", "-selection", "primary"}},
		{name: "xsel clipboard write", tool: unixClipboardXsel, wantName: xsel, wantArgs: []string{"--input", "--clipboard"}},
		{name: "xsel primary read", tool: unixClipboardXsel, read: true, selection: SelectionPrimary, wantName: xsel, wantArgs: []string{"--output", "--primary"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, args, err := unixClipboardCommand(test.tool, test.read, test.selection)
			if err != nil {
				t.Fatalf("unixClipboardCommand: %v", err)
			}
			if name != test.wantName || !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("command = %s %v, want %s %v", name, args, test.wantName, test.wantArgs)
			}
		})
	}
}

func TestClipboardContextCancelsRunningCommand(t *testing.T) {
	for _, test := range clipboardContextTestCalls() {
		t.Run(test.name, func(t *testing.T) {
			script := "#!/bin/sh\nprintf ready > \"$ROBOTGO_CLIPBOARD_TEST_READY\"\nexec /bin/sleep 30\n"
			directory := installClipboardTestCommand(t, script)
			ready := filepath.Join(directory, "ready")
			t.Setenv("ROBOTGO_CLIPBOARD_TEST_READY", ready)

			cancel, result := startCancelableClipboardTestCall(t, test.run)
			requireClipboardTestPath(t, ready)
			cancel()
			requireCanceledClipboardTestCall(t, result)
		})
	}
}

func TestClipboardContextDoesNotStartCommandWhenAlreadyCanceled(t *testing.T) {
	for _, test := range clipboardContextTestCalls() {
		t.Run(test.name, func(t *testing.T) {
			script := "#!/bin/sh\nprintf invoked > \"$ROBOTGO_CLIPBOARD_TEST_INVOKED\"\n"
			directory := installClipboardTestCommand(t, script)
			invoked := filepath.Join(directory, "invoked")
			t.Setenv("ROBOTGO_CLIPBOARD_TEST_INVOKED", invoked)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := test.run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("clipboard call error = %v, want cancellation", err)
			}
			if _, err := os.Stat(invoked); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("already-canceled clipboard call invoked backend or stat failed: %v", err)
			}
		})
	}
}

func TestClipboardRejectsInvalidSelection(t *testing.T) {
	if _, err := ReadAllContext(context.Background(), Selection(99)); err == nil {
		t.Fatal("invalid clipboard selection accepted")
	}
	if err := WriteAllContext(context.Background(), "text", SelectionClipboard, SelectionPrimary); err == nil {
		t.Fatal("multiple clipboard selections accepted")
	}
}
