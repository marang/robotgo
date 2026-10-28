//go:build freebsd || linux || netbsd || openbsd || solaris || dragonfly

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	commandpkg "github.com/marang/robotgo/internal/command"
)

const (
	envClipboardTempDir           = "TMPDIR"
	waylandClipboardTempPattern   = "robotgo-wl-copy-"
	waylandClipboardVersionPrefix = "wl-clipboard"
	waylandClipboardMinimumMajor  = 2
	waylandClipboardMinimumMinor  = 3
)

// wl-copy before 2.3.0 ignores TMPDIR and can leave a named payload file in
// /tmp when canceled. Probe without transferring text before every write, so
// older or unrecognized installed helpers are rejected before receiving text.
func requirePrivateWaylandClipboardSpool(ctx context.Context, name string) error {
	probeContext, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()
	output, err := commandpkg.Output(probeContext, name, "--version")
	if err != nil {
		return errors.Join(probeContext.Err(), fmt.Errorf("clipboard: probe wl-copy version for private temporary-file cleanup: %w", err))
	}
	firstLine, _, _ := strings.Cut(string(output), "\n")
	fields := strings.Fields(firstLine)
	if len(fields) == 2 && fields[0] == waylandClipboardVersionPrefix {
		parts := strings.Split(fields[1], ".")
		var version [3]int
		valid := len(parts) == len(version)
		if valid {
			for index, part := range parts {
				if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
					valid = false
					break
				}
				version[index], err = strconv.Atoi(part)
				if err != nil {
					valid = false
					break
				}
			}
		}
		if valid && (version[0] > waylandClipboardMinimumMajor || version[0] == waylandClipboardMinimumMajor && version[1] >= waylandClipboardMinimumMinor) {
			return nil
		}
	}
	return errors.New("clipboard: wl-copy 2.3.0 or later is required for private temporary-file cleanup; unrecognized and older helpers cannot safely receive clipboard text")
}

func writeWaylandClipboardContext(ctx context.Context, text, name string, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Bind metadata and payload to the same executable even if another
	// goroutine changes PATH while the version probe is running.
	copyPath, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("clipboard: resolve wl-copy: %w", err)
	}
	if err := requirePrivateWaylandClipboardSpool(ctx, copyPath); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", waylandClipboardTempPattern)
	if err != nil {
		return fmt.Errorf("clipboard: create private wl-copy temporary directory: %w", err)
	}
	// The final environment entry wins without changing the parent's TMPDIR.
	environment := append(os.Environ(), envClipboardTempDir+"="+directory)
	// Do not capture stderr through a pipe: wl-copy's successful background
	// selection owner inherits it. RunWithCleanup leaves stdout/stderr on
	// /dev/null and terminates the process group on command or cleanup failure,
	// not after a successful handoff.
	return commandpkg.RunWithCleanup(ctx, strings.NewReader(text), environment, func() error {
		return removeWaylandClipboardSpool(directory)
	}, copyPath, args...)
}

func removeWaylandClipboardSpool(directory string) error {
	removeErr := os.RemoveAll(directory)
	_, statErr := os.Lstat(directory)
	if errors.Is(statErr, os.ErrNotExist) {
		statErr = nil
	} else if statErr == nil {
		statErr = errors.New("private directory still exists")
	}
	if err := errors.Join(removeErr, statErr); err != nil {
		return fmt.Errorf("clipboard: remove private wl-copy temporary directory: %w", err)
	}
	return nil
}
