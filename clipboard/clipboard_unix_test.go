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

func TestClipboardContextCancelsCommand(t *testing.T) {
	directory := t.TempDir()
	command := filepath.Join(directory, xclip)
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	previousTool := selectedTool
	selectedTool = unixClipboardXclip
	t.Cleanup(func() { selectedTool = previousTool })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := ReadAllContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReadAllContext error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled clipboard command took %v", elapsed)
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
